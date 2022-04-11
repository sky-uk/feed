package k8s

import (
	"context"
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
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1apply "k8s.io/client-go/applyconfigurations/networking/v1"
	clientnetworkingv1 "k8s.io/client-go/kubernetes/typed/networking/v1"
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
			ingressesInStore := []*networkingv1.Ingress{{}}
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

	Describe("GetIngresses", func() {
		var (
			fakesIngressStore        *cache.FakeCustomStore
			fakesNamespaceStore      *cache.FakeCustomStore
			fakesNamespaceController *fakeController
			fakesIngressController   *fakeController
			clt                      *client
		)

		BeforeEach(func() {
			fakesNamespaceController = &fakeController{}
			fakesIngressController = &fakeController{}
			fakesIngressStore = &cache.FakeCustomStore{}
			fakesNamespaceStore = &cache.FakeCustomStore{}
			clt = &client{
				namespaceController: fakesNamespaceController,
				ingressController:   fakesIngressController,
				ingressStore:        fakesIngressStore,
				namespaceStore:      fakesNamespaceStore,
			}
			fakesNamespaceController.On("HasSynced").Return(true)
			fakesIngressController.On("HasSynced").Return(true)
		})

		It("should match all provided labels on ingress namespace", func() {
			// given
			namespaceSelectors := []*NamespaceSelector{
				{
					LabelName:  "some-label-name",
					LabelValue: "some-value",
				},
				{
					LabelName:  "team",
					LabelValue: "some-team-name",
				},
			}
			fakesIngressStore.ListFunc = func() []interface{} {
				ingresses := make([]interface{}, 2)
				ingresses[0] = &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{
					Namespace: "matching-namespace",
				}}
				ingresses[1] = &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{
					Namespace: "non-matching-namespace",
				}}
				return ingresses
			}

			fakesNamespaceStore.ListFunc = func() []interface{} {
				namespaces := make([]interface{}, 2)
				namespaces[0] = &v1.Namespace{ObjectMeta: metav1.ObjectMeta{
					Name: "matching-namespace",
					Labels: map[string]string{
						"some-label-name": "some-value",
						"team":            "some-team-name",
					},
				}}
				namespaces[1] = &v1.Namespace{ObjectMeta: metav1.ObjectMeta{
					Name: "non-matching-namespace",
					Labels: map[string]string{
						"team": "some-team-name",
					},
				}}
				return namespaces
			}

			// when
			ingresses, err := clt.GetIngresses(namespaceSelectors, true)
			Expect(err).To(BeNil())
			Expect(len(ingresses)).To(Equal(1))
			Expect(ingresses[0].Namespace).To(Equal("matching-namespace"))
		})

		It("should match any provided labels on ingress namespace", func() {
			// given
			namespaceSelectors := []*NamespaceSelector{
				{
					LabelName:  "team",
					LabelValue: "some-team-1",
				},
				{
					LabelName:  "team",
					LabelValue: "some-team-2",
				},
			}
			fakesIngressStore.ListFunc = func() []interface{} {
				ingresses := make([]interface{}, 2)
				ingresses[0] = &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{
					Namespace: "matching-namespace-one",
				}}
				ingresses[1] = &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{
					Namespace: "matching-namespace-two",
				}}
				return ingresses
			}

			fakesNamespaceStore.ListFunc = func() []interface{} {
				namespaces := make([]interface{}, 2)
				namespaces[0] = &v1.Namespace{ObjectMeta: metav1.ObjectMeta{
					Name: "matching-namespace-one",
					Labels: map[string]string{
						"some-label-name": "some-value",
						"team":            "some-team-1",
					},
				}}
				namespaces[1] = &v1.Namespace{ObjectMeta: metav1.ObjectMeta{
					Name: "matching-namespace-two",
					Labels: map[string]string{
						"team": "some-team-2",
					},
				}}
				return namespaces
			}

			// when
			ingresses, err := clt.GetIngresses(namespaceSelectors, false)
			Expect(err).To(BeNil())
			Expect(len(ingresses)).To(Equal(2))
			Expect(ingresses[0].Namespace).To(Equal("matching-namespace-one"))
			Expect(ingresses[1].Namespace).To(Equal("matching-namespace-two"))
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

				ingressClient.On("Get", context.TODO(), "test", metav1.GetOptions{}).Return(currentIngress).Once()
				ingressClient.On("UpdateStatus", context.TODO(), newIngress, metav1.UpdateOptions{}).Return(currentIngress, errors.NewApplyConflict([]metav1.StatusCause{}, "conflict"))
				ingressClient.On("Get", context.TODO(), "test", metav1.GetOptions{}).Return(independentlyUpdatedIngress)

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

				ingressClient.On("Get", context.TODO(), "test", metav1.GetOptions{}).Return(currentIngress).Once()
				ingressClient.On("UpdateStatus", context.TODO(), newIngress, metav1.UpdateOptions{}).Return(currentIngress, errors.NewApplyConflict([]metav1.StatusCause{}, "conflict"))
				ingressClient.On("Get", context.TODO(), "test", metav1.GetOptions{}).Return(independentlyUpdatedIngress)

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

				ingressClient.On("Get", context.TODO(), "test", metav1.GetOptions{}).Return(currentIngress).Once()
				ingressClient.On("UpdateStatus", context.TODO(), newIngress, metav1.UpdateOptions{}).Return(
					currentIngress, errors.NewServiceUnavailable("unavailable"))

				// when
				err := unitUnderTest.UpdateIngressStatus(newIngress)

				// then
				Expect(err).To(HaveOccurred())
			})
		})
	})
})

func ingressWithLoadBalancerStatus(status v1.LoadBalancerIngress) *networkingv1.Ingress {
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         "test-ns",
			Name:              "test",
			DeletionTimestamp: nil,
		},
		Status: networkingv1.IngressStatus{
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

var _ clientnetworkingv1.IngressInterface = &fakeIngressClient{}

type fakeIngressClient struct {
	mock.Mock
}

func (f *fakeIngressClient) Create(ctx context.Context, ingress *networkingv1.Ingress, options metav1.CreateOptions) (*networkingv1.Ingress, error) {
	r := f.Called(ctx, ingress, options)
	return r.Get(0).(*networkingv1.Ingress), r.Error(1)
}

func (f *fakeIngressClient) Update(ctx context.Context, ingress *networkingv1.Ingress, options metav1.UpdateOptions) (*networkingv1.Ingress, error) {
	r := f.Called(ctx, ingress, options)
	return r.Get(0).(*networkingv1.Ingress), r.Error(1)
}

func (f *fakeIngressClient) UpdateStatus(ctx context.Context, ingress *networkingv1.Ingress, options metav1.UpdateOptions) (*networkingv1.Ingress, error) {
	r := f.Called(ctx, ingress, options)
	return r.Get(0).(*networkingv1.Ingress), r.Error(1)
}

func (f *fakeIngressClient) Delete(ctx context.Context, name string, options metav1.DeleteOptions) error {
	r := f.Called(ctx, name, options)
	return r.Error(0)
}

func (f *fakeIngressClient) DeleteCollection(ctx context.Context, deleteOptions metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	r := f.Called(ctx, deleteOptions, listOptions)
	return r.Error(0)
}

func (f *fakeIngressClient) Get(ctx context.Context, name string, options metav1.GetOptions) (*networkingv1.Ingress, error) {
	r := f.Called(ctx, name, options)
	return r.Get(0).(*networkingv1.Ingress), nil
}

func (f *fakeIngressClient) List(ctx context.Context, options metav1.ListOptions) (*networkingv1.IngressList, error) {
	r := f.Called(ctx, options)
	return r.Get(0).(*networkingv1.IngressList), r.Error(1)
}

func (f *fakeIngressClient) Watch(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
	r := f.Called(ctx, options)
	return r.Get(0).(watch.Interface), r.Error(1)
}

func (f *fakeIngressClient) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (result *networkingv1.Ingress, err error) {
	r := f.Called(ctx, name, pt, data, options, subresources)
	return r.Get(0).(*networkingv1.Ingress), r.Error(1)
}

func (f *fakeIngressClient) Apply(ctx context.Context, ingress *networkingv1apply.IngressApplyConfiguration, options metav1.ApplyOptions) (result *networkingv1.Ingress, err error) {
	r := f.Called(ctx, ingress, options)
	return r.Get(0).(*networkingv1.Ingress), r.Error(1)
}

func (f *fakeIngressClient) ApplyStatus(ctx context.Context, ingress *networkingv1apply.IngressApplyConfiguration, options metav1.ApplyOptions) (result *networkingv1.Ingress, err error) {
	r := f.Called(ctx, ingress, options)
	return r.Get(0).(*networkingv1.Ingress), r.Error(1)
}

var _ clientnetworkingv1.IngressesGetter = &stubIngressGetter{}

type stubIngressGetter struct {
	ingressClient clientnetworkingv1.IngressInterface
}

func (s *stubIngressGetter) Ingresses(namespace string) clientnetworkingv1.IngressInterface {
	return s.ingressClient
}
