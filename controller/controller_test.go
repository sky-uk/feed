package controller

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sky-uk/feed/k8s"
	fake "github.com/sky-uk/feed/util/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const smallWaitTime = time.Millisecond * 50
const defaultIngressClass = "main"

type fakeUpdater struct {
	mock.Mock
}

func (lb *fakeUpdater) Update(update IngressEntries) error {
	r := lb.Called(update)
	return r.Error(0)
}

var started []*fakeUpdater

func (lb *fakeUpdater) Start() error {
	started = append(started, lb)
	r := lb.Called()
	return r.Error(0)
}

var stopped []*fakeUpdater

func (lb *fakeUpdater) Stop() error {
	stopped = append(stopped, lb)
	r := lb.Called()
	return r.Error(0)
}

func (lb *fakeUpdater) Health() error {
	r := lb.Called()
	return r.Error(0)
}

func (lb *fakeUpdater) Readiness() error {
	r := lb.Called()
	return r.Error(0)
}

func (lb *fakeUpdater) String() string {
	return "FakeUpdater"
}

type fakeWatcher struct {
	mock.Mock
}

func (w *fakeWatcher) Updates() <-chan interface{} {
	r := w.Called()
	return r.Get(0).(<-chan interface{})
}

func createFakeWatcher() (*fakeWatcher, chan interface{}) {
	watcher := new(fakeWatcher)
	updateCh := make(chan interface{}, 1)
	watcher.On("Updates").Return((<-chan interface{})(updateCh))
	return watcher, updateCh
}

func createDefaultStubs() (*fakeUpdater, *fake.FakeClient) {
	updater := new(fakeUpdater)
	client := new(fake.FakeClient)
	ingressWatcher, _ := createFakeWatcher()
	serviceWatcher, _ := createFakeWatcher()
	namespaceWatcher, _ := createFakeWatcher()

	client.On("GetAllIngresses").Return([]*networkingv1.Ingress{}, nil)
	client.On("GetIngresses", mock.Anything, mock.AnythingOfType("bool")).Return([]*networkingv1.Ingress{}, nil)
	client.On("GetServices").Return([]*corev1.Service{}, nil)
	client.On("WatchIngresses").Return(ingressWatcher)
	client.On("WatchServices").Return(serviceWatcher)
	client.On("WatchNamespaces").Return(namespaceWatcher)
	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Update", mock.Anything).Return(nil)
	updater.On("Health").Return(nil)
	updater.On("Readiness").Return(nil)

	return updater, client
}

func newController(lb Updater, client k8s.Client) Controller {
	return New(Config{
		Updaters:                     []Updater{lb},
		KubernetesClient:             client,
		DefaultAllow:                 ingressDefaultAllow,
		DefaultBackendTimeoutSeconds: backendTimeout,
	}, make(chan struct{}))
}

func TestControllerCanBeStartedAndStopped(t *testing.T) {
	asserter := assert.New(t)
	updater, client := createDefaultStubs()
	controller := newController(updater, client)

	asserter.NoError(controller.Start())
	asserter.NoError(controller.Stop())
	updater.AssertCalled(t, "Start")
	updater.AssertCalled(t, "Stop")
}

func TestControllerStartsAndStopsUpdatersInCorrectOrder(t *testing.T) {
	// given
	asserter := assert.New(t)
	updater1 := new(fakeUpdater)
	updater1.TestData().Set("name", "updater1")
	updater1.On("Start").Return(nil)
	updater1.On("Stop").Return(nil)

	updater2 := new(fakeUpdater)
	updater2.TestData().Set("name", "updater2")
	updater2.On("Start").Return(nil)
	updater2.On("Stop").Return(nil)

	_, client := createDefaultStubs()
	controller := New(Config{
		Updaters:                     []Updater{updater1, updater2},
		KubernetesClient:             client,
		DefaultAllow:                 ingressDefaultAllow,
		DefaultBackendTimeoutSeconds: backendTimeout,
	}, make(chan struct{}))

	// when
	started = nil
	stopped = nil
	asserter.NoError(controller.Start())
	asserter.NoError(controller.Stop())

	// then
	asserter.Equal(started, []*fakeUpdater{updater1, updater2}, "should start in order")
	asserter.Equal(stopped, []*fakeUpdater{updater2, updater1}, "should stop in reverse order")
}

func TestControllerStopsAnyStartedUpdatersIfOneFailsToStart(t *testing.T) {
	// given
	asserter := assert.New(t)
	updater1 := new(fakeUpdater)
	updater1.On("Start").Return(nil)
	updater1.On("Stop").Return(nil)

	updater2 := new(fakeUpdater)
	updater2.TestData().Set("name", "updater2")
	updater2.On("Start").Return(errors.New("kaboom"))

	_, client := createDefaultStubs()
	controller := New(Config{
		Updaters:                     []Updater{updater1, updater2},
		KubernetesClient:             client,
		DefaultAllow:                 ingressDefaultAllow,
		DefaultBackendTimeoutSeconds: backendTimeout,
	}, make(chan struct{}))

	// when
	asserter.Error(controller.Start())

	// then
	updater1.AssertExpectations(t)
	updater2.AssertExpectations(t)
}

func TestControllerCannotBeRestarted(t *testing.T) {
	// given
	asserter := assert.New(t)
	controller := newController(createDefaultStubs())

	// and
	asserter.NoError(controller.Start())
	asserter.NoError(controller.Stop())

	// then
	asserter.Error(controller.Start())
	asserter.Error(controller.Stop())
}

func TestControllerStartCannotBeCalledTwice(t *testing.T) {
	// given
	asserter := assert.New(t)
	controller := newController(createDefaultStubs())

	// expect
	asserter.NoError(controller.Start())
	asserter.Error(controller.Start())
	asserter.NoError(controller.Stop())
}

func TestControllerIsUnhealthyUntilStarted(t *testing.T) {
	// given
	asserter := assert.New(t)
	controller := newController(createDefaultStubs())

	// expect
	asserter.Error(controller.Health(), "should be unhealthy until started")
	asserter.NoError(controller.Start())
	time.Sleep(smallWaitTime)
	asserter.NoError(controller.Health(), "should be healthy after started")
	asserter.NoError(controller.Stop())
	time.Sleep(smallWaitTime)
	asserter.Error(controller.Health(), "should be unhealthy after stopped")
}

func TestControllerIsUnreadyUntilStarted(t *testing.T) {
	// given
	asserter := assert.New(t)
	controller := newController(createDefaultStubs())

	// expect
	asserter.Error(controller.Readiness(), "should be unready until started")
	asserter.NoError(controller.Start())
	time.Sleep(smallWaitTime)
	asserter.NoError(controller.Readiness(), "should be ready after started")
	asserter.NoError(controller.Stop())
	time.Sleep(smallWaitTime)
	asserter.Error(controller.Readiness(), "should be unready after stopped")
}

func TestControllerIsUnhealthyIfUpdaterIsUnhealthy(t *testing.T) {
	asserter := assert.New(t)
	_, client := createDefaultStubs()
	updater := new(fakeUpdater)
	controller := newController(updater, client)

	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Update", mock.Anything).Return(nil)
	// first return healthy, then unhealthy for lb
	updater.On("Health").Return(nil).Once()
	lbErr := fmt.Errorf("FakeUpdater: dead")
	updater.On("Health").Return(fmt.Errorf("dead")).Once()

	asserter.NoError(controller.Start())
	asserter.NoError(controller.Health())
	asserter.Equal(lbErr, controller.Health())

	_ = controller.Stop()
}

func TestControllerIsUnreadyIfUpdaterIsUnready(t *testing.T) {
	asserter := assert.New(t)
	_, client := createDefaultStubs()
	updater := new(fakeUpdater)
	controller := newController(updater, client)

	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Update", mock.Anything).Return(nil)
	// first return healthy, then unhealthy for lb
	updater.On("Health").Return(nil)
	updater.On("Readiness").Return(nil).Once()
	lbErr := fmt.Errorf("FakeUpdater: dead")
	updater.On("Readiness").Return(fmt.Errorf("dead")).Once()

	asserter.NoError(controller.Start())
	asserter.NoError(controller.Readiness())
	asserter.Equal(lbErr, controller.Readiness())

	_ = controller.Stop()
}

func TestControllerReturnsErrorIfUpdaterFails(t *testing.T) {
	// given
	_, client := createDefaultStubs()
	updater := new(fakeUpdater)
	controller := newController(updater, client)
	updater.On("Start").Return(fmt.Errorf("kaboooom"))
	updater.On("Stop").Return(nil)

	// when
	assert.Error(t, controller.Start())
}

func TestUnhealthyIfUpdaterFails(t *testing.T) {
	// given
	asserter := assert.New(t)
	updater := new(fakeUpdater)
	client := new(fake.FakeClient)
	controller := newController(updater, client)

	ingressWatcher, updateCh := createFakeWatcher()
	serviceWatcher, _ := createFakeWatcher()
	namespaceWatcher, _ := createFakeWatcher()

	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Update", mock.Anything).Return(nil).Once()
	updater.On("Update", mock.Anything).Return(fmt.Errorf("kaboom, update failed :(")).Once()
	updater.On("Health").Return(nil)

	client.On("GetAllIngresses").Return(createDefaultIngresses(), nil)
	client.On("GetServices").Return(createDefaultServices(), nil)
	client.On("WatchIngresses").Return(ingressWatcher)
	client.On("WatchServices").Return(serviceWatcher)
	client.On("WatchNamespaces").Return(namespaceWatcher)
	asserter.NoError(controller.Start())

	// expect
	updateCh <- struct{}{}
	time.Sleep(smallWaitTime)
	asserter.NoError(controller.Health())

	updateCh <- struct{}{}
	time.Sleep(smallWaitTime)
	asserter.Error(controller.Health())

	// cleanup
	_ = controller.Stop()
}

func defaultConfig() Config {
	return Config{
		DefaultAllow:                 ingressDefaultAllow,
		DefaultBackendTimeoutSeconds: backendTimeout,
		Name:                         defaultIngressClass,
	}
}

type testSpec struct {
	description string
	ingresses   []*networkingv1.Ingress
	services    []*corev1.Service
	namespaces  []*corev1.Namespace
	entries     IngressEntries
	config      Config
}

func TestUpdaterIsUpdatedForIngressTaggedWithSkyFrontendScheme(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress tagged with sky.uk/frontend-scheme",
		createIngressesFromNonELBAnnotation(false),
		createDefaultServices(),
		createDefaultNamespaces(),
		createLbEntriesFixture(),
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressTaggedWithLegacySkyFrontendScheme(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress tagged with legacy sky.uk/frontend-elb-scheme",
		createIngressesFromNonELBAnnotation(true),
		createDefaultServices(),
		createDefaultNamespaces(),
		createLbEntriesFixture(),
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithCorrespondingService(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with corresponding service",
		createDefaultIngresses(),
		createDefaultServices(),
		createDefaultNamespaces(),
		createLbEntriesFixture(),
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithExtraServices(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with extra services",
		createDefaultIngresses(),
		append(createDefaultServices(),
			createServiceFixture("another one", ingressNamespace, serviceIP)...),
		createDefaultNamespaces(),
		createLbEntriesFixture(),
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithoutCorrespondingService(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress without corresponding service",
		createDefaultIngresses(),
		[]*corev1.Service{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-default-name",
					Namespace: "non-default-namespace",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "non-default-ip",
				},
			},
		},
		createDefaultNamespaces(),
		nil,
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForServiceWithNonMatchingNamespace(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with service with non-matching namespace",
		createDefaultIngresses(),
		createServiceFixture(ingressSvcName, "lalala land", serviceIP),
		createDefaultNamespaces(),
		nil,
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForServiceWithNonMatchingName(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with service with non-matching name",
		createDefaultIngresses(),
		createServiceFixture("lalala service", ingressNamespace, serviceIP),
		createDefaultNamespaces(),
		nil,
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithMissingHostName(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with missing host name",
		createIngressesFixture(ingressNamespace, "", ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   ingressAllow,
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		nil,
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithMissingServiceName(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with missing service name",
		createIngressesFixture(ingressNamespace, ingressHost, "", ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   ingressAllow,
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		nil,
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithMissingServicePort(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with missing service port",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, 0, map[string]string{
			ingressAllowAnnotation:   ingressAllow,
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		nil,
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithMissingServiceIP(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with missing service IP",
		createDefaultIngresses(),
		createServiceFixture(ingressSvcName, ingressNamespace, ""),
		createDefaultNamespaces(),
		nil,
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithNoneAsServiceIP(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with 'None' as service IP",
		createDefaultIngresses(),
		createServiceFixture(ingressSvcName, ingressNamespace, "None"),
		createDefaultNamespaces(),
		nil,
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithDefaultAllow(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with default allow",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			backendTimeoutSeconds:  "10",
			ingressClassAnnotation: defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			Allow:                 strings.Split(ingressDefaultAllow, ","),
			IngressClass:          defaultIngressClass,
			BackendTimeoutSeconds: backendTimeout,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithEmptyAllow(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with empty allow",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "",
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{},
			BackendTimeoutSeconds: backendTimeout,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithAllowContainingWhitespace(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with empty allow",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "127.0.0.1, 192.168.0.1,\n10.1.2.3",
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{"127.0.0.1", "192.168.0.1", "10.1.2.3"},
			BackendTimeoutSeconds: backendTimeout,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithStripPathsTrue(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with strip paths set to true",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "",
			stripPathAnnotation:      "true",
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{},
			StripPaths:            true,
			BackendTimeoutSeconds: backendTimeout,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithStripPathsFalse(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with strip paths set to false",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "",
			stripPathAnnotation:      "false",
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: backendTimeout,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithExactPathTrue(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with exact path set to true",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "",
			exactPathAnnotation:      "true",
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{},
			ExactPath:             true,
			BackendTimeoutSeconds: backendTimeout,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithExactPathFalse(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with exact path set to false",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "",
			exactPathAnnotation:      "false",
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{},
			ExactPath:             false,
			BackendTimeoutSeconds: backendTimeout,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithOverriddenBackendTimeout(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with overridden backend timeout",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "",
			stripPathAnnotation:      "false",
			backendTimeoutSeconds:    "20",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: 20,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithOverriddenLegacyBackendTimeout(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with overridden backend timeout",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:        "",
			stripPathAnnotation:           "false",
			legacyBackendKeepaliveSeconds: "20",
			frontendSchemeAnnotation:      "internal",
			ingressClassAnnotation:        defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: 20,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithDefaultBackendTimeout(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with default backend timeout",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "",
			stripPathAnnotation:      "false",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: backendTimeout,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithOverriddenBackendMaxConnections(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with overridden backend max connections",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "",
			stripPathAnnotation:      "false",
			backendTimeoutSeconds:    "20",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   defaultIngressClass,
			backendMaxConnections:    "512",
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: 20,
			BackendMaxConnections: 512,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithOverriddenBackendMaxRequestsPerConnection(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with overridden backend max requests per connection",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:          "",
			stripPathAnnotation:             "false",
			frontendSchemeAnnotation:        "internal",
			ingressClassAnnotation:          defaultIngressClass,
			backendMaxRequestsPerConnection: "100",
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:                       ingressNamespace,
			Name:                            ingressName,
			Host:                            ingressHost,
			Path:                            ingressPath,
			ServiceAddress:                  serviceIP,
			ServicePort:                     ingressSvcPort,
			LbScheme:                        "internal",
			IngressClass:                    defaultIngressClass,
			Allow:                           []string{},
			StripPaths:                      false,
			BackendTimeoutSeconds:           10,
			BackendMaxConnections:           defaultMaxConnections,
			BackendMaxRequestsPerConnection: 100,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithOverriddenBackendConnectionKeepAlive(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with overridden backend max requests per connection",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:     "",
			stripPathAnnotation:        "false",
			frontendSchemeAnnotation:   "internal",
			ingressClassAnnotation:     defaultIngressClass,
			backendConnectionKeepalive: "5m",
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:               ingressNamespace,
			Name:                    ingressName,
			Host:                    ingressHost,
			Path:                    ingressPath,
			ServiceAddress:          serviceIP,
			ServicePort:             ingressSvcPort,
			LbScheme:                "internal",
			IngressClass:            defaultIngressClass,
			Allow:                   []string{},
			StripPaths:              false,
			BackendTimeoutSeconds:   10,
			BackendMaxConnections:   defaultMaxConnections,
			BackendKeepaliveTimeout: 5 * time.Minute,
		}},
		defaultConfig(),
	})
}

func TestUpdaterSkipsEntriesForIngressWithInvalidBackendConnectionKeepAliveAndBackendMaxRequestsPerConnection(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with overridden backend max requests per connection",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:          "",
			stripPathAnnotation:             "false",
			frontendSchemeAnnotation:        "internal",
			ingressClassAnnotation:          defaultIngressClass,
			backendConnectionKeepalive:      "50a",
			backendMaxRequestsPerConnection: "-1",
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: 10,
			BackendMaxConnections: defaultMaxConnections,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithDefaultBackendMaxConnections(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with default backend max connections",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "",
			stripPathAnnotation:      "false",
			backendTimeoutSeconds:    "20",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: 20,
			BackendMaxConnections: defaultMaxConnections,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithDefaultProxyBufferValuesWhenNotOverridden(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with default proxy buffer values when not overridden by the ingress definition",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation: "",
			ingressClassAnnotation: defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: 10,
			BackendMaxConnections: defaultMaxConnections,
			ProxyBufferSize:       2,
			ProxyBufferBlocks:     3,
		}},
		Config{
			DefaultBackendTimeoutSeconds: backendTimeout,
			DefaultProxyBufferSize:       2,
			DefaultProxyBufferBlocks:     3,
			Name:                         defaultIngressClass,
		},
	})
}

func TestUpdaterIsUpdatedForIngressOverridesDefaultProxyBufferValues(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress definition overrides default proxy buffer values",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:      "",
			proxyBufferSizeAnnotation:   "6",
			proxyBufferBlocksAnnotation: "4",
			ingressClassAnnotation:      defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: 10,
			BackendMaxConnections: defaultMaxConnections,
			ProxyBufferSize:       6,
			ProxyBufferBlocks:     4,
		}},
		Config{
			DefaultBackendTimeoutSeconds: backendTimeout,
			DefaultProxyBufferSize:       2,
			DefaultProxyBufferBlocks:     3,
			Name:                         defaultIngressClass,
		},
	})
}

func TestUpdaterIsUpdatedForIngressDefinitionResetsToMacWhenProxyBufferValuesExceedMax(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress definition resets to max when proxy buffer values exceed max allowed values",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:      "",
			proxyBufferSizeAnnotation:   "64",
			proxyBufferBlocksAnnotation: "12",
			ingressClassAnnotation:      defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: 10,
			BackendMaxConnections: defaultMaxConnections,
			ProxyBufferSize:       32,
			ProxyBufferBlocks:     8,
		}},
		Config{
			DefaultBackendTimeoutSeconds: backendTimeout,
			DefaultProxyBufferSize:       2,
			DefaultProxyBufferBlocks:     3,
			Name:                         defaultIngressClass,
		},
	})
}

func TestUpdaterIsUpdatedForIngressClassNotSetInIngress(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress without ingress class set",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "",
			stripPathAnnotation:      "false",
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressClass:          defaultIngressClass,
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: backendTimeout,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressClassSetToDefaultInIngress(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with class set to default value",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "",
			stripPathAnnotation:      "false",
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   defaultIngressClass,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: backendTimeout,
			IngressClass:          defaultIngressClass,
		}},
		Config{
			DefaultAllow:                 ingressDefaultAllow,
			DefaultBackendTimeoutSeconds: backendTimeout,
			Name:                         defaultIngressClass,
		},
	})
}

func TestUpdaterIsNotUpdatedForIngressClassSetToTestAndConfigSetToDefault(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress requesting class==test; feed instance has class " + defaultIngressClass,
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "",
			stripPathAnnotation:      "false",
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   "test",
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		nil,
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedWhenIncludingClasslessIngresses(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress has no class annotation; feed-ingress is including classless ingresses",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "",
			stripPathAnnotation:      "false",
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: backendTimeout,
			IngressClass:          "",
		}},
		Config{
			DefaultAllow:                 ingressDefaultAllow,
			DefaultBackendTimeoutSeconds: backendTimeout,
			Name:                         defaultIngressClass,
			IncludeClasslessIngresses:    true,
		},
	})
}

func TestUpdaterIsNotUpdatedWhenExcludingClasslessIngresses(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress has no class annotation; feed-ingress is excluding classless ingresses",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "",
			stripPathAnnotation:      "false",
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		nil,
		Config{
			DefaultAllow:                 ingressDefaultAllow,
			DefaultBackendTimeoutSeconds: backendTimeout,
			Name:                         defaultIngressClass,
			IncludeClasslessIngresses:    false,
		},
	})
}

func TestUpdaterIsUpdatedForIngressClassSetToTestInIngressAndConfig(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress requesting class==test; feed has class test",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:   "",
			stripPathAnnotation:      "false",
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
			ingressClassAnnotation:   "test",
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: backendTimeout,
			IngressClass:          "test",
		}},
		Config{
			DefaultAllow:                 ingressDefaultAllow,
			DefaultBackendTimeoutSeconds: backendTimeout,
			Name:                         "test",
		},
	})
}

func TestNamespaceSelectorsIsUsedToGetIngresses(t *testing.T) {
	asserter := assert.New(t)

	client := new(fake.FakeClient)
	updater := new(fakeUpdater)

	config := Config{
		DefaultAllow:                 ingressDefaultAllow,
		DefaultBackendTimeoutSeconds: backendTimeout,
		Name:                         defaultIngressClass,
		NamespaceSelectors:           []*k8s.NamespaceSelector{{LabelName: "team", LabelValue: "theteam"}},
		MatchAllNamespaceSelectors:   false,
	}

	config.KubernetesClient = client
	config.Updaters = []Updater{updater}

	controller := New(config, make(chan struct{}))

	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Health").Return(nil)

	// The purpose of this test is to ensure that the NamespaceSelectors is passed to GetIngresses
	client.On("GetIngresses", config.NamespaceSelectors, config.MatchAllNamespaceSelectors).Return([]*networkingv1.Ingress{}, nil)

	ingressWatcher, ingressCh := createFakeWatcher()
	serviceWatcher, serviceCh := createFakeWatcher()
	namespaceWatcher, namespaceCh := createFakeWatcher()
	client.On("WatchIngresses").Return(ingressWatcher)
	client.On("WatchServices").Return(serviceWatcher)
	client.On("WatchNamespaces").Return(namespaceWatcher)

	asserter.NoError(controller.Start())
	ingressCh <- struct{}{}
	serviceCh <- struct{}{}
	namespaceCh <- struct{}{}
	time.Sleep(smallWaitTime)

	asserter.EqualError(controller.Health(), "updates failed to apply: found 0 ingresses")
	asserter.NoError(controller.Stop())
	time.Sleep(smallWaitTime)
	updater.AssertExpectations(t)
	client.AssertExpectations(t)
}

func TestUpdaterIsUpdatedForIngressWithoutHostDefinition(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress without host definition",
		createIngressesFixture(ingressNamespace, "", ingressSvcName, ingressSvcPort, map[string]string{
			ingressClassAnnotation: defaultIngressClass,
		}, ""),
		createServiceFixture(ingressSvcName, "lalala land", serviceIP),
		createDefaultNamespaces(),
		nil,
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithoutPathDefinition(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress without path definition",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressClassAnnotation: defaultIngressClass,
		}, ""),
		createServiceFixture(ingressSvcName, "lalala land", serviceIP),
		createDefaultNamespaces(),
		nil,
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithoutRulesDefinition(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress without rules definition",
		createIngressWithoutRules(),
		createServiceFixture(ingressSvcName, "lalala land", serviceIP),
		createDefaultNamespaces(),
		nil,
		defaultConfig(),
	})
}

type clientExpectation func(client *fake.FakeClient, ingresses []*networkingv1.Ingress)

var expectGetAllIngresses = func(client *fake.FakeClient, ingresses []*networkingv1.Ingress) {
	client.On("GetAllIngresses").Return(ingresses, nil)
}

var expectGetIngresses = func(client *fake.FakeClient, ingresses []*networkingv1.Ingress) {
	client.On("GetIngresses", &k8s.NamespaceSelector{LabelName: "team", LabelValue: "theteam"}).Return(ingresses, nil)
}

func runAndAssertUpdates(t *testing.T, clientExpectation clientExpectation, test testSpec) {
	//given
	asserter := assert.New(t)

	fmt.Printf("test: %s\n", test.description)
	// add ingress pointers to entries
	test.entries = addIngresses(test.ingresses, test.entries)

	// setup clients
	client := new(fake.FakeClient)
	updater := new(fakeUpdater)

	config := test.config

	config.KubernetesClient = client
	config.Updaters = []Updater{updater}

	controller := New(config, make(chan struct{}))

	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	// once for each update: ingress, service, namespace
	updater.On("Update", test.entries).Return(nil).Times(3)

	clientExpectation(client, test.ingresses)
	client.On("GetServices").Return(test.services, nil)

	ingressWatcher, ingressCh := createFakeWatcher()
	serviceWatcher, serviceCh := createFakeWatcher()
	namespaceWatcher, namespaceCh := createFakeWatcher()
	client.On("WatchIngresses").Return(ingressWatcher)
	client.On("WatchServices").Return(serviceWatcher)
	client.On("WatchNamespaces").Return(namespaceWatcher)

	//when
	asserter.NoError(controller.Start())
	ingressCh <- struct{}{}
	serviceCh <- struct{}{}
	namespaceCh <- struct{}{}
	time.Sleep(smallWaitTime)

	//then
	asserter.NoError(controller.Stop())
	time.Sleep(smallWaitTime)
	updater.AssertExpectations(t)
	client.AssertExpectations(t)
}

func addIngresses(ingresses []*networkingv1.Ingress, entries IngressEntries) IngressEntries {
	if len(ingresses) != len(entries) {
		return entries
	}
	appendedEntries := IngressEntries{}
	for i, entry := range entries {
		entry.Ingress = ingresses[i]
		appendedEntries = append(appendedEntries, entry)
	}
	return appendedEntries
}

func createLbEntriesFixture() IngressEntries {
	return []IngressEntry{{
		Namespace:             ingressNamespace,
		Name:                  ingressName,
		Host:                  ingressHost,
		Path:                  ingressPath,
		ServiceAddress:        serviceIP,
		ServicePort:           ingressSvcPort,
		Allow:                 strings.Split(ingressAllow, ","),
		LbScheme:              lbScheme,
		IngressClass:          defaultIngressClass,
		BackendTimeoutSeconds: backendTimeout,
	}}
}

const (
	ingressHost           = "foo.sky.com"
	ingressPath           = "/foo"
	ingressName           = "foo-ingress"
	ingressSvcName        = "foo-svc"
	ingressSvcPort        = 80
	ingressNamespace      = "happysky"
	ingressAllow          = "10.82.0.0/16,10.44.0.0/16"
	ingressDefaultAllow   = "10.50.0.0/16,10.1.0.0/16"
	serviceIP             = "10.254.0.82"
	lbScheme              = "internal"
	backendTimeout        = 10
	defaultMaxConnections = 0
)

func createDefaultIngresses() []*networkingv1.Ingress {
	return createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
		ingressAllowAnnotation:   ingressAllow,
		backendTimeoutSeconds:    "10",
		frontendSchemeAnnotation: "internal",
		ingressClassAnnotation:   defaultIngressClass,
	}, ingressPath)
}

func createIngressesFromNonELBAnnotation(legacyAnnotations bool) []*networkingv1.Ingress {
	var timeoutAnnotation, schemeAnnotation string
	if legacyAnnotations {
		timeoutAnnotation = legacyBackendKeepaliveSeconds
		schemeAnnotation = legacyFrontendElbSchemeAnnotation
	} else {
		timeoutAnnotation = backendTimeoutSeconds
		schemeAnnotation = frontendSchemeAnnotation
	}

	return createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
		ingressAllowAnnotation: ingressAllow,
		timeoutAnnotation:      "10",
		schemeAnnotation:       "internal",
		ingressClassAnnotation: defaultIngressClass,
	}, ingressPath)
}

func createIngressWithoutRules() []*networkingv1.Ingress {
	return []*networkingv1.Ingress{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ingressName,
				Namespace: ingressNamespace,
				Annotations: map[string]string{
					ingressClassAnnotation: defaultIngressClass,
				},
			},
			Spec: networkingv1.IngressSpec{},
		},
	}

}

func createIngressesFixture(namespace string, host string, serviceName string, servicePort int, ingressAnnotations map[string]string, path string) []*networkingv1.Ingress {

	pathType := networkingv1.PathTypeImplementationSpecific
	paths := []networkingv1.HTTPIngressPath{{
		Path:     path,
		PathType: &pathType,
		Backend: networkingv1.IngressBackend{
			Service: &networkingv1.IngressServiceBackend{
				Name: serviceName,
				Port: networkingv1.ServiceBackendPort{
					Name:   "",
					Number: int32(servicePort),
				},
			},
		},
	}}

	annotations := make(map[string]string)

	for annotationName, annotationVal := range ingressAnnotations {
		switch annotationName {
		case ingressAllowAnnotation:
			annotations[ingressAllowAnnotation] = annotationVal
		case stripPathAnnotation:
			annotations[stripPathAnnotation] = annotationVal
		case exactPathAnnotation:
			annotations[exactPathAnnotation] = annotationVal
		case legacyFrontendElbSchemeAnnotation:
			annotations[legacyFrontendElbSchemeAnnotation] = annotationVal
		case frontendSchemeAnnotation:
			annotations[frontendSchemeAnnotation] = annotationVal
		case legacyBackendKeepaliveSeconds:
			annotations[legacyBackendKeepaliveSeconds] = annotationVal
		case backendTimeoutSeconds:
			annotations[backendTimeoutSeconds] = annotationVal
		case backendMaxConnections:
			annotations[backendMaxConnections] = annotationVal
		case proxyBufferSizeAnnotation:
			annotations[proxyBufferSizeAnnotation] = annotationVal
		case proxyBufferBlocksAnnotation:
			annotations[proxyBufferBlocksAnnotation] = annotationVal
		case ingressClassAnnotation:
			annotations[ingressClassAnnotation] = annotationVal
		case backendConnectionKeepalive:
			annotations[backendConnectionKeepalive] = annotationVal
		case backendMaxRequestsPerConnection:
			annotations[backendMaxRequestsPerConnection] = annotationVal
		}
	}

	ingressDefinition := createIngressWithoutRules()
	var ingressRules []networkingv1.IngressRule

	if host != "" {
		ingressRuleValue := networkingv1.IngressRuleValue{}
		if path != "" {
			ingressRuleValue.HTTP = &networkingv1.HTTPIngressRuleValue{Paths: paths}
		}
		ingressRule := networkingv1.IngressRule{
			Host:             host,
			IngressRuleValue: ingressRuleValue,
		}

		ingressRules = append(ingressRules, ingressRule)
	}

	ingressDefinition[0].ObjectMeta.Namespace = namespace
	ingressDefinition[0].ObjectMeta.Annotations = annotations
	ingressDefinition[0].Spec.Rules = ingressRules

	return ingressDefinition
}

func createDefaultServices() []*corev1.Service {
	return createServiceFixture(ingressSvcName, ingressNamespace, serviceIP)
}

func createServiceFixture(name string, namespace string, clusterIP string) []*corev1.Service {
	return []*corev1.Service{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: corev1.ServiceSpec{
				ClusterIP: clusterIP,
			},
		},
	}
}

func createDefaultNamespaces() []*corev1.Namespace {
	return createNamespaceFixture(ingressNamespace, map[string]string{})
}

func createNamespaceFixture(name string, labels map[string]string) []*corev1.Namespace {
	return []*corev1.Namespace{
		{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Namespace",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: labels,
			},
			Spec:   corev1.NamespaceSpec{},
			Status: corev1.NamespaceStatus{},
		},
	}
}

func TestUpdateFailsWhenK8sClientReturnsNoIngresses(t *testing.T) {
	test := testSpec{
		"ingress without rules definition",
		createDefaultIngresses(),
		createDefaultServices(),
		createDefaultNamespaces(),
		createLbEntriesFixture(),
		defaultConfig(),
	}

	asserter := assert.New(t)
	fmt.Printf("test: %s\n", test.description)
	test.entries = addIngresses(test.ingresses, test.entries)

	client := new(fake.FakeClient)
	updater := new(fakeUpdater)

	config := test.config
	config.KubernetesClient = client
	config.Updaters = []Updater{updater}
	controller := New(config, make(chan struct{}))

	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Health").Return(nil)

	// This is the call we are testing (by returning an empty array of ingresses)
	client.On("GetAllIngresses").Return([]*networkingv1.Ingress{}, nil)

	ingressWatcher, ingressCh := createFakeWatcher()
	serviceWatcher, serviceCh := createFakeWatcher()
	namespaceWatcher, namespaceCh := createFakeWatcher()
	client.On("WatchIngresses").Return(ingressWatcher)
	client.On("WatchServices").Return(serviceWatcher)
	client.On("WatchNamespaces").Return(namespaceWatcher)

	asserter.NoError(controller.Start())
	ingressCh <- struct{}{}
	serviceCh <- struct{}{}
	namespaceCh <- struct{}{}

	time.Sleep(smallWaitTime)

	// This is the main assertion we are making (that 0 ingresses fails the update)
	asserter.EqualError(controller.Health(), "updates failed to apply: found 0 ingresses")
	asserter.NoError(controller.Stop())

	time.Sleep(smallWaitTime)

	updater.AssertExpectations(t)
	client.AssertExpectations(t)
}

func TestUpdateFailsWhenK8sClientReturnsNoNamespaceIngresses(t *testing.T) {

	namespaceSelectors := []*k8s.NamespaceSelector{{LabelName: "team", LabelValue: "theteam"}}

	test := testSpec{
		"ingress without rules definition",
		createDefaultIngresses(),
		createDefaultServices(),
		createDefaultNamespaces(),
		createLbEntriesFixture(),
		Config{
			DefaultAllow:                 ingressDefaultAllow,
			DefaultBackendTimeoutSeconds: backendTimeout,
			Name:                         defaultIngressClass,
			NamespaceSelectors:           namespaceSelectors,
			MatchAllNamespaceSelectors:   false,
		},
	}

	asserter := assert.New(t)
	fmt.Printf("test: %s\n", test.description)
	test.entries = addIngresses(test.ingresses, test.entries)

	client := new(fake.FakeClient)
	updater := new(fakeUpdater)

	config := test.config
	config.KubernetesClient = client
	config.Updaters = []Updater{updater}
	controller := New(config, make(chan struct{}))

	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Health").Return(nil)

	// This is the call we are testing (by returning an empty array of ingresses)
	client.On("GetIngresses", namespaceSelectors, false).Return([]*networkingv1.Ingress{}, nil)

	ingressWatcher, ingressCh := createFakeWatcher()
	serviceWatcher, serviceCh := createFakeWatcher()
	namespaceWatcher, namespaceCh := createFakeWatcher()
	client.On("WatchIngresses").Return(ingressWatcher)
	client.On("WatchServices").Return(serviceWatcher)
	client.On("WatchNamespaces").Return(namespaceWatcher)

	asserter.NoError(controller.Start())
	ingressCh <- struct{}{}
	serviceCh <- struct{}{}
	namespaceCh <- struct{}{}

	time.Sleep(smallWaitTime)

	// This is the main assertion we are making (that 0 ingresses fails the update)
	asserter.EqualError(controller.Health(), "updates failed to apply: found 0 ingresses")
	asserter.NoError(controller.Stop())

	time.Sleep(smallWaitTime)

	updater.AssertExpectations(t)
	client.AssertExpectations(t)
}

func TestUpdateFailsWhenK8sClientReturnsNoServices(t *testing.T) {
	test := testSpec{
		"ingress without rules definition",
		createDefaultIngresses(),
		createDefaultServices(),
		createDefaultNamespaces(),
		createLbEntriesFixture(),
		defaultConfig(),
	}

	asserter := assert.New(t)
	fmt.Printf("test: %s\n", test.description)
	test.entries = addIngresses(test.ingresses, test.entries)

	client := new(fake.FakeClient)
	updater := new(fakeUpdater)

	config := test.config
	config.KubernetesClient = client
	config.Updaters = []Updater{updater}
	controller := New(config, make(chan struct{}))

	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Health").Return(nil)
	updater.AssertNotCalled(t, "Update")
	//
	// We have to return ingresses successfully first
	client.On("GetAllIngresses").Return(test.ingresses, nil)
	// This is the call we are testing (by returning an empty array of services)
	client.On("GetServices").Return([]*corev1.Service{}, nil)

	ingressWatcher, ingressCh := createFakeWatcher()
	serviceWatcher, serviceCh := createFakeWatcher()
	namespaceWatcher, namespaceCh := createFakeWatcher()
	client.On("WatchIngresses").Return(ingressWatcher)
	client.On("WatchServices").Return(serviceWatcher)
	client.On("WatchNamespaces").Return(namespaceWatcher)

	asserter.NoError(controller.Start())
	ingressCh <- struct{}{}
	serviceCh <- struct{}{}
	namespaceCh <- struct{}{}

	time.Sleep(smallWaitTime)

	// This is the main assertion we are making (that 0 services fails the update)
	asserter.EqualError(controller.Health(), "updates failed to apply: found 0 services")
	asserter.NoError(controller.Stop())

	time.Sleep(smallWaitTime)

	updater.AssertExpectations(t)
	client.AssertExpectations(t)
}
