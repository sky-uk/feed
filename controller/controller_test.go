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
	"k8s.io/client-go/pkg/util/intstr"
)

const smallWaitTime = time.Millisecond * 50

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

	client.On("GetIngresses").Return([]*v1beta1.Ingress{}, nil)
	client.On("GetServices").Return([]*v1.Service{}, nil)
	client.On("WatchIngresses").Return(ingressWatcher)
	client.On("WatchServices").Return(serviceWatcher)
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

	controller.Stop()
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

	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Update", mock.Anything).Return(nil).Once()
	updater.On("Update", mock.Anything).Return(fmt.Errorf("kaboom, update failed :(")).Once()
	updater.On("Health").Return(nil)

	client.On("GetIngresses").Return([]*v1beta1.Ingress{}, nil)
	client.On("GetServices").Return([]*v1.Service{}, nil)
	client.On("WatchIngresses").Return(ingressWatcher)
	client.On("WatchServices").Return(serviceWatcher)
	assert.NoError(controller.Start())

	// expect
	updateCh <- struct{}{}
	time.Sleep(smallWaitTime)
	assert.NoError(controller.Health())

	updateCh <- struct{}{}
	time.Sleep(smallWaitTime)
	assert.Error(controller.Health())

	// cleanup
	controller.Stop()
}

func defaultConfig() Config {
	return Config{
		DefaultAllow:                 ingressDefaultAllow,
		DefaultBackendTimeoutSeconds: backendTimeout,
	}
}

func TestUpdaterIsUpdatedOnK8sUpdates(t *testing.T) {
	//given
	assert := assert.New(t)

	var tests = []struct {
		description string
		ingresses   []*v1beta1.Ingress
		services    []*v1.Service
		entries     IngressEntries
		config      Config
	}{
		{
			"ingress tagged with sky.uk/frontend-scheme",
			createIngressesFromNonELBAnnotation(),
			createDefaultServices(),
			createLbEntriesFixture(),
			defaultConfig(),
		},
		{
			"ingress without http spec",
			createIngressWithoutHTTPSpec(),
			createDefaultServices(),
			nil,
			defaultConfig(),
		},
		{
			"ingress with corresponding service",
			createDefaultIngresses(),
			createDefaultServices(),
			createLbEntriesFixture(),
			defaultConfig(),
		},
		{
			"ingress with extra services",
			createDefaultIngresses(),
			append(createDefaultServices(),
				createServiceFixture("another one", ingressNamespace, serviceIP)...),
			createLbEntriesFixture(),
			defaultConfig(),
		},
		{
			"ingress without corresponding service",
			createDefaultIngresses(),
			[]*v1.Service{},
			nil,
			defaultConfig(),
		},
		{
			"ingress with service with non-matching namespace",
			createDefaultIngresses(),
			createServiceFixture(ingressSvcName, "lalala land", serviceIP),
			nil,
			defaultConfig(),
		},
		{
			"ingress with service with non-matching name",
			createDefaultIngresses(),
			createServiceFixture("lalala service", ingressNamespace, serviceIP),
			nil,
			defaultConfig(),
		},
		{
			"ingress with missing host name",
			createIngressesFixture("", ingressSvcName, ingressSvcPort,
				map[string]string{
					ingressAllowAnnotation:      ingressAllow,
					backendTimeoutSeconds:       "10",
					frontendElbSchemeAnnotation: "internal",
				}),
			createDefaultServices(),
			nil,
			defaultConfig(),
		},
		{
			"ingress with missing service name",
			createIngressesFixture(ingressHost, "", ingressSvcPort,
				map[string]string{
					ingressAllowAnnotation:      ingressAllow,
					backendTimeoutSeconds:       "10",
					frontendElbSchemeAnnotation: "internal",
				}),
			createDefaultServices(),
			nil,
			defaultConfig(),
		},
		{
			"ingress with missing service port",
			createIngressesFixture(ingressHost, ingressSvcName, 0,
				map[string]string{
					ingressAllowAnnotation:      ingressAllow,
					backendTimeoutSeconds:       "10",
					frontendElbSchemeAnnotation: "internal",
				}),
			createDefaultServices(),
			nil,
			defaultConfig(),
		},
		{
			"ingress with missing service IP",
			createDefaultIngresses(),
			createServiceFixture(ingressSvcName, ingressNamespace, ""),
			nil,
			defaultConfig(),
		},
		{
			"ingress with 'None' as service IP",
			createDefaultIngresses(),
			createServiceFixture(ingressSvcName, ingressNamespace, "None"),
			nil,
			defaultConfig(),
		},
		{
			"ingress with default allow",
			createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort,
				map[string]string{
					backendTimeoutSeconds: "10",
				}),
			createDefaultServices(),
			[]IngressEntry{{
				Namespace:      ingressNamespace,
				Name:           ingressName,
				Host:           ingressHost,
				Path:           ingressPath,
				ServiceAddress: serviceIP,
				ServicePort:    ingressSvcPort,
				Allow:          strings.Split(ingressDefaultAllow, ","),
				BackendTimeoutSeconds: backendTimeout,
			}},
			defaultConfig(),
		},
		{
			"ingress with empty allow",
			createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort,
				map[string]string{
					ingressAllowAnnotation:      "",
					backendTimeoutSeconds:       "10",
					frontendElbSchemeAnnotation: "internal",
				}),
			createDefaultServices(),
			[]IngressEntry{{
				Namespace:      ingressNamespace,
				Name:           ingressName,
				Host:           ingressHost,
				Path:           ingressPath,
				ServiceAddress: serviceIP,
				ServicePort:    ingressSvcPort,
				LbScheme:       "internal",
				Allow:          []string{},
				BackendTimeoutSeconds: backendTimeout,
			}},
			defaultConfig(),
		},
		{
			"ingress with strip paths set to true",
			createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort,
				map[string]string{
					ingressAllowAnnotation:      "",
					stripPathAnnotation:         "true",
					backendTimeoutSeconds:       "10",
					frontendElbSchemeAnnotation: "internal",
				}),
			createDefaultServices(),
			[]IngressEntry{{
				Namespace:             ingressNamespace,
				Name:                  ingressName,
				Host:                  ingressHost,
				Path:                  ingressPath,
				ServiceAddress:        serviceIP,
				ServicePort:           ingressSvcPort,
				LbScheme:              "internal",
				Allow:                 []string{},
				StripPaths:            true,
				BackendTimeoutSeconds: backendTimeout,
			}},
			defaultConfig(),
		},
		{
			"ingress with strip paths set to false",
			createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort,
				map[string]string{
					ingressAllowAnnotation:      "",
					stripPathAnnotation:         "false",
					backendTimeoutSeconds:       "10",
					frontendElbSchemeAnnotation: "internal",
				}),
			createDefaultServices(),
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
			}},
			defaultConfig(),
		},
		{
			"ingress with exact path set to true",
			createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort,
				map[string]string{
					ingressAllowAnnotation:      "",
					exactPathAnnotation:         "true",
					backendTimeoutSeconds:       "10",
					frontendElbSchemeAnnotation: "internal",
				}),
			createDefaultServices(),
			[]IngressEntry{{
				Namespace:             ingressNamespace,
				Name:                  ingressName,
				Host:                  ingressHost,
				Path:                  ingressPath,
				ServiceAddress:        serviceIP,
				ServicePort:           ingressSvcPort,
				LbScheme:              "internal",
				Allow:                 []string{},
				ExactPath:             true,
				BackendTimeoutSeconds: backendTimeout,
			}},
			defaultConfig(),
		},
		{
			"ingress with exact path set to false",
			createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort,
				map[string]string{
					ingressAllowAnnotation:      "",
					exactPathAnnotation:         "false",
					backendTimeoutSeconds:       "10",
					frontendElbSchemeAnnotation: "internal",
				}),
			createDefaultServices(),
			[]IngressEntry{{
				Namespace:             ingressNamespace,
				Name:                  ingressName,
				Host:                  ingressHost,
				Path:                  ingressPath,
				ServiceAddress:        serviceIP,
				ServicePort:           ingressSvcPort,
				LbScheme:              "internal",
				Allow:                 []string{},
				ExactPath:             false,
				BackendTimeoutSeconds: backendTimeout,
			}},
			defaultConfig(),
		},
		{
			"ingress with overridden backend timeout",
			createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort,
				map[string]string{
					ingressAllowAnnotation:      "",
					stripPathAnnotation:         "false",
					backendTimeoutSeconds:       "20",
					frontendElbSchemeAnnotation: "internal",
				}),
			createDefaultServices(),
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
				BackendTimeoutSeconds: 20,
			}},
			defaultConfig(),
		},
		{
			"ingress with default backend timeout",
			createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort,
				map[string]string{
					ingressAllowAnnotation:      "",
					stripPathAnnotation:         "false",
					frontendElbSchemeAnnotation: "internal",
				}),
			createDefaultServices(),
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
			}},
			defaultConfig(),
		},
		{
			"ingress with overridden backend max connections",
			createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort,
				map[string]string{
					ingressAllowAnnotation:      "",
					stripPathAnnotation:         "false",
					backendTimeoutSeconds:       "20",
					frontendElbSchemeAnnotation: "internal",
					backendMaxConnections:       "512",
				}),
			createDefaultServices(),
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
				BackendTimeoutSeconds: 20,
				BackendMaxConnections: 512,
			}},
			defaultConfig(),
		},
		{
			"ingress with default backend max connections",
			createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort,
				map[string]string{
					ingressAllowAnnotation:      "",
					stripPathAnnotation:         "false",
					backendTimeoutSeconds:       "20",
					frontendElbSchemeAnnotation: "internal",
				}),
			createDefaultServices(),
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
				BackendTimeoutSeconds: 20,
				BackendMaxConnections: defaultMaxConnections,
			}},
			defaultConfig(),
		},
		{
			"ingress with default proxy buffer values when not overridden by the ingress definition",
			createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort,
				map[string]string{
					ingressAllowAnnotation: "",
				}),
			createDefaultServices(),
			[]IngressEntry{{
				Namespace:             ingressNamespace,
				Name:                  ingressName,
				Host:                  ingressHost,
				Path:                  ingressPath,
				ServiceAddress:        serviceIP,
				ServicePort:           ingressSvcPort,
				LbScheme:              "",
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
			},
		},
		{
			"ingress definition overrides default proxy buffer values",
			createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort,
				map[string]string{
					ingressAllowAnnotation:      "",
					proxyBufferSizeAnnotation:   "6",
					proxyBufferBlocksAnnotation: "4",
				}),
			createDefaultServices(),
			[]IngressEntry{{
				Namespace:             ingressNamespace,
				Name:                  ingressName,
				Host:                  ingressHost,
				Path:                  ingressPath,
				ServiceAddress:        serviceIP,
				ServicePort:           ingressSvcPort,
				LbScheme:              "",
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
			},
		},
		{
			"ingress definition resets to max when proxy buffer values exceed max allowed values",
			createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort,
				map[string]string{
					ingressAllowAnnotation:      "",
					proxyBufferSizeAnnotation:   "64",
					proxyBufferBlocksAnnotation: "12",
				}),
			createDefaultServices(),
			[]IngressEntry{{
				Namespace:             ingressNamespace,
				Name:                  ingressName,
				Host:                  ingressHost,
				Path:                  ingressPath,
				ServiceAddress:        serviceIP,
				ServicePort:           ingressSvcPort,
				LbScheme:              "",
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
			},
		},
	}

	for _, test := range tests {
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
		// once for ingress update, once for service update
		updater.On("Update", test.entries).Return(nil).Times(2)

		client.On("GetIngresses").Return(test.ingresses, nil)
		client.On("GetServices").Return(test.services, nil)

		ingressWatcher, ingressCh := createFakeWatcher()
		serviceWatcher, serviceCh := createFakeWatcher()
		client.On("WatchIngresses").Return(ingressWatcher)
		client.On("WatchServices").Return(serviceWatcher)

		//when
		assert.NoError(controller.Start())
		ingressCh <- struct{}{}
		serviceCh <- struct{}{}
		time.Sleep(smallWaitTime)

		//then
		assert.NoError(controller.Stop())
		time.Sleep(smallWaitTime)
		updater.AssertExpectations(t)
	}
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
		Name:                  ingressName,
		Host:                  ingressHost,
		Path:                  ingressPath,
		ServiceAddress:        serviceIP,
		ServicePort:           ingressSvcPort,
		Allow:                 strings.Split(ingressAllow, ","),
		LbScheme:              lbScheme,
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

func createDefaultIngresses() []*v1beta1.Ingress {
	return createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort,
		map[string]string{
			ingressAllowAnnotation:      ingressAllow,
			backendTimeoutSeconds:       "10",
			frontendElbSchemeAnnotation: "internal",
		})
}

func createIngressesFromNonELBAnnotation() []*v1beta1.Ingress {
	return createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort,
		map[string]string{
			ingressAllowAnnotation:   ingressAllow,
			backendTimeoutSeconds:    "10",
			frontendSchemeAnnotation: "internal",
		})
}

func createIngressWithoutHTTPSpec() []*v1beta1.Ingress {

	return []*v1beta1.Ingress{
		{
			ObjectMeta: v1.ObjectMeta{
				Name:      ingressName,
				Namespace: ingressNamespace,
			},
			Spec: v1beta1.IngressSpec{
				Rules: []v1beta1.IngressRule{},
			},
		},
	}
}

func createIngressesFixture(host string, serviceName string, servicePort int, ingressAnnotations map[string]string) []*v1beta1.Ingress {

	paths := []v1beta1.HTTPIngressPath{{
		Path: ingressPath,
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
		}
	}

	return []*v1beta1.Ingress{
		{
			ObjectMeta: v1.ObjectMeta{
				Name:        ingressName,
				Namespace:   ingressNamespace,
				Annotations: annotations,
			},
			Spec: v1beta1.IngressSpec{
				Rules: []v1beta1.IngressRule{{
					Host: host,
					IngressRuleValue: v1beta1.IngressRuleValue{HTTP: &v1beta1.HTTPIngressRuleValue{
						Paths: paths,
					}},
				}},
			},
		},
	}
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
