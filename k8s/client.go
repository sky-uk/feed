/*
Package k8s implements a client for communicating with a Kubernetes API server. It is intended
to support an ingress controller, so it is limited to the types needed.

The types are copied from the stable api of the Kubernetes 1.3 release.
*/
package k8s

import (
	"context"
	"errors"
	"sync"
	"time"

	k8errors "k8s.io/apimachinery/pkg/api/errors"
	clientV1Beta1 "k8s.io/client-go/kubernetes/typed/extensions/v1beta1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// Time to handle multiple updates occurring in a short time period, such as at startup where
// each existing endpoint / ingress produces a single update.
const bufferedWatcherDuration = time.Millisecond * 50

// Client for connecting to a Kubernetes cluster.
// Watchers will receive a notification whenever the client connects to the API server,
// including reconnects, to notify that there may be new ingresses that need to be retrieved.
// Watchers do not intentionally return errors, so they can be used to setup the control loop.
// It's intended that client code will call the getters to retrieve the current state when notified.
// Error handling including retry mechanism is expected to be put in place around calls to getters.
type Client interface {
	// GetAllIngresses returns all the ingresses in the cluster.
	GetAllIngresses() ([]*v1beta1.Ingress, error)

	// GetIngresses returns ingresses in namespaces with matching labels
	GetIngresses([]*NamespaceSelector, bool) ([]*v1beta1.Ingress, error)

	// GetServices returns all the services in the cluster.
	GetServices() ([]*v1.Service, error)

	// WatchIngresses watches for updates to ingresses and notifies the Watcher.
	WatchIngresses() Watcher

	// WatchServices watches for updates to services and notifies the Watcher.
	WatchServices() Watcher

	// WatchNamespaces watches for updates to namespaces and notifies the Watcher.
	WatchNamespaces() Watcher

	// UpdateIngressStatus updates the ingress status with the loadbalancer hostname or ip address.
	UpdateIngressStatus(*v1beta1.Ingress) error
}

type client struct {
	sync.Mutex
	ingressGetter       clientV1Beta1.IngressesGetter
	stopCh              chan struct{}
	informerFactory     informerFactory
	eventHandlerFactory eventHandlerFactory
	resyncPeriod        time.Duration
	ingressStore        cache.Store
	ingressController   cache.Controller
	ingressWatcher      *handlerWatcher
	serviceStore        cache.Store
	serviceController   cache.Controller
	serviceWatcher      *handlerWatcher
	namespaceStore      cache.Store
	namespaceController cache.Controller
	namespaceWatcher    *handlerWatcher
}

// NamespaceSelector defines the label name and value for filtering namespaces
type NamespaceSelector struct {
	LabelName  string
	LabelValue string
}

// New creates a client for the kubernetes API server.
func New(kubeconfig string, resyncPeriod time.Duration, stopCh chan struct{}) (Client, error) {
	clientConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	return &client{
		ingressGetter:       clientset.ExtensionsV1beta1(),
		resyncPeriod:        resyncPeriod,
		stopCh:              stopCh,
		informerFactory:     &cacheInformerFactory{clientset: clientset},
		eventHandlerFactory: &bufferedEventHandlerFactory{},
	}, nil
}

func (c *client) GetAllIngresses() ([]*v1beta1.Ingress, error) {
	return c.GetIngresses(nil, false)
}

func (c *client) GetIngresses(namespaceSelectors []*NamespaceSelector, matchAllNamespaceSelectors bool) ([]*v1beta1.Ingress, error) {
	if !c.namespaceController.HasSynced() {
		return nil, errors.New("namespaces haven't synced yet")
	}
	if !c.ingressController.HasSynced() {
		return nil, errors.New("ingresses haven't synced yet")
	}

	var allIngresses []*v1beta1.Ingress
	for _, obj := range c.ingressStore.List() {
		allIngresses = append(allIngresses, obj.(*v1beta1.Ingress))
	}

	if namespaceSelectors == nil {
		return allIngresses, nil
	}

	supportedNamespaces := supportedNamespaces(toNamespaces(c.namespaceStore.List()), namespaceSelectors, matchAllNamespaceSelectors)

	var filteredIngresses []*v1beta1.Ingress
	for _, ingress := range allIngresses {
		if ingressInNamespace(ingress, supportedNamespaces) {
			filteredIngresses = append(filteredIngresses, ingress)
		}
	}
	return filteredIngresses, nil
}

func toNamespaces(interfaces []interface{}) []*v1.Namespace {
	namespaces := make([]*v1.Namespace, len(interfaces))
	for i, obj := range interfaces {
		namespaces[i] = obj.(*v1.Namespace)
	}
	return namespaces
}

func supportedNamespaces(namespaces []*v1.Namespace, namespaceSelectors []*NamespaceSelector, matchAllNamespaceSelectors bool) []*v1.Namespace {
	if namespaceSelectors == nil {
		return namespaces
	}

	var filteredNamespaces []*v1.Namespace

	if matchAllNamespaceSelectors {
		for _, namespace := range namespaces {
			filteredNamespaces = safeAppend(filteredNamespaces, filterNamespacesMatchingAllLabels(namespace, namespaceSelectors))
		}

		log.Debugf("Found %d of %d namespaces that match the passed in namespace selectors", len(filteredNamespaces), len(namespaces))
		return filteredNamespaces
	}

	for _, namespaceSelector := range namespaceSelectors {
		filteredNamespaces = safeAppend(filteredNamespaces, filterNamespacesMatchingAnyLabel(namespaces, namespaceSelector)...)
	}
	return filteredNamespaces
}

func filterNamespacesMatchingAllLabels(namespace *v1.Namespace, namespaceSelectors []*NamespaceSelector) *v1.Namespace {
	allMatch := true
	for _, namespaceSelector := range namespaceSelectors {
		_, ok := namespace.Labels[namespaceSelector.LabelName]
		allMatch = allMatch && ok
	}

	if allMatch {
		return namespace
	}
	return nil
}

func filterNamespacesMatchingAnyLabel(namespaces []*v1.Namespace, namespaceSelector *NamespaceSelector) []*v1.Namespace {
	var filteredNamespaces []*v1.Namespace
	for _, namespace := range namespaces {
		if val, ok := namespace.Labels[namespaceSelector.LabelName]; ok && val == namespaceSelector.LabelValue {
			filteredNamespaces = append(filteredNamespaces, namespace)
		}
	}

	log.Debugf("Found %d of %d namespaces that match the selector %s=%s",
		len(filteredNamespaces), len(namespaces), namespaceSelector.LabelName, namespaceSelector.LabelValue)

	return filteredNamespaces
}

func ingressInNamespace(ingress *v1beta1.Ingress, namespaces []*v1.Namespace) bool {
	for _, namespace := range namespaces {
		if namespace.Name == ingress.Namespace {
			return true
		}
	}
	return false
}

func (c *client) WatchIngresses() Watcher {
	c.createIngressSource()
	return c.ingressWatcher
}

func (c *client) createIngressSource() {
	c.Lock()
	defer c.Unlock()
	if c.ingressStore != nil {
		return
	}

	watcher := c.eventHandlerFactory.createBufferedHandler(bufferedWatcherDuration)
	store, controller := c.informerFactory.createIngressInformer(c.resyncPeriod, watcher)
	go controller.Run(c.stopCh)

	c.ingressWatcher = watcher
	c.ingressStore = store
	c.ingressController = controller
}

func (c *client) GetServices() ([]*v1.Service, error) {
	if !c.serviceController.HasSynced() {
		return nil, errors.New("services haven't synced yet")
	}

	var services []*v1.Service
	for _, obj := range c.serviceStore.List() {
		services = append(services, obj.(*v1.Service))
	}

	return services, nil
}

func (c *client) WatchServices() Watcher {
	c.createServiceSource()
	return c.serviceWatcher
}

func (c *client) createServiceSource() {
	c.Lock()
	defer c.Unlock()
	if c.serviceStore != nil {
		return
	}

	watcher := c.eventHandlerFactory.createBufferedHandler(bufferedWatcherDuration)
	store, controller := c.informerFactory.createServiceInformer(c.resyncPeriod, watcher)
	go controller.Run(c.stopCh)

	c.serviceWatcher = watcher
	c.serviceStore = store
	c.serviceController = controller
}

func (c *client) WatchNamespaces() Watcher {
	c.createNamespaceSource()
	return c.namespaceWatcher
}

func (c *client) createNamespaceSource() {
	c.Lock()
	defer c.Unlock()
	if c.namespaceStore != nil {
		return
	}

	watcher := c.eventHandlerFactory.createBufferedHandler(bufferedWatcherDuration)
	store, controller := c.informerFactory.createNamespaceInformer(c.resyncPeriod, watcher)
	go controller.Run(c.stopCh)

	c.namespaceWatcher = watcher
	c.namespaceStore = store
	c.namespaceController = controller
}

func (c *client) UpdateIngressStatus(ingress *v1beta1.Ingress) error {
	ingressClient := c.ingressGetter.Ingresses(ingress.Namespace)

	currentIng, err := ingressClient.Get(context.Background(), ingress.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	currentIng.Status.LoadBalancer.Ingress = ingress.Status.LoadBalancer.Ingress
	return c.updateIngressAndHandleConflicts(ingressClient, currentIng)
}

func (c *client) updateIngressAndHandleConflicts(ingressClient clientV1Beta1.IngressInterface, ingress *v1beta1.Ingress) error {
	_, err := ingressClient.UpdateStatus(context.Background(), ingress, metav1.UpdateOptions{
		FieldManager:    "feed-ingress-controller",
		FieldValidation: metav1.FieldValidationStrict,
	})

	switch {
	case k8errors.IsConflict(err):
		// In the event of a conflict, check whether another feed instance has already made the same ingress status
		// change for us.
		updatedIng, getErr := ingressClient.Get(context.Background(), ingress.Name, metav1.GetOptions{})
		if getErr == nil && ingressStatusEqual(updatedIng.Status.LoadBalancer.Ingress, ingress.Status.LoadBalancer.Ingress) {
			// Another feed instance has already made the appropriate change, no need to report an error.
			return nil
		}
		return err

	case err != nil:
		return err

	default:
		return nil
	}
}

func ingressStatusEqual(i1 []v1.LoadBalancerIngress, i2 []v1.LoadBalancerIngress) bool {
	if len(i1) != len(i2) {
		return false
	}

	for x := range i1 {
		if i1[x].IP != i2[x].IP {
			return false
		}
		if i1[x].Hostname != i2[x].Hostname {
			return false
		}
		if len(i1[x].Ports) != len(i2[x].Ports) {
			return false
		}
		for y := range i1[x].Ports {
			if i1[x].Ports[y] != i2[x].Ports[y] {
				return false
			}
		}
	}

	return true
}

func safeAppend(arr []*v1.Namespace, elem ...*v1.Namespace) []*v1.Namespace {
	if elem != nil {
		for i := 0; i < len(elem); i++ {
			if elem[i] != nil {
				arr = append(arr, elem[i])
			}
		}
	}
	return arr
}
