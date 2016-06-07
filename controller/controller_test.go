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

func createDefaultStubs() (*fakeUpdater, *test.FakeClient) {
	updater := new(fakeUpdater)
	client := new(test.FakeClient)

	client.On("GetIngresses").Return([]k8s.Ingress{}, nil)
	client.On("WatchIngresses", mock.Anything).Return(nil)
	updater.On("Start").Return(nil)
	updater.On("Stop").Return(nil)
	updater.On("Update", mock.Anything).Return(nil)
	updater.On("Health").Return(nil)

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

func TestControllerReturnsErrorIfWatcherFails(t *testing.T) {
	// given
	lb, _ := createDefaultStubs()
	client := new(test.FakeClient)
	controller := newController(lb, client)
	client.On("WatchIngresses", mock.Anything).Return(fmt.Errorf("failed to watch ingresses"))

	// when
	assert.Error(t, controller.Start())
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

	watcherChan := make(chan k8s.Watcher, 1)
	client.On("WatchIngresses", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		fmt.Println("WatchIngresses called")
		watcherChan <- args.Get(0).(k8s.Watcher)
	})
	assert.NoError(controller.Start())

	// when
	watcher, err := getWatcher(watcherChan, smallWaitTime)
	assert.NoError(err)
	watcherErr := fmt.Errorf("not watching for updates")
	watcher.SetHealth(watcherErr)

	// then
	assert.Equal(watcherErr, controller.Health())
}

func TestUpdatesOnIngressUpdates(t *testing.T) {
	//setup
	assert := assert.New(t)

	//given
	client := new(test.FakeClient)
	updater, _ := createDefaultStubs()
	ingresses := createIngressesFixture()
	controller := newController(updater, client)
	watcherChan := make(chan k8s.Watcher, 1)

	client.On("GetIngresses").Return(ingresses, nil).Once()
	client.On("WatchIngresses", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		watcherChan <- args.Get(0).(k8s.Watcher)
	})

	//when
	assert.NoError(controller.Start())

	// wait a bit for it to start up
	time.Sleep(smallWaitTime)

	// return new set of ingresses, which should be used on update
	watcher, err := getWatcher(watcherChan, smallWaitTime)
	assert.Nil(err)
	err = sendUpdate(watcher, ingresses[0], smallWaitTime)
	assert.Nil(err)

	//then
	entries := createLbEntriesFixture()
	updater.AssertCalled(t, "Update", entries)

	//cleanup
	assert.NoError(controller.Stop())
}

func getWatcher(watcher <-chan k8s.Watcher, d time.Duration) (k8s.Watcher, error) {
	t := time.NewTimer(d)
	select {
	case w := <-watcher:
		return w, nil
	case <-t.C:
		return nil, fmt.Errorf("timed out waiting for watcher")
	}
}

func sendUpdate(watcher k8s.Watcher, value interface{}, d time.Duration) error {
	t := time.NewTimer(d)
	select {
	case watcher.Updates() <- value:
		time.Sleep(smallWaitTime) // give it time to be processed
	case <-t.C:
		return fmt.Errorf("timed out sending update")
	}

	return nil
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
