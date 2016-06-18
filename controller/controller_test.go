package controller

import (
	"testing"

	"time"

	"fmt"

	"strings"

	"github.com/sky-uk/feed/k8s"
	fake "github.com/sky-uk/feed/util/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const smallWaitTime = time.Millisecond * 50

type fakeUpdater struct {
	mock.Mock
}

func (lb *fakeUpdater) Update(update IngressUpdate) error {
	r := lb.Called(update)
	return r.Error(0)
}

func (lb *fakeUpdater) Start() error {
	r := lb.Called()
	return r.Error(0)
}

func (lb *fakeUpdater) Stop() error {
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

func (w *fakeWatcher) Done() chan<- struct{} {
	r := w.Called()
	return r.Get(0).(chan<- struct{})
}

func (w *fakeWatcher) Health() error {
	r := w.Called()
	return r.Error(0)
}

func createFakeWatcher() (*fakeWatcher, chan interface{}, chan struct{}) {
	watcher := new(fakeWatcher)
	updateCh := make(chan interface{}, 1)
	doneCh := make(chan struct{})
	watcher.On("Updates").Return((<-chan interface{})(updateCh))
	watcher.On("Done").Return((chan<- struct{})(doneCh))
	return watcher, updateCh, doneCh
}

func createDefaultStubs() (*fakeUpdater, *fake.FakeClient) {
	updater := new(fakeUpdater)
	client := new(fake.FakeClient)
	ingressWatcher, _, _ := createFakeWatcher()
	serviceWatcher, _, _ := createFakeWatcher()

	client.On("GetIngresses").Return([]k8s.Ingress{}, nil)
	client.On("GetServices").Return([]k8s.Service{}, nil)
	client.On("WatchIngresses").Return(ingressWatcher)
	client.On("WatchServices").Return(serviceWatcher)
	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Update", mock.Anything).Return(nil)
	updater.On("Health").Return(nil)
	ingressWatcher.On("Health").Return(nil)
	serviceWatcher.On("Health").Return(nil)

	return updater, client
}

func newController(lb Updater, client k8s.Client) Controller {
	return New(Config{
		Updaters:         []Updater{lb},
		KubernetesClient: client,
		DefaultAllow:     ingressDefaultAllow,
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

func TestUnhealthyIfNotWatchingForUpdates(t *testing.T) {
	// given
	assert := assert.New(t)
	updater, _ := createDefaultStubs()
	client := new(fake.FakeClient)
	controller := newController(updater, client)

	ingressWatcher, _, _ := createFakeWatcher()
	serviceWatcher, _, _ := createFakeWatcher()

	client.On("WatchIngresses").Return(ingressWatcher)
	client.On("WatchServices").Return(serviceWatcher)
	assert.NoError(controller.Start())

	// when
	watcherErr := fmt.Errorf("i'm a sad watcher")
	ingressWatcher.On("Health").Return(watcherErr).Once()
	ingressWatcher.On("Health").Return(nil)
	serviceWatcher.On("Health").Return(watcherErr).Once()
	serviceWatcher.On("Health").Return(nil)

	// then
	assert.Error(controller.Health())
	assert.Error(controller.Health())
	assert.NoError(controller.Health())

	// cleanup
	controller.Stop()
}

func TestUnhealthyIfUpdaterFails(t *testing.T) {
	// given
	assert := assert.New(t)
	updater := new(fakeUpdater)
	client := new(fake.FakeClient)
	controller := newController(updater, client)

	ingressWatcher, updateCh, _ := createFakeWatcher()
	serviceWatcher, _, _ := createFakeWatcher()
	ingressWatcher.On("Health").Return(nil)
	serviceWatcher.On("Health").Return(nil)

	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Update", mock.Anything).Return(nil).Once()
	updater.On("Update", mock.Anything).Return(fmt.Errorf("kaboom, update failed :(")).Once()
	updater.On("Health").Return(nil)

	client.On("GetIngresses").Return([]k8s.Ingress{}, nil)
	client.On("GetServices").Return([]k8s.Service{}, nil)
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

func TestUpdaterIsUpdatedOnK8sUpdates(t *testing.T) {
	//given
	assert := assert.New(t)

	var tests = []struct {
		description string
		ingresses   []k8s.Ingress
		services    []k8s.Service
		entries     IngressUpdate
	}{
		{
			"ingress with corresponding service",
			createDefaultIngresses(),
			createDefaultServices(),
			createLbEntriesFixture(),
		},
		{
			"ingress with extra services",
			createDefaultIngresses(),
			append(createDefaultServices(),
				createServiceFixture("another one", ingressNamespace, serviceIP)...),
			createLbEntriesFixture(),
		},
		{
			"ingress without corresponding service",
			createDefaultIngresses(),
			[]k8s.Service{},
			IngressUpdate{Entries: []IngressEntry{}},
		},
		{
			"ingress with service with non-matching namespace",
			createDefaultIngresses(),
			createServiceFixture(ingressSvcName, "lalala land", serviceIP),
			IngressUpdate{Entries: []IngressEntry{}},
		},
		{
			"ingress with service with non-matching name",
			createDefaultIngresses(),
			createServiceFixture("lalala service", ingressNamespace, serviceIP),
			IngressUpdate{Entries: []IngressEntry{}},
		},
		{
			"ingress with missing host name",
			createIngressesFixture("", ingressSvcName, ingressSvcPort, ingressAllow),
			createDefaultServices(),
			IngressUpdate{Entries: []IngressEntry{}},
		},
		{
			"ingress with missing service name",
			createIngressesFixture(ingressHost, "", ingressSvcPort, ingressAllow),
			createDefaultServices(),
			IngressUpdate{Entries: []IngressEntry{}},
		},
		{
			"ingress with missing service port",
			createIngressesFixture(ingressHost, ingressSvcName, 0, ingressAllow),
			createDefaultServices(),
			IngressUpdate{Entries: []IngressEntry{}},
		},
		{
			"ingress with missing service IP",
			createDefaultIngresses(),
			createServiceFixture(ingressSvcName, ingressNamespace, ""),
			IngressUpdate{Entries: []IngressEntry{}},
		},
		{
			"ingress with default allow",
			createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort, "MISSING"),
			createDefaultServices(),
			IngressUpdate{Entries: []IngressEntry{{
				Name:           ingressNamespace + "/" + ingressName,
				Host:           ingressHost,
				Path:           ingressPath,
				ServiceAddress: serviceIP,
				ServicePort:    ingressSvcPort,
				Allow:          strings.Split(ingressDefaultAllow, ","),
			}}},
		},
		{
			"ingress with empty allow",
			createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort, ""),
			createDefaultServices(),
			IngressUpdate{Entries: []IngressEntry{{
				Name:           ingressNamespace + "/" + ingressName,
				Host:           ingressHost,
				Path:           ingressPath,
				ServiceAddress: serviceIP,
				ServicePort:    ingressSvcPort,
				Allow:          []string{},
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
		// once for ingress update, once for service update
		updater.On("Update", test.entries).Return(nil).Times(2)

		client.On("GetIngresses").Return(test.ingresses, nil)
		client.On("GetServices").Return(test.services, nil)

		ingressWatcher, ingressCh, _ := createFakeWatcher()
		serviceWatcher, serviceCh, _ := createFakeWatcher()
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

func createLbEntriesFixture() IngressUpdate {
	return IngressUpdate{Entries: []IngressEntry{{
		Name:           ingressNamespace + "/" + ingressName,
		Host:           ingressHost,
		Path:           ingressPath,
		ServiceAddress: serviceIP,
		ServicePort:    ingressSvcPort,
		Allow:          strings.Split(ingressAllow, ","),
	}}}
}

const (
	ingressHost         = "foo.sky.com"
	ingressPath         = "/foo"
	ingressName         = "foo-ingress"
	ingressSvcName      = "foo-svc"
	ingressSvcPort      = 80
	ingressNamespace    = "happysky"
	ingressAllow        = "10.82.0.0/16,10.44.0.0/16"
	ingressDefaultAllow = "10.50.0.0/16,10.1.0.0/16"
	serviceIP           = "10.254.0.82"
)

func createDefaultIngresses() []k8s.Ingress {
	return createIngressesFixture(ingressHost, ingressSvcName, ingressSvcPort, ingressAllow)
}

func createIngressesFixture(host string, serviceName string, servicePort int, allow string) []k8s.Ingress {
	paths := []k8s.HTTPIngressPath{{
		Path: ingressPath,
		Backend: k8s.IngressBackend{
			ServiceName: serviceName,
			ServicePort: k8s.FromInt(servicePort),
		},
	}}

	annotations := make(map[string]string)
	if allow != "MISSING" {
		annotations[ingressAllowAnnotation] = allow
	}

	return []k8s.Ingress{
		{
			ObjectMeta: k8s.ObjectMeta{
				Name:        ingressName,
				Namespace:   ingressNamespace,
				Annotations: annotations,
			},
			Spec: k8s.IngressSpec{
				Rules: []k8s.IngressRule{{
					Host: host,
					IngressRuleValue: k8s.IngressRuleValue{HTTP: &k8s.HTTPIngressRuleValue{
						Paths: paths,
					}},
				}},
			},
		},
	}
}

func createDefaultServices() []k8s.Service {
	return createServiceFixture(ingressSvcName, ingressNamespace, serviceIP)
}

func createServiceFixture(name string, namespace string, clusterIP string) []k8s.Service {
	return []k8s.Service{
		{
			ObjectMeta: k8s.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: k8s.ServiceSpec{
				ClusterIP: clusterIP,
			},
		},
	}
}
