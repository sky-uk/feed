package k8s

import (
	"errors"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Time to handle multiple updates occurring in a short time period, such as at startup where
// each existing endpoint / ingress produces a single update.
const bufferedWatcherDuration = time.Millisecond * 50

type resourceStore struct {
	clientset                *kubernetes.Clientset
	stopCh                   chan struct{}
	resyncPeriod             time.Duration
	informerFactory          informerFactory
	eventHandlerFactory      eventHandlerFactory
	namespaceWatchedResource *watchedResource
	ingressWatchedResource   *watchedResource
	serviceWatchedResource   *watchedResource
}

type watchedResource struct {
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

func newStore(clientset *kubernetes.Clientset, stopCh chan struct{}, resyncPeriod time.Duration) *resourceStore {
	return &resourceStore{
		clientset:           clientset,
		stopCh:              stopCh,
		resyncPeriod:        resyncPeriod,
		informerFactory:     &cacheInformerFactory{clientset: clientset},
		eventHandlerFactory: &bufferedEventHandlerFactory{},
	}
}

func (s *resourceStore) getOrCreateNamespaceSource() (*watchedResource, error) {
	if s.namespaceWatchedResource != nil {
		return s.namespaceWatchedResource, nil
	}

	namespaceWatcher := s.eventHandlerFactory.createBufferedHandler(bufferedWatcherDuration)
	store, controller := s.informerFactory.createNamespaceInformer(s.resyncPeriod, namespaceWatcher)

	go controller.Run(s.stopCh)

	if !cache.WaitForCacheSync(s.stopCh, controller.HasSynced) {
		return &watchedResource{}, errors.New("error while waiting for namespace cache to populate")
	}

	return &watchedResource{
		store:   store,
		watcher: namespaceWatcher,
	}, nil
}

func (s *resourceStore) getOrCreateIngressSource() (*watchedResource, error) {
	if s.ingressWatchedResource != nil {
		return s.ingressWatchedResource, nil
	}

	ingressWatcher := s.eventHandlerFactory.createBufferedHandler(bufferedWatcherDuration)
	store, controller := s.informerFactory.createIngressInformer(s.resyncPeriod, ingressWatcher)

	go controller.Run(s.stopCh)

	if !cache.WaitForCacheSync(s.stopCh, controller.HasSynced) {
		return &watchedResource{}, errors.New("error while waiting for ingress cache to populate")
	}

	return &watchedResource{
		store:   store,
		watcher: ingressWatcher,
	}, nil
}

func (s *resourceStore) getOrCreateServiceSource() (*watchedResource, error) {
	if s.serviceWatchedResource != nil {
		return s.serviceWatchedResource, nil
	}

	serviceWatcher := s.eventHandlerFactory.createBufferedHandler(bufferedWatcherDuration)
	store, controller := s.informerFactory.createServiceInformer(s.resyncPeriod, serviceWatcher)

	go controller.Run(s.stopCh)

	if !cache.WaitForCacheSync(s.stopCh, controller.HasSynced) {
		return &watchedResource{}, errors.New("error while waiting for service cache to populate")
	}

	return &watchedResource{
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
