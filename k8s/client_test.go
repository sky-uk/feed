package k8s

import (
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	clientV1Beta1 "k8s.io/client-go/kubernetes/typed/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
)

func TestClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Client Test Suite")
}

var _ = Describe("Client", func() {

	Describe("Watchers", func() {

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

		BeforeEach(func() {
			stopCh = make(chan struct{})
			resyncPeriod = time.Minute
			eventHandler = &handlerWatcher{}
			fakesInformerFactory = &fakeInformerFactory{}
			fakesHandlerFactory = &fakeEventHandlerFactory{}
			fakesStore = &cache.FakeCustomStore{}
			fakesController = &fakeController{}
			clt = &client{
				ingressGetter:       nil,
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
				<-runExecutedCh

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
					ingressGetter:       nil,
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

				<-runExecutedCh

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
				<-runExecutedCh

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
					ingressGetter:       nil,
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

				<-runExecutedCh

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
				<-runExecutedCh

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
					ingressGetter:       nil,
					resyncPeriod:        resyncPeriod,
					stopCh:              stopCh,
					informerFactory:     fakesInformerFactory,
					eventHandlerFactory: fakesHandlerFactory,
					namespaceStore:      existingStore,
					namespaceWatcher:    existingHandler,
					namespaceController: existingController,
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

				<-runExecutedCh

				waitGroup.Wait()
				Expect(lengthOf(watchers)).To(Equal(1))
			})
		})

	})

	Describe("GetAllIngresses", func() {

		var (
			fakesIngressStore        *cache.FakeCustomStore
			fakesNamespaceController *fakeController
			fakesIngressController   *fakeController
			clt                      *client
		)

		BeforeEach(func() {
			fakesNamespaceController = &fakeController{}
			fakesIngressController = &fakeController{}
			fakesIngressStore = &cache.FakeCustomStore{}
			clt = &client{
				namespaceController: fakesNamespaceController,
				ingressController:   fakesIngressController,
				ingressStore:        fakesIngressStore,
			}
		})

		It("should return all ingresses in the store when stores have synced", func() {
			ingressesInStore := []*v1beta1.Ingress{{}}
			fakesIngressStore.ListFunc = func() []interface{} {
				theList := make([]interface{}, len(ingressesInStore))
				for i := range ingressesInStore {
					theList[i] = ingressesInStore[i]
				}
				return theList
			}

			fakesNamespaceController.On("HasSynced").Return(true)
			fakesIngressController.On("HasSynced").Return(true)

			ingresses, err := clt.GetAllIngresses()
			Expect(ingresses).To(Equal(ingressesInStore))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return an error when namespace controller has not synced", func() {
			fakesNamespaceController.On("HasSynced").Return(false)
			fakesIngressController.On("HasSynced").Return(false)
			ingresses, err := clt.GetAllIngresses()
			Expect(err).To(HaveOccurred())
			Expect(ingresses).To(BeNil())
		})

		It("should return an error when ingress controller has not synced", func() {
			fakesNamespaceController.On("HasSynced").Return(true)
			fakesIngressController.On("HasSynced").Return(false)
			ingresses, err := clt.GetAllIngresses()
			Expect(err).To(HaveOccurred())
			Expect(ingresses).To(BeNil())
		})

		It("should return an error when both namespace and ingress controllers have not synced", func() {
			fakesNamespaceController.On("HasSynced").Return(false)
			fakesIngressController.On("HasSynced").Return(false)
			ingresses, err := clt.GetAllIngresses()
			Expect(err).To(HaveOccurred())
			Expect(ingresses).To(BeNil())
		})
	})

	Describe("GetServices", func() {
		var (
			fakesServiceStore      *cache.FakeCustomStore
			fakesServiceController *fakeController
			clt                    *client
		)

		BeforeEach(func() {
			fakesServiceController = &fakeController{}
			fakesServiceStore = &cache.FakeCustomStore{}
			clt = &client{
				serviceController: fakesServiceController,
				serviceStore:      fakesServiceStore,
			}
		})

		It("should return all services in the store when the service store has synced", func() {
			servicesInStore := []*v1.Service{{}}
			fakesServiceStore.ListFunc = func() []interface{} {
				theList := make([]interface{}, len(servicesInStore))
				for i := range servicesInStore {
					theList[i] = servicesInStore[i]
				}
				return theList
			}

			fakesServiceController.On("HasSynced").Return(true)

			services, err := clt.GetServices()
			Expect(services).To(Equal(servicesInStore))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return an error when service controller has not synced", func() {
			fakesServiceController.On("HasSynced").Return(false)
			services, err := clt.GetServices()
			Expect(err).To(HaveOccurred())
			Expect(services).To(BeNil())
		})

	})

	Describe("UpdateStatus", func() {
		var ingressClient *fakeIngressClient
		var unitUnderTest Client

		BeforeEach(func() {
			ingressClient = &fakeIngressClient{}

			unitUnderTest = &client{
				ingressGetter: &stubIngressGetter{ingressClient: ingressClient},
			}
		})

		Context("another feed pod has made a conflicting update", func() {
			It("should not report an error if the desired change has already been applied", func() {
				// given
				currentIngress := ingressWithLoadBalancerStatus(v1.LoadBalancerIngress{Hostname: "old-host"})
				newIngress := ingressWithLoadBalancerStatus(v1.LoadBalancerIngress{Hostname: "new-host"})
				independentlyUpdatedIngress := ingressWithLoadBalancerStatus(v1.LoadBalancerIngress{Hostname: "new-host"})

				ingressClient.On("Get", "test", metav1.GetOptions{}).Return(currentIngress).Once()
				ingressClient.On("UpdateStatus", newIngress).Return(currentIngress, errors.NewApplyConflict([]metav1.StatusCause{}, "conflict"))
				ingressClient.On("Get", "test", metav1.GetOptions{}).Return(independentlyUpdatedIngress)

				// when
				err := unitUnderTest.UpdateIngressStatus(newIngress)

				// then
				Expect(err).ToNot(HaveOccurred())
			})

			It("should report an error if the desired change has not been applied", func() {
				// given
				currentIngress := ingressWithLoadBalancerStatus(v1.LoadBalancerIngress{Hostname: "old-host"})
				newIngress := ingressWithLoadBalancerStatus(v1.LoadBalancerIngress{Hostname: "new-host"})
				independentlyUpdatedIngress := ingressWithLoadBalancerStatus(v1.LoadBalancerIngress{Hostname: "other-host"})

				ingressClient.On("Get", "test", metav1.GetOptions{}).Return(currentIngress).Once()
				ingressClient.On("UpdateStatus", newIngress).Return(currentIngress, errors.NewApplyConflict([]metav1.StatusCause{}, "conflict"))
				ingressClient.On("Get", "test", metav1.GetOptions{}).Return(independentlyUpdatedIngress)

				// when
				err := unitUnderTest.UpdateIngressStatus(newIngress)

				// then
				Expect(err).To(HaveOccurred())
			})
		})

		Context("the update fails for another reason", func() {
			It("should report the error directly", func() {
				// given
				currentIngress := ingressWithLoadBalancerStatus(v1.LoadBalancerIngress{Hostname: "old-host"})
				newIngress := ingressWithLoadBalancerStatus(v1.LoadBalancerIngress{Hostname: "new-host"})

				ingressClient.On("Get", "test", metav1.GetOptions{}).Return(currentIngress).Once()
				ingressClient.On("UpdateStatus", newIngress).Return(currentIngress, errors.NewServiceUnavailable("unavailable"))

				// when
				err := unitUnderTest.UpdateIngressStatus(newIngress)

				// then
				Expect(err).To(HaveOccurred())
			})
		})
	})
})

func ingressWithLoadBalancerStatus(status v1.LoadBalancerIngress) *v1beta1.Ingress {
	return &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         "test-ns",
			Name:              "test",
			DeletionTimestamp: nil,
		},
		Status: v1beta1.IngressStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{status},
			},
		},
	}
}

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

var _ clientV1Beta1.IngressInterface = &fakeIngressClient{}

type fakeIngressClient struct {
	mock.Mock
}

func (f *fakeIngressClient) Create(ingress *v1beta1.Ingress) (*v1beta1.Ingress, error) {
	r := f.Called(ingress)
	return r.Get(0).(*v1beta1.Ingress), r.Error(1)
}

func (f *fakeIngressClient) Update(ingress *v1beta1.Ingress) (*v1beta1.Ingress, error) {
	r := f.Called(ingress)
	return r.Get(0).(*v1beta1.Ingress), r.Error(1)
}

func (f *fakeIngressClient) UpdateStatus(ingress *v1beta1.Ingress) (*v1beta1.Ingress, error) {
	r := f.Called(ingress)
	return r.Get(0).(*v1beta1.Ingress), r.Error(1)
}

func (f *fakeIngressClient) Delete(name string, options *metav1.DeleteOptions) error {
	r := f.Called(name, options)
	return r.Error(0)
}

func (f *fakeIngressClient) DeleteCollection(options *metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	r := f.Called(options, listOptions)
	return r.Error(0)
}

func (f *fakeIngressClient) Get(name string, options metav1.GetOptions) (*v1beta1.Ingress, error) {
	r := f.Called(name, options)
	return r.Get(0).(*v1beta1.Ingress), nil
}

func (f *fakeIngressClient) List(opts metav1.ListOptions) (*v1beta1.IngressList, error) {
	r := f.Called(opts)
	return r.Get(0).(*v1beta1.IngressList), r.Error(1)
}

func (f *fakeIngressClient) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	r := f.Called(opts)
	return r.Get(0).(watch.Interface), r.Error(1)
}

func (f *fakeIngressClient) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1beta1.Ingress, err error) {
	r := f.Called(name, pt, data, subresources)
	return r.Get(0).(*v1beta1.Ingress), r.Error(1)
}

var _ clientV1Beta1.IngressesGetter = &stubIngressGetter{}

type stubIngressGetter struct {
	ingressClient clientV1Beta1.IngressInterface
}

func (s *stubIngressGetter) Ingresses(namespace string) clientV1Beta1.IngressInterface {
	return s.ingressClient
}
