package k8s

import (
	"errors"
	"time"

	v1 "k8s.io/api/core/v1"
	log "github.com/sirupsen/logrus"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Time to handle multiple updates occurring in a short time period, such as at startup where
// each existing endpoint / ingress produces a single update.
const bufferedWatcherDuration = time.Millisecond * 50

//
type Store interface {
	// GetOrCreateNamespaceSource create a namespace informer and returns its corresponding store and event handler
	GetOrCreateNamespaceSource() (*WatchedResource, error)
	// GetOrCreateIngressSource create an ingress informer and returns its corresponding store and event handler
	GetOrCreateIngressSource() (*WatchedResource, error)
	// GetOrCreateServiceSource create a service informer and returns its corresponding store and event handler
	GetOrCreateServiceSource() (*WatchedResource, error)
}

type lazyLoadedStore struct {
	clientset                *kubernetes.Clientset
	stopCh                   chan struct{}
	resyncPeriod             time.Duration
	informerFactory          informerFactory
	eventHandlerFactory      eventHandlerFactory
	namespaceWatchedResource *WatchedResource
	ingressWatchedResource   *WatchedResource
	serviceWatchedResource   *WatchedResource
}

// WatchedResource exposes a store for the resource to allow interrogating the current state via get/list operations
// and an watcher so clients can subscribe to update notifications
type WatchedResource struct {
	store   cache.Store
	watcher *handlerWatcher
}

// Implement cache.ResourceEventHandler
var _ informerFactory = &cacheInformerFactory{}

type informerFactory interface {
	createNamespaceInformer(time.Duration, cache.ResourceEventHandler) (cache.Store, cache.Controller)
	createIngressInformer(time.Duration, cache.ResourceEventHandler) (cache.Store, cache.Controller)
	createServiceInformer(time.Duration, cache.ResourceEventHandler) (cache.Store, cache.Controller)
}

type cacheInformerFactory struct {
	clientset *kubernetes.Clientset
}

// Implement cache.ResourceEventHandler
var _ informerFactory = &cacheInformerFactory{}

// NewStore creates a new store that starts watching for resources only when requested
func NewStore(clientset *kubernetes.Clientset, stopCh chan struct{}, resyncPeriod time.Duration) Store {
	return &lazyLoadedStore{
		clientset:           clientset,
		stopCh:              stopCh,
		resyncPeriod:        resyncPeriod,
		informerFactory:     &cacheInformerFactory{clientset: clientset},
		eventHandlerFactory: &bufferedEventHandlerFactory{},
	}
}

//  GetOrCreateNamespaceSource creates a namespace informer and registers and event handler to watch for changes
func (s *lazyLoadedStore) GetOrCreateNamespaceSource() (*WatchedResource, error) {
	if s.namespaceWatchedResource != nil {
		return s.namespaceWatchedResource, nil
	}

	log.Debug("Creating an informer to watch namespace resources")
	namespaceWatcher := s.eventHandlerFactory.createBufferedHandler(bufferedWatcherDuration)
	store, controller := s.informerFactory.createNamespaceInformer(s.resyncPeriod, namespaceWatcher)

	go controller.Run(s.stopCh)

	if !cache.WaitForCacheSync(s.stopCh, controller.HasSynced) {
		return &WatchedResource{}, errors.New("error while waiting for namespace cache to populate")
	}

	log.Debug("Namespace cache has been fully populated")
	return &WatchedResource{
		store:   store,
		watcher: namespaceWatcher,
	}, nil
}

//  GetOrCreateNamespaceSource creates an ingress informer and registers and event handler to watch for changes
func (s *lazyLoadedStore) GetOrCreateIngressSource() (*WatchedResource, error) {
	if s.ingressWatchedResource != nil {
		return s.ingressWatchedResource, nil
	}

	log.Debug("Creating an informer to watch ingress resources")
	ingressWatcher := s.eventHandlerFactory.createBufferedHandler(bufferedWatcherDuration)
	store, controller := s.informerFactory.createIngressInformer(s.resyncPeriod, ingressWatcher)

	go controller.Run(s.stopCh)

	if !cache.WaitForCacheSync(s.stopCh, controller.HasSynced) {
		return &WatchedResource{}, errors.New("error while waiting for ingress cache to populate")
	}

	log.Debug("Ingress cache has been fully populated")
	return &WatchedResource{
		store:   store,
		watcher: ingressWatcher,
	}, nil
}

//  GetOrCreateServiceSource creates an ingress informer and registers and event handler to watch for changes
func (s *lazyLoadedStore) GetOrCreateServiceSource() (*WatchedResource, error) {
	if s.serviceWatchedResource != nil {
		return s.serviceWatchedResource, nil
	}

	log.Debug("Creating an informer to watch service resources")
	serviceWatcher := s.eventHandlerFactory.createBufferedHandler(bufferedWatcherDuration)
	store, controller := s.informerFactory.createServiceInformer(s.resyncPeriod, serviceWatcher)

	go controller.Run(s.stopCh)

	if !cache.WaitForCacheSync(s.stopCh, controller.HasSynced) {
		return &WatchedResource{}, errors.New("error while waiting for service cache to populate")
	}

	log.Debug("Service cache has been fully populated")
	return &WatchedResource{
		store:   store,
		watcher: serviceWatcher,
	}, nil
}

func (c *cacheInformerFactory) createNamespaceInformer(resyncPeriod time.Duration, eventHandler cache.ResourceEventHandler) (cache.Store, cache.Controller) {
	namespaceLW := cache.NewListWatchFromClient(c.clientset.CoreV1().RESTClient(), "namespaces", "", fields.Everything())
	return cache.NewInformer(namespaceLW, &v1.Namespace{}, resyncPeriod, eventHandler)
}

func (c *cacheInformerFactory) createIngressInformer(resyncPeriod time.Duration, eventHandler cache.ResourceEventHandler) (cache.Store, cache.Controller) {
	ingressLW := cache.NewListWatchFromClient(c.clientset.ExtensionsV1beta1().RESTClient(), "ingresses", "", fields.Everything())
	return cache.NewInformer(ingressLW, &v1beta1.Ingress{}, resyncPeriod, eventHandler)
}

func (c *cacheInformerFactory) createServiceInformer(resyncPeriod time.Duration, eventHandler cache.ResourceEventHandler) (cache.Store, cache.Controller) {
	serviceLW := cache.NewListWatchFromClient(c.clientset.CoreV1().RESTClient(), "services", "", fields.Everything())
	return cache.NewInformer(serviceLW, &v1.Service{}, resyncPeriod, eventHandler)
}
