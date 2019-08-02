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
	v1 "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	metav1 "k8s.io/client-go/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/util/intstr"
)

const smallWaitTime = time.Millisecond * 50
const defaultControllerName = "main"

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

	client.On("GetAllIngresses").Return([]*v1beta1.Ingress{}, nil)
	client.On("GetIngresses", mock.Anything).Return([]*v1beta1.Ingress{}, nil)
	client.On("GetServices").Return([]*v1.Service{}, nil)
	client.On("WatchIngresses").Return(ingressWatcher)
	client.On("WatchServices").Return(serviceWatcher)
	client.On("WatchNamespaces").Return(namespaceWatcher)
	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Update", mock.Anything).Return(nil)
	updater.On("Health").Return(nil)

	return updater, client
}

func newController(lb Updater, client k8s.Client) Controller {
	return New(Config{
		Updaters:                     []Updater{lb},
		KubernetesClient:             client,
		DefaultAllow:                 ingressDefaultAllow,
		DefaultBackendTimeoutSeconds: backendTimeout,
	})
}

func TestControllerCanBeStartedAndStopped(t *testing.T) {
	assert := assert.New(t)
	updater, client := createDefaultStubs()
	controller := newController(updater, client)

	assert.NoError(controller.Start())
	assert.NoError(controller.Stop())
	updater.AssertCalled(t, "Start")
	updater.AssertCalled(t, "Stop")
}

func TestControllerStartsAndStopsUpdatersInCorrectOrder(t *testing.T) {
	// given
	assert := assert.New(t)
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
	})

	// when
	started = nil
	stopped = nil
	assert.NoError(controller.Start())
	assert.NoError(controller.Stop())

	// then
	assert.Equal(started, []*fakeUpdater{updater1, updater2}, "should start in order")
	assert.Equal(stopped, []*fakeUpdater{updater2, updater1}, "should stop in reverse order")
}

func TestControllerStopsAnyStartedUpdatersIfOneFailsToStart(t *testing.T) {
	// given
	assert := assert.New(t)
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
	})

	// when
	assert.Error(controller.Start())

	// then
	updater1.AssertExpectations(t)
	updater2.AssertExpectations(t)
}

func TestControllerCannotBeRestarted(t *testing.T) {
	// given
	assert := assert.New(t)
	controller := newController(createDefaultStubs())

	// and
	assert.NoError(controller.Start())
	assert.NoError(controller.Stop())

	// then
	assert.Error(controller.Start())
	assert.Error(controller.Stop())
}

func TestControllerStartCannotBeCalledTwice(t *testing.T) {
	// given
	assert := assert.New(t)
	controller := newController(createDefaultStubs())

	// expect
	assert.NoError(controller.Start())
	assert.Error(controller.Start())
	assert.NoError(controller.Stop())
}

func TestControllerIsUnhealthyUntilStarted(t *testing.T) {
	// given
	assert := assert.New(t)
	controller := newController(createDefaultStubs())

	// expect
	assert.Error(controller.Health(), "should be unhealthy until started")
	assert.NoError(controller.Start())
	time.Sleep(smallWaitTime)
	assert.NoError(controller.Health(), "should be healthy after started")
	assert.NoError(controller.Stop())
	time.Sleep(smallWaitTime)
	assert.Error(controller.Health(), "should be unhealthy after stopped")
}

func TestControllerIsUnhealthyIfUpdaterIsUnhealthy(t *testing.T) {
	assert := assert.New(t)
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

	assert.NoError(controller.Start())
	assert.NoError(controller.Health())
	assert.Equal(lbErr, controller.Health())

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
	assert := assert.New(t)
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

	client.On("GetAllIngresses").Return([]*v1beta1.Ingress{}, nil)
	client.On("GetServices").Return([]*v1.Service{}, nil)
	client.On("WatchIngresses").Return(ingressWatcher)
	client.On("WatchServices").Return(serviceWatcher)
	client.On("WatchNamespaces").Return(namespaceWatcher)
	assert.NoError(controller.Start())

	// expect
	updateCh <- struct{}{}
	time.Sleep(smallWaitTime)
	assert.NoError(controller.Health())

	updateCh <- struct{}{}
	time.Sleep(smallWaitTime)
	assert.Error(controller.Health())

	// cleanup
	_ = controller.Stop()
}

func defaultConfig() Config {
	return Config{
		DefaultAllow:                 ingressDefaultAllow,
		DefaultBackendTimeoutSeconds: backendTimeout,
		Name:                         defaultControllerName,
	}
}

type testSpec struct {
	description string
	ingresses   []*v1beta1.Ingress
	services    []*v1.Service
	namespaces  []*v1.Namespace
	entries     IngressEntries
	config      Config
}

func TestUpdaterIsUpdatedForIngressTaggedWithSkyFrontendScheme(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress tagged with sky.uk/frontend-scheme",
		createIngressesFromNonELBAnnotation(),
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
		[]*v1.Service{},
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
			ingressAllowAnnotation:          ingressAllow,
			backendTimeoutSeconds:           "10",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
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
			ingressAllowAnnotation:          ingressAllow,
			backendTimeoutSeconds:           "10",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
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
			ingressAllowAnnotation:          ingressAllow,
			backendTimeoutSeconds:           "10",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
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
			backendTimeoutSeconds:           "10",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			Allow:                 strings.Split(ingressDefaultAllow, ","),
			IngressControllerName: defaultControllerName,
			BackendTimeoutSeconds: backendTimeout,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithEmptyAllow(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with empty allow",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:          "",
			backendTimeoutSeconds:           "10",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressControllerName: defaultControllerName,
			Allow:                 []string{},
			BackendTimeoutSeconds: backendTimeout,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithStripPathsTrue(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with strip paths set to true",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:          "",
			stripPathAnnotation:             "true",
			backendTimeoutSeconds:           "10",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressControllerName: defaultControllerName,
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
			ingressAllowAnnotation:          "",
			stripPathAnnotation:             "false",
			backendTimeoutSeconds:           "10",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressControllerName: defaultControllerName,
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
			ingressAllowAnnotation:          "",
			exactPathAnnotation:             "true",
			backendTimeoutSeconds:           "10",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressControllerName: defaultControllerName,
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
			ingressAllowAnnotation:          "",
			exactPathAnnotation:             "false",
			backendTimeoutSeconds:           "10",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressControllerName: defaultControllerName,
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
			ingressAllowAnnotation:          "",
			stripPathAnnotation:             "false",
			backendTimeoutSeconds:           "20",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressControllerName: defaultControllerName,
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
			ingressAllowAnnotation:          "",
			stripPathAnnotation:             "false",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressControllerName: defaultControllerName,
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
			ingressAllowAnnotation:          "",
			stripPathAnnotation:             "false",
			backendTimeoutSeconds:           "20",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
			backendMaxConnections:           "512",
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressControllerName: defaultControllerName,
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: 20,
			BackendMaxConnections: 512,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressWithDefaultBackendMaxConnections(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with default backend max connections",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:          "",
			stripPathAnnotation:             "false",
			backendTimeoutSeconds:           "20",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressControllerName: defaultControllerName,
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
			ingressAllowAnnotation:          "",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "",
			IngressControllerName: defaultControllerName,
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
			Name:                         defaultControllerName,
		},
	})
}

func TestUpdaterIsUpdatedForIngressOverridesDefaultProxyBufferValues(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress definition overrides default proxy buffer values",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:          "",
			proxyBufferSizeAnnotation:       "6",
			proxyBufferBlocksAnnotation:     "4",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "",
			IngressControllerName: defaultControllerName,
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
			Name:                         defaultControllerName,
		},
	})
}

func TestUpdaterIsUpdatedForIngressDefinitionResetsToMacWhenProxyBufferValuesExceedMax(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress definition resets to max when proxy buffer values exceed max allowed values",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:          "",
			proxyBufferSizeAnnotation:       "64",
			proxyBufferBlocksAnnotation:     "12",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "",
			IngressControllerName: defaultControllerName,
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
			Name:                         defaultControllerName,
		},
	})
}

func TestUpdaterIsUpdatedForIngressNameNotSetInIngress(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"controller name not set in ingress",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:          "",
			stripPathAnnotation:             "false",
			backendTimeoutSeconds:           "10",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			IngressControllerName: defaultControllerName,
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: backendTimeout,
		}},
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressNameSetToDefaultInIngress(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"controller name set to default in ingress",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:          "",
			stripPathAnnotation:             "false",
			backendTimeoutSeconds:           "10",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: backendTimeout,
			IngressControllerName: defaultControllerName,
		}},
		Config{
			DefaultAllow:                 ingressDefaultAllow,
			DefaultBackendTimeoutSeconds: backendTimeout,
			Name:                         defaultControllerName,
		},
	})
}

func TestUpdaterIsUpdatedForIngressNameSetToTestAndConfigSetToDefault(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress with controller name set to test and config set to default",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:          "",
			stripPathAnnotation:             "false",
			backendTimeoutSeconds:           "10",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: "test",
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		nil,
		defaultConfig(),
	})
}

func TestUpdaterIsUpdatedForIngressNameSetToTestInIngressAndConfig(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"controller name set to test in ingress and config",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:          "",
			stripPathAnnotation:             "false",
			backendTimeoutSeconds:           "10",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: "test",
		}, ingressPath),
		createDefaultServices(),
		createDefaultNamespaces(),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: backendTimeout,
			IngressControllerName: "test",
		}},
		Config{
			DefaultAllow:                 ingressDefaultAllow,
			DefaultBackendTimeoutSeconds: backendTimeout,
			Name:                         "test",
		},
	})
}

func TestUpdaterIsUpdatedForIngressInNamespaceMatchingSelector(t *testing.T) {
	runAndAssertUpdates(t, expectGetIngresses, testSpec{
		"ingress is in a namespace that matches the namespace selector",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:          "",
			stripPathAnnotation:             "false",
			backendTimeoutSeconds:           "10",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		createDefaultServices(),
		createNamespaceFixture(ingressNamespace, map[string]string{"team": "theteam"}),
		[]IngressEntry{{
			Namespace:             ingressNamespace,
			Name:                  ingressControllerName,
			Host:                  ingressHost,
			Path:                  ingressPath,
			ServiceAddress:        serviceIP,
			ServicePort:           ingressSvcPort,
			LbScheme:              "internal",
			Allow:                 []string{},
			StripPaths:            false,
			BackendTimeoutSeconds: backendTimeout,
			IngressControllerName: defaultControllerName,
		}},
		Config{
			DefaultAllow:                 ingressDefaultAllow,
			DefaultBackendTimeoutSeconds: backendTimeout,
			Name:                         defaultControllerName,
			NamespaceSelector:            &k8s.NamespaceSelector{LabelName: "team", LabelValue: "theteam"},
		},
	})
}

func TestUpdaterIsNotUpdatedForIngressInNamespaceMatchingSelector(t *testing.T) {
	runAndAssertUpdates(t, expectGetIngresses, testSpec{
		"ingress is in a namespace that matches the namespace selector",
		createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
			ingressAllowAnnotation:          "",
			stripPathAnnotation:             "false",
			backendTimeoutSeconds:           "10",
			frontendElbSchemeAnnotation:     "internal",
			ingressControllerNameAnnotation: defaultControllerName,
		}, ingressPath),
		nil,
		createNamespaceFixture(ingressNamespace, map[string]string{"team": "otherteam"}),
		nil,
		Config{
			DefaultAllow:                 ingressDefaultAllow,
			DefaultBackendTimeoutSeconds: backendTimeout,
			Name:                         defaultControllerName,
			NamespaceSelector:            &k8s.NamespaceSelector{LabelName: "team", LabelValue: "theteam"},
		},
	})
}

func TestUpdaterIsUpdatedForIngressWithoutHostDefinition(t *testing.T) {
	runAndAssertUpdates(t, expectGetAllIngresses, testSpec{
		"ingress without host definition",
		createIngressesFixture(ingressNamespace, "", ingressSvcName, ingressSvcPort, map[string]string{
			ingressControllerNameAnnotation: defaultControllerName,
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
			ingressControllerNameAnnotation: defaultControllerName,
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

type clientExpectation func(client *fake.FakeClient, ingresses []*v1beta1.Ingress)

var expectGetAllIngresses = func(client *fake.FakeClient, ingresses []*v1beta1.Ingress) {
	client.On("GetAllIngresses").Return(ingresses, nil)
}

var expectGetIngresses = func(client *fake.FakeClient, ingresses []*v1beta1.Ingress) {
	client.On("GetIngresses", &k8s.NamespaceSelector{LabelName: "team", LabelValue: "theteam"}).Return(ingresses, nil)
}

func runAndAssertUpdates(t *testing.T, clientExpectation clientExpectation, test testSpec) {
	//given
	assert := assert.New(t)

	fmt.Printf("test: %s\n", test.description)
	// add ingress pointers to entries
	test.entries = addIngresses(test.ingresses, test.entries)

	// setup clients
	client := new(fake.FakeClient)
	updater := new(fakeUpdater)

	config := test.config

	config.KubernetesClient = client
	config.Updaters = []Updater{updater}

	controller := New(config)

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
	assert.NoError(controller.Start())
	ingressCh <- struct{}{}
	serviceCh <- struct{}{}
	namespaceCh <- struct{}{}
	time.Sleep(smallWaitTime)

	//then
	assert.NoError(controller.Stop())
	time.Sleep(smallWaitTime)
	updater.AssertExpectations(t)
	client.AssertExpectations(t)
}

func addIngresses(ingresses []*v1beta1.Ingress, entries IngressEntries) IngressEntries {
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
		Name:                  ingressControllerName,
		Host:                  ingressHost,
		Path:                  ingressPath,
		ServiceAddress:        serviceIP,
		ServicePort:           ingressSvcPort,
		Allow:                 strings.Split(ingressAllow, ","),
		LbScheme:              lbScheme,
		IngressControllerName: defaultControllerName,
		BackendTimeoutSeconds: backendTimeout,
	}}
}

const (
	ingressHost           = "foo.sky.com"
	ingressPath           = "/foo"
	ingressControllerName = "foo-ingress"
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

func createDefaultIngresses() []*v1beta1.Ingress {
	return createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
		ingressAllowAnnotation:          ingressAllow,
		backendTimeoutSeconds:           "10",
		frontendElbSchemeAnnotation:     "internal",
		ingressControllerNameAnnotation: defaultControllerName,
	}, ingressPath)
}

func createIngressesFromNonELBAnnotation() []*v1beta1.Ingress {
	return createIngressesFixture(ingressNamespace, ingressHost, ingressSvcName, ingressSvcPort, map[string]string{
		ingressAllowAnnotation:          ingressAllow,
		backendTimeoutSeconds:           "10",
		frontendSchemeAnnotation:        "internal",
		ingressControllerNameAnnotation: defaultControllerName,
	}, ingressPath)
}

func createIngressWithoutRules() []*v1beta1.Ingress {
	return []*v1beta1.Ingress{
		{
			ObjectMeta: v1.ObjectMeta{
				Name:      ingressControllerName,
				Namespace: ingressNamespace,
				Annotations: map[string]string{
					ingressControllerNameAnnotation: defaultControllerName,
				},
			},
			Spec: v1beta1.IngressSpec{},
		},
	}

}

func createIngressesFixture(namespace string, host string, serviceName string, servicePort int, ingressAnnotations map[string]string, path string) []*v1beta1.Ingress {

	paths := []v1beta1.HTTPIngressPath{{
		Path: path,
		Backend: v1beta1.IngressBackend{
			ServiceName: serviceName,
			ServicePort: intstr.FromInt(servicePort),
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
		case frontendElbSchemeAnnotation:
			annotations[frontendElbSchemeAnnotation] = annotationVal
		case frontendSchemeAnnotation:
			annotations[frontendSchemeAnnotation] = annotationVal
		case backendTimeoutSeconds:
			annotations[backendTimeoutSeconds] = annotationVal
		case backendMaxConnections:
			annotations[backendMaxConnections] = annotationVal
		case proxyBufferSizeAnnotation:
			annotations[proxyBufferSizeAnnotation] = annotationVal
		case proxyBufferBlocksAnnotation:
			annotations[proxyBufferBlocksAnnotation] = annotationVal
		case ingressControllerNameAnnotation:
			annotations[ingressControllerNameAnnotation] = annotationVal
		}
	}

	ingressDefinition := createIngressWithoutRules()
	ingressRules := []v1beta1.IngressRule{}

	if host != "" {
		ingressRuleValue := v1beta1.IngressRuleValue{}
		if path != "" {
			ingressRuleValue.HTTP = &v1beta1.HTTPIngressRuleValue{Paths: paths}
		}
		ingressRule := v1beta1.IngressRule{
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

func createDefaultServices() []*v1.Service {
	return createServiceFixture(ingressSvcName, ingressNamespace, serviceIP)
}

func createServiceFixture(name string, namespace string, clusterIP string) []*v1.Service {
	return []*v1.Service{
		{
			ObjectMeta: v1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: v1.ServiceSpec{
				ClusterIP: clusterIP,
			},
		},
	}
}

func createDefaultNamespaces() []*v1.Namespace {
	return createNamespaceFixture(ingressNamespace, map[string]string{})
}

func createNamespaceFixture(name string, labels map[string]string) []*v1.Namespace {
	return []*v1.Namespace{
		{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Namespace",
				APIVersion: "v1",
			},
			ObjectMeta: v1.ObjectMeta{
				Name:   name,
				Labels: labels,
			},
			Spec:   v1.NamespaceSpec{},
			Status: v1.NamespaceStatus{},
		},
	}
}
