/*
Package k8s implements a client for communicating with a Kubernetes API server. It is intended
to support an ingress controller, so it is limited to the types needed.

The types are copied from the stable api of the Kubernetes 1.3 release.
*/
package k8s

import (
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

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
	WatchIngresses() (Watcher, error)

	// WatchServices watches for updates to services and notifies the Watcher.
	WatchServices() (Watcher, error)

	// WatchNamespaces watches for updates to namespaces and notifies the Watcher.
	WatchNamespaces() (Watcher, error)

	// UpdateIngressStatus updates the ingress status with the loadbalancer hostname or ip address.
	UpdateIngressStatus(*v1beta1.Ingress) error
}

type client struct {
	sync.Mutex
	clientset    *kubernetes.Clientset
	resyncPeriod time.Duration
	store        Store
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

	c := &client{
		clientset:    clientset,
		resyncPeriod: resyncPeriod,
		store:        NewStore(clientset, resyncPeriod, stopCh),
	}
	return c, nil
}

func (c *client) GetAllIngresses() ([]*v1beta1.Ingress, error) {
	return c.GetIngresses(nil)
}

func (c *client) GetIngresses(selector *NamespaceSelector) ([]*v1beta1.Ingress, error) {
	var allIngresses []*v1beta1.Ingress
	ingressWatchedResource, err := c.store.GetOrCreateIngressSource()
	if err != nil {
		return nil, err
	}
	for _, obj := range ingressWatchedResource.store.List() {
		allIngresses = append(allIngresses, obj.(*v1beta1.Ingress))
	}

	if selector == nil {
		return allIngresses, nil
	}

	namespaceWatchedResource, err := c.store.GetOrCreateNamespaceSource()
	if err != nil {
		return nil, err
	}
	supportedNamespaces := supportedNamespaces(selector, toNamespaces(namespaceWatchedResource.store.List()))

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

func supportedNamespaces(selector *NamespaceSelector, namespaces []*v1.Namespace) []*v1.Namespace {
	if selector == nil {
		return namespaces
	}

	var filteredNamespaces []*v1.Namespace
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

func (c *client) WatchIngresses() (Watcher, error) {
	ingressWatchedResource, err := c.store.GetOrCreateIngressSource()
	if err != nil {
		return nil, err
	}
	return ingressWatchedResource.watcher, nil
}

func (c *client) GetServices() ([]*v1.Service, error) {
	var services []*v1.Service
	serviceSource, err := c.store.GetOrCreateServiceSource()
	if err != nil {
		return nil, err
	}
	for _, obj := range serviceSource.store.List() {
		services = append(services, obj.(*v1.Service))
	}

	return services, nil
}

func (c *client) WatchServices() (Watcher, error) {
	serviceSource, err := c.store.GetOrCreateServiceSource()
	if err != nil {
		return nil, err
	}
	return serviceSource.watcher, nil
}

func (c *client) WatchNamespaces() (Watcher, error) {
	namespaceSource, err := c.store.GetOrCreateNamespaceSource()
	if err != nil {
		return nil, err
	}
	return namespaceSource.watcher, nil
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
