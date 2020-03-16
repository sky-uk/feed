package k8s

import (
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	"k8s.io/client-go/tools/cache"
)

func TestStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Store Test Suite")
}

var _ = Describe("Store", func() {

	var (
		stopCh               chan struct{}
		resyncPeriod         time.Duration
		store                *lazyLoadedStore
		fakesInformerFactory *fakeInformerFactory
		fakesHandlerFactory  *fakeEventHandlerFactory
		fakesStore           *cache.FakeCustomStore
		fakesController      *fakeController
		eventHandler         *handlerWatcher
	)

	BeforeEach(func() {
		stopCh = make(chan struct{})
		resyncPeriod = time.Minute
		eventHandler = &handlerWatcher{}
		fakesInformerFactory = &fakeInformerFactory{}
		fakesHandlerFactory = &fakeEventHandlerFactory{}
		fakesStore = &cache.FakeCustomStore{}
		fakesController = &fakeController{}
		store = &lazyLoadedStore{
			clientset:           nil,
			stopCh:              stopCh,
			resyncPeriod:        resyncPeriod,
			informerFactory:     fakesInformerFactory,
			eventHandlerFactory: fakesHandlerFactory,
		}
	})

	AfterEach(func() {
		fakesInformerFactory.AssertExpectations(GinkgoT())
		fakesHandlerFactory.AssertExpectations(GinkgoT())
		fakesController.AssertExpectations(GinkgoT())
	})

	Describe("namespace source creation", func() {

		It("should return the namespace store and its corresponding event handler", func() {
			fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
			fakesInformerFactory.On("createNamespaceInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
			fakesController.On("Run", mock.Anything)
			fakesController.On("HasSynced").Return(true)

			source, err := store.GetOrCreateNamespaceSource()
			Expect(err).NotTo(HaveOccurred())
			Expect(source).NotTo(BeNil())
			Expect(source.store).To(Equal(fakesStore))
			Expect(source.watcher).To(Equal(eventHandler))
		})

		It("should start the informer and wait until its cache has synced", func() {
			fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
			fakesInformerFactory.On("createNamespaceInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
			fakesController.On("Run", mock.Anything)
			cacheSyncDuration := time.Second * 1
			fakesController.On("HasSynced").Return(false).After(cacheSyncDuration).Return(true)

			startTime := time.Now()
			_, err := store.GetOrCreateNamespaceSource()
			endTime := time.Now()
			Expect(err).NotTo(HaveOccurred())
			Expect(endTime).To(BeTemporally(">", startTime.Add(cacheSyncDuration), time.Millisecond*500))
		})

		It("should return the existing watched resource when already exists", func() {
			store = &lazyLoadedStore{
				clientset:                nil,
				stopCh:                   stopCh,
				resyncPeriod:             resyncPeriod,
				informerFactory:          fakesInformerFactory,
				eventHandlerFactory:      fakesHandlerFactory,
				namespaceWatchedResource: &WatchedResource{watcher: eventHandler, store: fakesStore},
			}

			source, err := store.GetOrCreateNamespaceSource()
			Expect(err).NotTo(HaveOccurred())
			Expect(source).NotTo(BeNil())
			Expect(source.store).To(Equal(fakesStore))
			Expect(source.watcher).To(Equal(eventHandler))
		})

		It("should guard against concurrent access", func() {
			fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
			fakesInformerFactory.On("createNamespaceInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
			fakesController.On("Run", mock.Anything)
			fakesController.On("HasSynced").Return(true)

			var waitGroup sync.WaitGroup
			watchedResources := sync.Map{}
			concurrentCalls := 10
			for i := 0; i < concurrentCalls; i++ {
				waitGroup.Add(1)
				go func() {
					source, err := store.GetOrCreateNamespaceSource()
					Expect(err).NotTo(HaveOccurred())
					Expect(source).NotTo(BeNil())
					watchedResources.LoadOrStore(source, true)
					waitGroup.Done()
				}()
			}

			waitGroup.Wait()
			Expect(lengthOf(watchedResources)).To(Equal(1))
		})
	})

	Describe("ingress source creation", func() {

		It("should return the ingress store and its corresponding event handler", func() {
			fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
			fakesInformerFactory.On("createIngressInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
			fakesController.On("Run", mock.Anything)
			fakesController.On("HasSynced").Return(true)

			source, err := store.GetOrCreateIngressSource()
			Expect(err).NotTo(HaveOccurred())
			Expect(source).NotTo(BeNil())
			Expect(source.store).To(Equal(fakesStore))
			Expect(source.watcher).To(Equal(eventHandler))
		})

		It("should start the informer and wait until its cache has synced", func() {
			fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
			fakesInformerFactory.On("createIngressInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
			fakesController.On("Run", mock.Anything)
			cacheSyncDuration := time.Second * 1
			fakesController.On("HasSynced").Return(false).After(cacheSyncDuration).Return(true)

			startTime := time.Now()
			_, err := store.GetOrCreateIngressSource()
			endTime := time.Now()
			Expect(err).NotTo(HaveOccurred())
			Expect(endTime).To(BeTemporally(">", startTime.Add(cacheSyncDuration), time.Millisecond*500))
		})

		It("should return the existing watched resource when already exists", func() {
			store = &lazyLoadedStore{
				clientset:              nil,
				stopCh:                 stopCh,
				resyncPeriod:           resyncPeriod,
				informerFactory:        fakesInformerFactory,
				eventHandlerFactory:    fakesHandlerFactory,
				ingressWatchedResource: &WatchedResource{watcher: eventHandler, store: fakesStore},
			}

			source, err := store.GetOrCreateIngressSource()
			Expect(err).NotTo(HaveOccurred())
			Expect(source).NotTo(BeNil())
			Expect(source.store).To(Equal(fakesStore))
			Expect(source.watcher).To(Equal(eventHandler))
		})

		It("should guard against concurrent access", func() {
			fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
			fakesInformerFactory.On("createIngressInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
			fakesController.On("Run", mock.Anything)
			fakesController.On("HasSynced").Return(true)

			var waitGroup sync.WaitGroup
			watchedResources := sync.Map{}
			concurrentCalls := 10
			for i := 0; i < concurrentCalls; i++ {
				waitGroup.Add(1)
				go func() {
					source, err := store.GetOrCreateIngressSource()
					Expect(err).NotTo(HaveOccurred())
					Expect(source).NotTo(BeNil())
					watchedResources.LoadOrStore(source, true)
					waitGroup.Done()
				}()
			}

			waitGroup.Wait()
			Expect(lengthOf(watchedResources)).To(Equal(1))
		})
	})

	Describe("service source creation", func() {

		It("should return the service store and its corresponding event handler", func() {
			fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
			fakesInformerFactory.On("createServiceInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
			fakesController.On("Run", mock.Anything)
			fakesController.On("HasSynced").Return(true)

			source, err := store.GetOrCreateServiceSource()
			Expect(err).NotTo(HaveOccurred())
			Expect(source).NotTo(BeNil())
			Expect(source.store).To(Equal(fakesStore))
			Expect(source.watcher).To(Equal(eventHandler))
		})

		It("should start the informer and wait until its cache has synced", func() {
			fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
			fakesInformerFactory.On("createServiceInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
			fakesController.On("Run", mock.Anything)
			cacheSyncDuration := time.Second * 1
			fakesController.On("HasSynced").Return(false).After(cacheSyncDuration).Return(true)

			startTime := time.Now()
			_, err := store.GetOrCreateServiceSource()
			endTime := time.Now()
			Expect(err).NotTo(HaveOccurred())
			Expect(endTime).To(BeTemporally(">", startTime.Add(cacheSyncDuration), time.Millisecond*500))
		})

		It("should return the existing watched resource when already exists", func() {
			store = &lazyLoadedStore{
				clientset:              nil,
				stopCh:                 stopCh,
				resyncPeriod:           resyncPeriod,
				informerFactory:        fakesInformerFactory,
				eventHandlerFactory:    fakesHandlerFactory,
				serviceWatchedResource: &WatchedResource{watcher: eventHandler, store: fakesStore},
			}

			source, err := store.GetOrCreateServiceSource()
			Expect(err).NotTo(HaveOccurred())
			Expect(source).NotTo(BeNil())
			Expect(source.store).To(Equal(fakesStore))
			Expect(source.watcher).To(Equal(eventHandler))
		})

		It("should guard against concurrent access", func() {
			fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
			fakesInformerFactory.On("createServiceInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
			fakesController.On("Run", mock.Anything)
			fakesController.On("HasSynced").Return(true)

			var waitGroup sync.WaitGroup
			watchedResources := sync.Map{}
			concurrentCalls := 10
			for i := 0; i < concurrentCalls; i++ {
				waitGroup.Add(1)
				go func() {
					source, err := store.GetOrCreateServiceSource()
					Expect(err).NotTo(HaveOccurred())
					Expect(source).NotTo(BeNil())
					watchedResources.LoadOrStore(source, true)
					waitGroup.Done()
				}()
			}

			waitGroup.Wait()
			Expect(lengthOf(watchedResources)).To(Equal(1))
		})
	})

})

func lengthOf(theSyncMap sync.Map) int {
	length := 0
	theSyncMap.Range(func(key, val interface{}) bool {
		length++
		return true
	})
	return length
}

type fakeController struct {
	mock.Mock
}

func (c *fakeController) Run(stopCh <-chan struct{}) {
	c.Called(stopCh)
}

func (c *fakeController) HasSynced() bool {
	args := c.Called()
	return args.Get(0).(bool)
}

func (c *fakeController) LastSyncResourceVersion() string {
	args := c.Called()
	return args.Get(0).(string)
}

type fakeInformerFactory struct {
	mock.Mock
}

func (i *fakeInformerFactory) createNamespaceInformer(resyncPeriod time.Duration, eventHandler cache.ResourceEventHandler) (cache.Store, cache.Controller) {
	args := i.Called(resyncPeriod, eventHandler)
	return args.Get(0).(cache.Store), args.Get(1).(cache.Controller)
}

func (i *fakeInformerFactory) createIngressInformer(resyncPeriod time.Duration, eventHandler cache.ResourceEventHandler) (cache.Store, cache.Controller) {
	args := i.Called(resyncPeriod, eventHandler)
	return args.Get(0).(cache.Store), args.Get(1).(cache.Controller)
}

func (i *fakeInformerFactory) createServiceInformer(resyncPeriod time.Duration, eventHandler cache.ResourceEventHandler) (cache.Store, cache.Controller) {
	args := i.Called(resyncPeriod, eventHandler)
	return args.Get(0).(cache.Store), args.Get(1).(cache.Controller)
}

type fakeEventHandlerFactory struct {
	mock.Mock
}

func (h *fakeEventHandlerFactory) createBufferedHandler(bufferTime time.Duration) *handlerWatcher {
	args := h.Called(bufferTime)
	return args.Get(0).(*handlerWatcher)
}
