package k8s

import (
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/sky-uk/feed/k8s/mocks"
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
				namespaces[0] = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
					Name: "matching-namespace",
					Labels: map[string]string{
						"some-label-name": "some-value",
						"team":            "some-team-name",
					},
				}}
				namespaces[1] = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
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
				namespaces[0] = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
					Name: "matching-namespace-one",
					Labels: map[string]string{
						"some-label-name": "some-value",
						"team":            "some-team-1",
					},
				}}
				namespaces[1] = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
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
			servicesInStore := []*corev1.Service{{}}
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
		var mockController *gomock.Controller
		var ingressClient *mocks.MockIngressInterface
		var unitUnderTest Client

		BeforeEach(func() {
			mockController = gomock.NewController(GinkgoT())
			ingressClient = mocks.NewMockIngressInterface(mockController)

			getter := mocks.NewMockIngressesGetter(mockController)
			getter.EXPECT().Ingresses(gomock.Any()).Return(ingressClient).AnyTimes()
			unitUnderTest = &client{
				ingressGetter: getter,
			}
		})
		AfterEach(func() {
			mockController.Finish()
		})

		Context("another feed pod has made a conflicting update", func() {
			It("should not report an error if the desired change has already been applied", func() {
				// given
				currentIngress := ingressWithLoadBalancerStatus(corev1.LoadBalancerIngress{Hostname: "old-host"})
				newIngress := ingressWithLoadBalancerStatus(corev1.LoadBalancerIngress{Hostname: "new-host"})
				independentlyUpdatedIngress := ingressWithLoadBalancerStatus(corev1.LoadBalancerIngress{Hostname: "new-host"})

				ingressClient.EXPECT().Get(gomock.Any(), "test", metav1.GetOptions{}).
					Return(currentIngress, nil).
					Times(1)
				ingressClient.EXPECT().UpdateStatus(gomock.Any(), newIngress, gomock.Any()).
					Return(currentIngress, errors.NewApplyConflict([]metav1.StatusCause{}, "conflict"))
				ingressClient.EXPECT().Get(gomock.Any(), "test", metav1.GetOptions{}).
					Return(independentlyUpdatedIngress, nil)

				// when
				err := unitUnderTest.UpdateIngressStatus(newIngress)

				// then
				Expect(err).ToNot(HaveOccurred())
			})

			It("should report an error if the desired change has not been applied", func() {
				// given
				currentIngress := ingressWithLoadBalancerStatus(corev1.LoadBalancerIngress{Hostname: "old-host"})
				newIngress := ingressWithLoadBalancerStatus(corev1.LoadBalancerIngress{Hostname: "new-host"})
				independentlyUpdatedIngress := ingressWithLoadBalancerStatus(corev1.LoadBalancerIngress{Hostname: "other-host"})

				ingressClient.EXPECT().Get(gomock.Any(), "test", metav1.GetOptions{}).
					Return(currentIngress, nil).
					Times(1)
				ingressClient.EXPECT().UpdateStatus(gomock.Any(), newIngress, gomock.Any()).
					Return(currentIngress, errors.NewApplyConflict([]metav1.StatusCause{}, "conflict"))
				ingressClient.EXPECT().Get(gomock.Any(), "test", metav1.GetOptions{}).
					Return(independentlyUpdatedIngress, nil)

				// when
				err := unitUnderTest.UpdateIngressStatus(newIngress)

				// then
				Expect(err).To(HaveOccurred())
			})
		})

		Context("the update fails for another reason", func() {
			It("should report the error directly", func() {
				// given
				currentIngress := ingressWithLoadBalancerStatus(corev1.LoadBalancerIngress{Hostname: "old-host"})
				newIngress := ingressWithLoadBalancerStatus(corev1.LoadBalancerIngress{Hostname: "new-host"})

				ingressClient.EXPECT().Get(gomock.Any(), "test", metav1.GetOptions{}).
					Return(currentIngress, nil).
					Times(1)
				ingressClient.EXPECT().UpdateStatus(gomock.Any(), newIngress, gomock.Any()).
					Return(currentIngress, errors.NewServiceUnavailable("unavailable"))

				// when
				err := unitUnderTest.UpdateIngressStatus(newIngress)

				// then
				Expect(err).To(HaveOccurred())
			})
		})
	})
})

func ingressWithLoadBalancerStatus(status corev1.LoadBalancerIngress) *networkingv1.Ingress {
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         "test-ns",
			Name:              "test",
			DeletionTimestamp: nil,
		},
		Status: networkingv1.IngressStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{status},
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
