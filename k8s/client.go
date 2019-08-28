/*
Package k8s implements a client for communicating with a Kubernetes apiserver. It is intended
to support an ingress controller, so it is limited to the types needed.

The types are copied from the stable api of the Kubernetes 1.3 release.
*/
package k8s

import (
	"errors"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/fields"
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
// It's intended that client code will call the getters to retrieve the current state when notified.
type Client interface {
	// GetAllIngresses returns all the ingresses in the cluster.
	GetAllIngresses() ([]*v1beta1.Ingress, error)

	// GetIngresses returns ingresses in namespaces with matching labels
	GetIngresses(*NamespaceSelector) ([]*v1beta1.Ingress, error)

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
	clientset           *kubernetes.Clientset
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

// New creates a client for the kubernetes apiserver.
func New(kubeconfig string, resyncPeriod time.Duration) (Client, error) {
	clientConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	return &client{clientset: clientset, resyncPeriod: resyncPeriod}, nil
}

func (c *client) GetAllIngresses() ([]*v1beta1.Ingress, error) {
	return c.GetIngresses(nil)
}

func (c *client) GetIngresses(selector *NamespaceSelector) ([]*v1beta1.Ingress, error) {
	if !c.namespaceController.HasSynced() {
		return nil, errors.New("namespaces haven't synced yet")
	}

	allIngresses := []*v1beta1.Ingress{}
	for _, obj := range c.ingressStore.List() {
		allIngresses = append(allIngresses, obj.(*v1beta1.Ingress))
	}

	if selector == nil {
		return allIngresses, nil
	}

	supportedNamespaces := supportedNamespaces(selector, toNamespaces(c.namespaceStore.List()))

	filteredIngresses := []*v1beta1.Ingress{}
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

func supportedNamespaces(selector *NamespaceSelector, namespaces []*v1.Namespace) []*v1.Namespace {
	if selector == nil {
		return namespaces
	}

	filteredNamespaces := []*v1.Namespace{}
	for _, namespace := range namespaces {
		if val, ok := namespace.Labels[selector.LabelName]; ok && val == selector.LabelValue {
			filteredNamespaces = append(filteredNamespaces, namespace)
		}
	}
	log.Debugf("Found %d of %d namespaces that match the selector %s=%s",
		len(filteredNamespaces), len(namespaces), selector.LabelName, selector.LabelValue)

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

	ingressLW := cache.NewListWatchFromClient(c.clientset.ExtensionsV1beta1().RESTClient(), "ingresses", "",
		fields.Everything())
	c.ingressWatcher = &handlerWatcher{bufferedWatcher: newBufferedWatcher(bufferedWatcherDuration)}
	store, controller := cache.NewInformer(ingressLW, &v1beta1.Ingress{}, c.resyncPeriod, c.ingressWatcher)

	c.ingressStore = store
	c.ingressController = controller
	go controller.Run(make(chan struct{}))
}

func (c *client) GetServices() ([]*v1.Service, error) {
	c.createServiceSource()

	if !c.serviceController.HasSynced() {
		return nil, errors.New("services haven't synced yet")
	}

	services := []*v1.Service{}
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

	serviceLW := cache.NewListWatchFromClient(c.clientset.CoreV1().RESTClient(), "services", "", fields.Everything())
	c.serviceWatcher = &handlerWatcher{bufferedWatcher: newBufferedWatcher(bufferedWatcherDuration)}
	store, controller := cache.NewInformer(serviceLW, &v1.Service{}, c.resyncPeriod, c.serviceWatcher)

	c.serviceStore = store
	c.serviceController = controller
	go controller.Run(make(chan struct{}))
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

	namespaceLW := cache.NewListWatchFromClient(
		c.clientset.CoreV1().RESTClient(), "namespaces", "", fields.Everything())
	c.namespaceWatcher = &handlerWatcher{bufferedWatcher: newBufferedWatcher(bufferedWatcherDuration)}
	store, controller := cache.NewInformer(namespaceLW, &v1.Namespace{}, c.resyncPeriod, c.namespaceWatcher)

	c.namespaceStore = store
	c.namespaceController = controller
	go controller.Run(make(chan struct{}))
}

func (c *client) UpdateIngressStatus(ingress *v1beta1.Ingress) error {
	ingressClient := c.clientset.ExtensionsV1beta1().Ingresses(ingress.Namespace)

	currentIng, err := ingressClient.Get(ingress.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	currentIng.Status.LoadBalancer.Ingress = ingress.Status.LoadBalancer.Ingress

	_, err = ingressClient.UpdateStatus(currentIng)

	return err
}

// Implement cache.ResourceEventHandler
type handlerWatcher struct {
	*bufferedWatcher
}

func (w *handlerWatcher) notify() {
	w.bufferUpdate()
}

func (w *handlerWatcher) OnAdd(obj interface{}) {
	log.Debugf("OnAdd called for %v - updating watcher", obj)
	go w.notify()
}

func (w *handlerWatcher) OnUpdate(old interface{}, new interface{}) {
	log.Debugf("OnUpdate called for %v to %v - updating watcher", old, new)
	go w.notify()
}

func (w *handlerWatcher) OnDelete(obj interface{}) {
	log.Debugf("OnDelete called for %v - updating watcher", obj)
	go w.notify()
}
