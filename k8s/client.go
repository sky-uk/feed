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

	log "github.com/Sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/pkg/fields"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// Client for connecting to a Kubernetes cluster.
// Watchers will receive a notification whenever the client connects to the API server,
// including reconnects, to notify that there may be new ingresses that need to be retrieved.
// It's intended that client code will call the getters to retrieve the current state when notified.
type Client interface {
	// GetIngresses returns all the ingresses in the cluster.
	GetIngresses() ([]*v1beta1.Ingress, error)

	// GetEndpoints returns all the endpoints in the cluster.
	GetEndpoints() ([]*v1.Endpoints, error)

	// WatchIngresses watches for updates to ingresses and notifies the Watcher.
	WatchIngresses() Watcher

	// WatchEndpoints watches for updates to endpoints and notifies the Watcher.
	WatchEndpoints() Watcher
}

type client struct {
	sync.Mutex
	clientset          *kubernetes.Clientset
	resyncPeriod       time.Duration
	ingressStore       cache.Store
	ingressController  *cache.Controller
	ingressWatcher     *handlerWatcher
	endpointStore      cache.Store
	endpointController *cache.Controller
	endpointWatcher    *handlerWatcher
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
	c.ingressWatcher = &handlerWatcher{bufferedWatcher: newBufferedWatcher(time.Second)}
	store, controller := cache.NewInformer(ingressLW, &v1beta1.Ingress{}, c.resyncPeriod, c.ingressWatcher)

	c.ingressStore = store
	c.ingressController = controller
	go controller.Run(make(chan struct{}))
}

func (c *client) GetEndpoints() ([]*v1.Endpoints, error) {
	c.createEndpointSource()

	if !c.endpointController.HasSynced() {
		return nil, errors.New("Endpoints haven't synced yet")
	}

	var endpoints []*v1.Endpoints
	for _, obj := range c.endpointStore.List() {
		endpoints = append(endpoints, obj.(*v1.Endpoints))
	}

	return endpoints, nil
}

func (c *client) WatchEndpoints() Watcher {
	c.createEndpointSource()
	return c.endpointWatcher
}

func (c *client) createEndpointSource() {
	c.Lock()
	defer c.Unlock()
	if c.endpointStore != nil {
		return
	}

	endpointLW := cache.NewListWatchFromClient(c.clientset.CoreV1().RESTClient(), "endpoints", "", fields.Everything())
	c.endpointWatcher = &handlerWatcher{bufferedWatcher: newBufferedWatcher(time.Second)}
	store, controller := cache.NewInformer(endpointLW, &v1.Endpoints{}, c.resyncPeriod, c.endpointWatcher)

	c.endpointStore = store
	c.endpointController = controller
	go controller.Run(make(chan struct{}))
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
