/*
Package k8s implements a client for communicating with a Kubernetes apiserver. It is intended
to support an ingress controller, so it is limited to the types needed.

The types are copied from the stable api of the Kubernetes 1.3 release.
*/
package k8s

import (
	"sync"
	"time"

	"errors"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/pkg/fields"
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
	// GetIngresses returns all the ingresses in the cluster.
	GetIngresses() ([]*v1beta1.Ingress, error)

	// GetServices returns all the services in the cluster.
	GetServices() ([]*v1.Service, error)

	// WatchIngresses watches for updates to ingresses and notifies the Watcher.
	WatchIngresses() Watcher

	// WatchServices watches for updates to services and notifies the Watcher.
	WatchServices() Watcher

	// UpdateIngressStatus updates the ingress status with the loadbalancer hostname or ip address.
	UpdateIngressStatus(*v1beta1.Ingress) error
}

type client struct {
	sync.Mutex
	clientset         *kubernetes.Clientset
	resyncPeriod      time.Duration
	ingressStore      cache.Store
	ingressController *cache.Controller
	ingressWatcher    *handlerWatcher
	serviceStore      cache.Store
	serviceController *cache.Controller
	serviceWatcher    *handlerWatcher
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

func (c *client) GetIngresses() ([]*v1beta1.Ingress, error) {
	c.createIngressSource()

	if !c.ingressController.HasSynced() {
		return nil, errors.New("Ingresses haven't synced yet")
	}

	ingresses := []*v1beta1.Ingress{}
	for _, obj := range c.ingressStore.List() {
		ingresses = append(ingresses, obj.(*v1beta1.Ingress))
	}

	return ingresses, nil
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
		return nil, errors.New("Services haven't synced yet")
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

func (c *client) UpdateIngressStatus(ingress *v1beta1.Ingress) error {
	ingressClient := c.clientset.ExtensionsV1beta1().Ingresses(ingress.Namespace)

	currentIng, err := ingressClient.Get(ingress.Name)
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
	log.Debug("OnDelete called for %v - updating watcher", obj)
	go w.notify()
}
