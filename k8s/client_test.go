package k8s

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	"k8s.io/client-go/tools/cache"
	"sync"
	"testing"
	"time"
)

func TestClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Client Test Suite")
}

var _ = Describe("Client", func() {

	var (
		clt                  *client
		stopCh               chan struct{}
		resyncPeriod         time.Duration
		fakesInformerFactory *fakeInformerFactory
		fakesHandlerFactory  *fakeEventHandlerFactory
		fakesStore           *cache.FakeCustomStore
		fakesController      *fakeController
		eventHandler         *handlerWatcher
	)

	Describe("Watchers", func() {

		BeforeEach(func() {
			stopCh = make(chan struct{})
			resyncPeriod = time.Minute
			eventHandler = &handlerWatcher{}
			fakesInformerFactory = &fakeInformerFactory{}
			fakesHandlerFactory = &fakeEventHandlerFactory{}
			fakesStore = &cache.FakeCustomStore{}
			fakesController = &fakeController{}
			clt = &client{
				clientset:           nil,
				resyncPeriod:        resyncPeriod,
				stopCh:              stopCh,
				informerFactory:     fakesInformerFactory,
				eventHandlerFactory: fakesHandlerFactory,
			}
		})

		AfterEach(func() {
			fakesInformerFactory.AssertExpectations(GinkgoT())
			fakesHandlerFactory.AssertExpectations(GinkgoT())
			fakesController.AssertExpectations(GinkgoT())
		})

		Describe("WatchIngress", func() {

			It("should create the ingress source and return the corresponding watcher", func() {
				runExecutedCh := make(chan struct{})
				fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
				fakesInformerFactory.On("createIngressInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
				fakesController.On("Run", mock.Anything).Run(func(args mock.Arguments) {
					runExecutedCh <- struct{}{}
				})

				watcher := clt.WatchIngresses()
				<- runExecutedCh

				Expect(watcher).To(Equal(eventHandler))
				Expect(clt.ingressWatcher).To(Equal(eventHandler))
				Expect(clt.ingressController).To(Equal(fakesController))
				Expect(clt.ingressStore).To(Equal(fakesStore))
			})

			It("should return the existing ingress source and watcher when already exists", func() {
				existingHandler := &handlerWatcher{}
				existingStore := &cache.FakeCustomStore{}
				existingController := &fakeController{}

				clt = &client{
					clientset:           nil,
					resyncPeriod:        resyncPeriod,
					stopCh:              stopCh,
					informerFactory:     fakesInformerFactory,
					eventHandlerFactory: fakesHandlerFactory,
					ingressStore:        existingStore,
					ingressWatcher:      existingHandler,
					ingressController:   existingController,
				}

				watcher := clt.WatchIngresses()
				Expect(watcher).To(Equal(existingHandler))
				Expect(clt.ingressWatcher).To(Equal(existingHandler))
				Expect(clt.ingressController).To(Equal(existingController))
				Expect(clt.ingressStore).To(Equal(existingStore))
			})

			It("should guard against concurrent access", func() {
				runExecutedCh := make(chan struct{})
				fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
				fakesInformerFactory.On("createIngressInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
				fakesController.On("Run", mock.Anything).Run(func(args mock.Arguments) {
					runExecutedCh <- struct{}{}
				})

				var waitGroup sync.WaitGroup
				watchers := &sync.Map{}
				concurrentCalls := 10
				for i := 0; i < concurrentCalls; i++ {
					waitGroup.Add(1)
					go func() {
						watcher := clt.WatchIngresses()
						Expect(watcher).NotTo(BeNil())
						watchers.LoadOrStore(watcher, true)
						waitGroup.Done()
					}()
				}

				<- runExecutedCh

				waitGroup.Wait()
				Expect(lengthOf(watchers)).To(Equal(1))
			})
		})

		Describe("WatchServices", func() {

			It("should create the service source and return the corresponding watcher", func() {
				runExecutedCh := make(chan struct{})
				fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
				fakesInformerFactory.On("createServiceInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
				fakesController.On("Run", mock.Anything).Run(func(args mock.Arguments) {
					runExecutedCh <- struct{}{}
				})

				watcher := clt.WatchServices()
				<- runExecutedCh

				Expect(watcher).To(Equal(eventHandler))
				Expect(clt.serviceWatcher).To(Equal(eventHandler))
				Expect(clt.serviceController).To(Equal(fakesController))
				Expect(clt.serviceStore).To(Equal(fakesStore))
			})

			It("should return the existing service source and watcher when already exists", func() {
				existingHandler := &handlerWatcher{}
				existingStore := &cache.FakeCustomStore{}
				existingController := &fakeController{}

				clt = &client{
					clientset:           nil,
					resyncPeriod:        resyncPeriod,
					stopCh:              stopCh,
					informerFactory:     fakesInformerFactory,
					eventHandlerFactory: fakesHandlerFactory,
					serviceStore:        existingStore,
					serviceWatcher:      existingHandler,
					serviceController:   existingController,
				}

				watcher := clt.WatchServices()
				Expect(watcher).To(Equal(existingHandler))
				Expect(clt.serviceWatcher).To(Equal(existingHandler))
				Expect(clt.serviceController).To(Equal(existingController))
				Expect(clt.serviceStore).To(Equal(existingStore))
			})

			It("should guard against concurrent access", func() {
				runExecutedCh := make(chan struct{})
				fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
				fakesInformerFactory.On("createServiceInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
				fakesController.On("Run", mock.Anything).Run(func(args mock.Arguments) {
					runExecutedCh <- struct{}{}
				})

				var waitGroup sync.WaitGroup
				watchers := &sync.Map{}
				concurrentCalls := 10
				for i := 0; i < concurrentCalls; i++ {
					waitGroup.Add(1)
					go func() {
						watcher := clt.WatchServices()
						Expect(watcher).NotTo(BeNil())
						watchers.LoadOrStore(watcher, true)
						waitGroup.Done()
					}()
				}

				<- runExecutedCh

				waitGroup.Wait()
				Expect(lengthOf(watchers)).To(Equal(1))
			})
		})

		Describe("WatchNamespaces", func() {

			It("should create the namespace source and return the corresponding watcher", func() {
				runExecutedCh := make(chan struct{})
				fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
				fakesInformerFactory.On("createNamespaceInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
				fakesController.On("Run", mock.Anything).Run(func(args mock.Arguments) {
					runExecutedCh <- struct{}{}
				})

				watcher := clt.WatchNamespaces()
				<- runExecutedCh

				Expect(watcher).To(Equal(eventHandler))
				Expect(clt.namespaceWatcher).To(Equal(eventHandler))
				Expect(clt.namespaceController).To(Equal(fakesController))
				Expect(clt.namespaceStore).To(Equal(fakesStore))
			})

			It("should return the existing namespace source and watcher when already exists", func() {
				existingHandler := &handlerWatcher{}
				existingStore := &cache.FakeCustomStore{}
				existingController := &fakeController{}

				clt = &client{
					clientset:           nil,
					resyncPeriod:        resyncPeriod,
					stopCh:              stopCh,
					informerFactory:     fakesInformerFactory,
					eventHandlerFactory: fakesHandlerFactory,
					namespaceStore:        existingStore,
					namespaceWatcher:      existingHandler,
					namespaceController:   existingController,
				}

				watcher := clt.WatchNamespaces()
				Expect(watcher).To(Equal(existingHandler))
				Expect(clt.namespaceWatcher).To(Equal(existingHandler))
				Expect(clt.namespaceController).To(Equal(existingController))
				Expect(clt.namespaceStore).To(Equal(existingStore))
			})

			It("should guard against concurrent access", func() {
				runExecutedCh := make(chan struct{})
				fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
				fakesInformerFactory.On("createNamespaceInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
				fakesController.On("Run", mock.Anything).Run(func(args mock.Arguments) {
					runExecutedCh <- struct{}{}
				})

				var waitGroup sync.WaitGroup
				watchers := &sync.Map{}
				concurrentCalls := 10
				for i := 0; i < concurrentCalls; i++ {
					waitGroup.Add(1)
					go func() {
						watcher := clt.WatchNamespaces()
						Expect(watcher).NotTo(BeNil())
						watchers.LoadOrStore(watcher, true)
						waitGroup.Done()
					}()
				}

				<- runExecutedCh

				waitGroup.Wait()
				Expect(lengthOf(watchers)).To(Equal(1))
			})
		})

	})


})

func lengthOf(theSyncMap *sync.Map) int {
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
