package k8s

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	"k8s.io/client-go/tools/cache"
	"testing"
	"time"
)

func TestStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Store Test Suite")
}

var _ = Describe("Store", func() {

	var (
		stopCh               chan struct{}
		resyncPeriod         time.Duration
		store                *resourceStore
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
		store = &resourceStore{
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

			source, err := store.createNamespaceSource()
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
			_, err := store.createNamespaceSource()
			endTime := time.Now()
			Expect(err).NotTo(HaveOccurred())
			Expect(endTime).To(BeTemporally(">", startTime.Add(cacheSyncDuration), time.Millisecond * 500))
		})
	})

	Describe("ingress source creation", func() {

		It("should return the ingress store and its corresponding event handler", func() {
			fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
			fakesInformerFactory.On("createIngressInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
			fakesController.On("Run", mock.Anything)
			fakesController.On("HasSynced").Return(true)

			source, err := store.createIngressSource()
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
			_, err := store.createIngressSource()
			endTime := time.Now()
			Expect(err).NotTo(HaveOccurred())
			Expect(endTime).To(BeTemporally(">", startTime.Add(cacheSyncDuration), time.Millisecond * 500))
		})
	})

	Describe("service source creation", func() {

		It("should return the service store and its corresponding event handler", func() {
			fakesHandlerFactory.On("createBufferedHandler", bufferedWatcherDuration).Return(eventHandler)
			fakesInformerFactory.On("createServiceInformer", resyncPeriod, eventHandler).Return(fakesStore, fakesController)
			fakesController.On("Run", mock.Anything)
			fakesController.On("HasSynced").Return(true)

			source, err := store.createServiceSource()
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
			_, err := store.createServiceSource()
			endTime := time.Now()
			Expect(err).NotTo(HaveOccurred())
			Expect(endTime).To(BeTemporally(">", startTime.Add(cacheSyncDuration), time.Millisecond * 500))
		})
	})

})

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
