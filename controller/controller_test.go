package controller

import (
	"testing"

	"time"

	"fmt"

	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/util/test"
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
	updateCh := make(chan interface{})
	doneCh := make(chan struct{})
	watcher.On("Updates").Return((<-chan interface{})(updateCh))
	watcher.On("Done").Return((chan<- struct{})(doneCh))
	return watcher, updateCh, doneCh
}

func createDefaultStubs() (*fakeUpdater, *test.FakeClient) {
	updater := new(fakeUpdater)
	client := new(test.FakeClient)
	watcher, updateCh, doneCh := createFakeWatcher()

	client.On("GetIngresses").Return([]k8s.Ingress{}, nil)
	client.On("WatchIngresses").Return(watcher)
	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Update", mock.Anything).Return(nil)
	updater.On("Health").Return(nil)

	watcher.On("Health").Return(nil)
	// clean up the channels
	t := time.NewTimer(smallWaitTime * 10)
	go func() {
		defer close(updateCh)
		select {
		case <-doneCh:
		case <-t.C:
		}
	}()

	return updater, client
}

func newController(lb Updater, client k8s.Client) Controller {
	return New(Config{Updater: lb, KubernetesClient: client, ServiceDomain: serviceDomain})
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
	lbErr := fmt.Errorf("dead")
	updater.On("Health").Return(fmt.Errorf("dead")).Once()

	assert.NoError(controller.Start())
	assert.NoError(controller.Health())
	assert.Equal(lbErr, controller.Health())
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
	client := new(test.FakeClient)
	controller := newController(updater, client)

	watcher, updateCh, _ := createFakeWatcher()
	defer close(updateCh)

	client.On("WatchIngresses").Return(watcher)
	assert.NoError(controller.Start())

	// when
	watcherErr := fmt.Errorf("i'm a sad watcher")
	watcher.On("Health").Return(watcherErr)

	// then
	assert.Error(controller.Health())
}

func TestUpdatesOnIngressUpdates(t *testing.T) {
	//setup
	assert := assert.New(t)

	//given
	client := new(test.FakeClient)
	updater, _ := createDefaultStubs()
	ingresses := createIngressesFixture()
	controller := newController(updater, client)

	watcher, updateCh, _ := createFakeWatcher()
	defer close(updateCh)

	client.On("GetIngresses").Return(ingresses, nil).Once()
	client.On("WatchIngresses").Return(watcher)

	//when
	assert.NoError(controller.Start())
	updateCh <- ingresses[0]
	time.Sleep(smallWaitTime)

	//then
	entries := createLbEntriesFixture()
	updater.AssertCalled(t, "Update", entries)
}

func createLbEntriesFixture() IngressUpdate {
	return IngressUpdate{Entries: []IngressEntry{IngressEntry{
		Name:        ingressNamespace + "/" + ingressName,
		Host:        ingressHost,
		Path:        ingressPath,
		ServiceName: ingressSvcName + "." + ingressNamespace + "." + serviceDomain,
		ServicePort: ingressSvcPort,
		Allow:       ingressAllow,
	}}}
}

const (
	ingressHost      = "foo.sky.com"
	ingressPath      = "/foo"
	ingressName      = "foo-ingress"
	ingressSvcName   = "foo-svc"
	ingressSvcPort   = 80
	ingressNamespace = "happysky"
	serviceDomain    = "svc.skycluster"
	ingressAllow     = "10.82.0.0/16"
)

func createIngressesFixture() []k8s.Ingress {
	paths := []k8s.HTTPIngressPath{k8s.HTTPIngressPath{
		Path: ingressPath,
		Backend: k8s.IngressBackend{
			ServiceName: ingressSvcName,
			ServicePort: k8s.FromInt(ingressSvcPort),
		},
	}}
	return []k8s.Ingress{
		k8s.Ingress{
			ObjectMeta: k8s.ObjectMeta{
				Name:        ingressName,
				Namespace:   ingressNamespace,
				Annotations: map[string]string{ingressAllowAnnotation: ingressAllow},
			},
			Spec: k8s.IngressSpec{
				Rules: []k8s.IngressRule{k8s.IngressRule{
					Host: ingressHost,
					IngressRuleValue: k8s.IngressRuleValue{HTTP: &k8s.HTTPIngressRuleValue{
						Paths: paths,
					}},
				}},
			},
		},
	}
}
