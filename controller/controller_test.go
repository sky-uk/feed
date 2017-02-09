package controller

import (
	"testing"

	"time"

	"strings"

	"strconv"

	"fmt"

	"github.com/sky-uk/feed/k8s"
	fake "github.com/sky-uk/feed/util/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/pkg/util/intstr"
)

const smallWaitTime = time.Millisecond * 50

type fakeUpdater struct {
	mock.Mock
}

func (lb *fakeUpdater) Update(update IngressUpdate) error {
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
	endpointWatcher, _ := createFakeWatcher()

	client.On("GetIngresses").Return([]*v1beta1.Ingress{}, nil)
	client.On("GetEndpoints").Return([]*v1.Endpoints{}, nil)
	client.On("WatchIngresses").Return(ingressWatcher)
	client.On("WatchEndpoints").Return(endpointWatcher)
	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Update", mock.Anything).Return(nil)
	updater.On("Health").Return(nil)

	return updater, client
}

func newController(lb Updater, client k8s.Client) Controller {
	return New(Config{
		Updaters:                []Updater{lb},
		KubernetesClient:        client,
		DefaultAllow:            ingressDefaultAllow,
		DefaultBackendKeepAlive: defaultBackendTimeout,
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
		Updaters:                []Updater{updater1, updater2},
		KubernetesClient:        client,
		DefaultAllow:            ingressDefaultAllow,
		DefaultBackendKeepAlive: defaultBackendTimeout,
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
	endpointWatcher, _ := createFakeWatcher()

	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Update", mock.Anything).Return(nil).Once()
	updater.On("Update", mock.Anything).Return(fmt.Errorf("kaboom, update failed :(")).Once()
	updater.On("Health").Return(nil)

	client.On("GetIngresses").Return([]*v1beta1.Ingress{}, nil)
	client.On("GetEndpoints").Return([]*v1.Endpoints{}, nil)
	client.On("WatchIngresses").Return(ingressWatcher)
	client.On("WatchEndpoints").Return(endpointWatcher)
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

func TestUpdaterIsUpdatedOnK8sUpdates(t *testing.T) {
	//given
	assert := assert.New(t)

	var tests = []struct {
		description    string
		ingresses      []*v1beta1.Ingress
		endpoints      []*v1.Endpoints
		expectedUpdate IngressUpdate
	}{
		{
			"ingress with corresponding service",
			createDefaultIngresses(),
			createDefaultEndpoints(),
			createExpectedIngressUpdate(),
		},
		{
			"ingress with extra services",
			createDefaultIngresses(),
			append(createDefaultEndpoints(),
				createEndpointsFixture("another one", ingressNamespace, []string{"1.1.1.1"}, []v1.EndpointPort{{Port: 5}})...),
			createExpectedIngressUpdate(),
		},
		{
			"ingress without corresponding service",
			createDefaultIngresses(),
			[]*v1.Endpoints{},
			IngressUpdate{Entries: []IngressEntry{}},
		},
		{
			"ingress with service with non-matching namespace",
			createDefaultIngresses(),
			createEndpointsFixture(serviceName, "lalala land", []string{"1.1.1.1"}, []v1.EndpointPort{{Port: 5}}),
			IngressUpdate{Entries: []IngressEntry{}},
		},
		{
			"ingress with service with non-matching name",
			createDefaultIngresses(),
			createEndpointsFixture("lalala service", ingressNamespace, []string{"1.1.1.1"}, []v1.EndpointPort{{Port: 5}}),
			IngressUpdate{Entries: []IngressEntry{}},
		},
		{
			"ingress with missing host name",
			createIngressesFixture("", serviceName, []int{serviceHTTPPort}, ingressAllow, stripPath, -1),
			createDefaultEndpoints(),
			IngressUpdate{Entries: []IngressEntry{}},
		},
		{
			"ingress with missing service name",
			createIngressesFixture(ingressHost, "", []int{serviceHTTPPort}, ingressAllow, stripPath, -1),
			createDefaultEndpoints(),
			IngressUpdate{Entries: []IngressEntry{}},
		},
		{
			"ingress with missing service port",
			createIngressesFixture(ingressHost, serviceName, []int{0}, ingressAllow, stripPath, -1),
			createDefaultEndpoints(),
			IngressUpdate{Entries: []IngressEntry{}},
		},
		{
			"ingress without any service endpoints",
			createDefaultIngresses(),
			createEndpointsFixture(serviceName, ingressNamespace, nil, []v1.EndpointPort{{Port: serviceHTTPPort}}),
			IngressUpdate{Entries: []IngressEntry{}},
		},
		{
			"ingress with default allow",
			createIngressesFixture(ingressHost, serviceName, []int{serviceHTTPPort}, "MISSING", stripPath, -1),
			createDefaultEndpoints(),
			IngressUpdate{Entries: []IngressEntry{{
				Namespace: ingressNamespace,
				Name:      ingressName,
				Host:      ingressHost,
				Path:      ingressPath,
				Service:   Service{Name: serviceName, Port: serviceHTTPPort, Addresses: endpointAddresses},
				Allow:     strings.Split(ingressDefaultAllow, ","),
				BackendKeepAliveSeconds: defaultBackendTimeout,
			}}},
		},
		{
			"ingress with empty allow",
			createIngressesFixture(ingressHost, serviceName, []int{serviceHTTPPort}, "", stripPath, -1),
			createDefaultEndpoints(),
			IngressUpdate{Entries: []IngressEntry{{
				Namespace: ingressNamespace,
				Name:      ingressName,
				Host:      ingressHost,
				Path:      ingressPath,
				Service:   Service{Name: serviceName, Port: serviceHTTPPort, Addresses: endpointAddresses},
				ELbScheme: "internal",
				Allow:     []string{},
				BackendKeepAliveSeconds: defaultBackendTimeout,
			}}},
		},
		{
			"ingress with strip paths set to true",
			createIngressesFixture(ingressHost, serviceName, []int{serviceHTTPPort}, "", "true", -1),
			createDefaultEndpoints(),
			IngressUpdate{Entries: []IngressEntry{{
				Namespace:               ingressNamespace,
				Name:                    ingressName,
				Host:                    ingressHost,
				Path:                    ingressPath,
				Service:                 Service{Name: serviceName, Port: serviceHTTPPort, Addresses: endpointAddresses},
				ELbScheme:               "internal",
				Allow:                   []string{},
				StripPaths:              true,
				BackendKeepAliveSeconds: defaultBackendTimeout,
			}}},
		},
		{
			"ingress with strip paths set to false",
			createIngressesFixture(ingressHost, serviceName, []int{serviceHTTPPort}, "", "false", -1),
			createDefaultEndpoints(),
			IngressUpdate{Entries: []IngressEntry{{
				Namespace:               ingressNamespace,
				Name:                    ingressName,
				Host:                    ingressHost,
				Path:                    ingressPath,
				Service:                 Service{Name: serviceName, Port: serviceHTTPPort, Addresses: endpointAddresses},
				ELbScheme:               "internal",
				Allow:                   []string{},
				StripPaths:              false,
				BackendKeepAliveSeconds: defaultBackendTimeout,
			}}},
		},
		{
			"ingress with overridden backend timeout",
			createIngressesFixture(ingressHost, serviceName, []int{serviceHTTPPort}, "", "false", 20),
			createDefaultEndpoints(),
			IngressUpdate{Entries: []IngressEntry{{
				Namespace:               ingressNamespace,
				Name:                    ingressName,
				Host:                    ingressHost,
				Path:                    ingressPath,
				Service:                 Service{Name: serviceName, Port: serviceHTTPPort, Addresses: endpointAddresses},
				ELbScheme:               "internal",
				Allow:                   []string{},
				StripPaths:              false,
				BackendKeepAliveSeconds: 20,
			}}},
		},
		{
			"ingress with default backend timeout",
			createIngressesFixture(ingressHost, serviceName, []int{serviceHTTPPort}, "", "false", -1),
			createDefaultEndpoints(),
			IngressUpdate{Entries: []IngressEntry{{
				Namespace:               ingressNamespace,
				Name:                    ingressName,
				Host:                    ingressHost,
				Path:                    ingressPath,
				Service:                 Service{Name: serviceName, Port: serviceHTTPPort, Addresses: endpointAddresses},
				ELbScheme:               "internal",
				Allow:                   []string{},
				StripPaths:              false,
				BackendKeepAliveSeconds: defaultBackendTimeout,
			}}},
		},
	}

	for _, test := range tests {
		fmt.Printf("test: %s\n", test.description)
		client := new(fake.FakeClient)
		updater := new(fakeUpdater)
		controller := newController(updater, client)

		updater.On("Start").Return(nil)
		updater.On("Stop").Return(nil)
		updater.On("Update", mock.MatchedBy(func(u IngressUpdate) bool {
			assert.Len(u.Entries, len(test.expectedUpdate.Entries), "Expected same number of entries in update")
			for _, entry := range test.expectedUpdate.Entries {
				assert.Contains(u.Entries, entry, "Expected entry in update")
			}
			return true
		})).Return(nil)

		client.On("GetIngresses").Return(test.ingresses, nil)
		client.On("GetEndpoints").Return(test.endpoints, nil)

		ingressWatcher, _ := createFakeWatcher()
		endpointsWatcher, endpointsCh := createFakeWatcher()
		client.On("WatchIngresses").Return(ingressWatcher)
		client.On("WatchEndpoints").Return(endpointsWatcher)

		//when
		assert.NoError(controller.Start())
		endpointsCh <- struct{}{}
		time.Sleep(smallWaitTime)

		//then
		assert.NoError(controller.Stop())
		time.Sleep(smallWaitTime)
		updater.AssertExpectations(t)

		if t.Failed() {
			t.FailNow()
		}
	}
}

const (
	ingressHost           = "foo.sky.com"
	ingressPath           = "/foo"
	ingressName           = "foo-ingress"
	serviceName           = "foo-svc"
	serviceHTTPPort       = 8080
	serviceAdminPort      = 8081
	ingressNamespace      = "namespace"
	ingressAllow          = "10.82.0.0/16,10.44.0.0/16"
	ingressDefaultAllow   = "10.50.0.0/16,10.1.0.0/16"
	elbScheme             = "internal"
	stripPath             = "MISSING"
	defaultBackendTimeout = 10
)

func createExpectedIngressUpdate() IngressUpdate {
	return IngressUpdate{Entries: []IngressEntry{
		{
			Namespace: ingressNamespace,
			Name:      ingressName,
			Host:      ingressHost,
			Path:      ingressPath,
			Service: Service{
				Name:      serviceName,
				Port:      serviceHTTPPort,
				Addresses: endpointAddresses,
			},
			Allow:                   strings.Split(ingressAllow, ","),
			ELbScheme:               elbScheme,
			BackendKeepAliveSeconds: defaultBackendTimeout,
		},
		{
			Namespace: ingressNamespace,
			Name:      ingressName,
			Host:      ingressHost,
			Path:      ingressPath,
			Service: Service{
				Name:      serviceName,
				Port:      serviceAdminPort,
				Addresses: endpointAddresses,
			},
			Allow:                   strings.Split(ingressAllow, ","),
			ELbScheme:               elbScheme,
			BackendKeepAliveSeconds: defaultBackendTimeout,
		},
	}}
}

var endpointAddresses = []string{"1.1.1.1", "2.2.2.2"}

func createDefaultIngresses() []*v1beta1.Ingress {
	return createIngressesFixture(ingressHost, serviceName, []int{serviceHTTPPort, serviceAdminPort}, ingressAllow,
		stripPath, -1)
}

func createIngressesFixture(host string, serviceName string, ports []int, allow string, stripPath string, backendTimeout int) []*v1beta1.Ingress {

	var paths []v1beta1.HTTPIngressPath
	for _, port := range ports {
		paths = append(paths, v1beta1.HTTPIngressPath{
			Path: ingressPath,
			Backend: v1beta1.IngressBackend{
				ServiceName: serviceName,
				ServicePort: intstr.FromInt(port),
			},
		})
	}

	annotations := make(map[string]string)
	if allow != "MISSING" {
		annotations[ingressAllowAnnotation] = allow
		annotations[frontendElbSchemeAnnotation] = elbScheme
	}
	if stripPath != "MISSING" {
		annotations[stripPathAnnotation] = stripPath
	}

	if backendTimeout != -1 {
		annotations[backendKeepAliveSeconds] = strconv.Itoa(backendTimeout)
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

func createDefaultEndpoints() []*v1.Endpoints {
	return createEndpointsFixture(serviceName, ingressNamespace,
		endpointAddresses,
		[]v1.EndpointPort{
			{
				Name:     "foo-http",
				Port:     serviceHTTPPort,
				Protocol: v1.ProtocolTCP,
			},
			{
				Name:     "foo-udp",
				Port:     6060,
				Protocol: v1.ProtocolUDP,
			},
			{
				Name: "foo-admin",
				Port: serviceAdminPort,
			},
		},
	)
}

func createEndpointsFixture(name string, namespace string, addresses []string, ports []v1.EndpointPort) []*v1.Endpoints {
	var endpointAddresses []v1.EndpointAddress
	for _, address := range addresses {
		endpointAddresses = append(endpointAddresses, v1.EndpointAddress{IP: address})
	}

	return []*v1.Endpoints{
		{
			ObjectMeta: v1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Subsets: []v1.EndpointSubset{{
				Addresses: endpointAddresses,
				NotReadyAddresses: []v1.EndpointAddress{
					{IP: "3.3.3.3"}, {IP: "4.4.4.4"},
				},
				Ports: ports,
			}},
		},
	}
}
