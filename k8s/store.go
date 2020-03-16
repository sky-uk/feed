package k8s

import (
	"errors"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Time to handle multiple updates occurring in a short time period, such as at startup where
// each existing endpoint / ingress produces a single update.
const bufferedWatcherDuration = time.Millisecond * 50

// Store encapsulates the creation of the shared informer for each watched resource
type Store interface {
	// GetOrCreateNamespaceSource create a namespace informer and returns its corresponding store and event handler
	GetOrCreateNamespaceSource() (*WatchedStore, error)
	// GetOrCreateIngressSource create an ingress informer and returns its corresponding store and event handler
	GetOrCreateIngressSource() (*WatchedStore, error)
	// GetOrCreateServiceSource create a service informer and returns its corresponding store and event handler
	GetOrCreateServiceSource() (*WatchedStore, error)
}

type lazyLoadedStore struct {
	sync.Mutex
	clientset             *kubernetes.Clientset
	stopCh                chan struct{}
	resyncPeriod          time.Duration
	informerFactory       informerFactory
	eventHandlerFactory   eventHandlerFactory
	namespaceWatchedStore *WatchedStore
	ingressWatchedStore   *WatchedStore
	serviceWatchedStore   *WatchedStore
}

// WatchedStore exposes a store for the resource to allow interrogating the current state via get/list operations
// and an watcher so clients can subscribe to update notifications
type WatchedStore struct {
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
func (s *lazyLoadedStore) GetOrCreateNamespaceSource() (*WatchedStore, error) {
	s.Lock()
	defer s.Unlock()
	if s.namespaceWatchedStore != nil {
		return s.namespaceWatchedStore, nil
	}

	log.Debug("Creating an informer to watch namespace resources")
	var err error
	s.namespaceWatchedStore, err = s.getOrCreateSource(s.informerFactory.createNamespaceInformer)
	if err != nil {
		return nil, err
	}
	return s.namespaceWatchedStore, nil
}

//  GetOrCreateNamespaceSource creates an ingress informer and registers and event handler to watch for changes
func (s *lazyLoadedStore) GetOrCreateIngressSource() (*WatchedStore, error) {
	s.Lock()
	defer s.Unlock()
	if s.ingressWatchedStore != nil {
		return s.ingressWatchedStore, nil
	}

	log.Debug("Creating an informer to watch ingress resources")
	var err error
	s.ingressWatchedStore, err = s.getOrCreateSource(s.informerFactory.createIngressInformer)
	if err != nil {
		return nil, err
	}
	return s.ingressWatchedStore, nil
}

//  GetOrCreateServiceSource creates an ingress informer and registers and event handler to watch for changes
func (s *lazyLoadedStore) GetOrCreateServiceSource() (*WatchedStore, error) {
	s.Lock()
	defer s.Unlock()
	if s.serviceWatchedStore != nil {
		return s.serviceWatchedStore, nil
	}

	log.Debug("Creating an informer to watch service resources")
	var err error
	s.serviceWatchedStore, err = s.getOrCreateSource(s.informerFactory.createServiceInformer)
	if err != nil {
		return nil, err
	}
	return s.serviceWatchedStore, nil
}

func (s *lazyLoadedStore) getOrCreateSource(createInformer func (time.Duration, cache.ResourceEventHandler) (cache.Store, cache.Controller)) (*WatchedStore, error) {
	watcher := s.eventHandlerFactory.createBufferedHandler(bufferedWatcherDuration)
	store, controller := createInformer(s.resyncPeriod, watcher)

	go controller.Run(s.stopCh)

	if !cache.WaitForCacheSync(s.stopCh, controller.HasSynced) {
		return &WatchedStore{}, errors.New("error while waiting for cache to populate")
	}

	return &WatchedStore{
		store:   store,
		watcher: watcher,
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
