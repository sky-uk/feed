package ingress

import (
	"testing"

	"time"

	"fmt"

	"github.com/sky-uk/feed/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const smallWaitTime = time.Millisecond * 50

type fakeLb struct {
	mock.Mock
}

func (lb *fakeLb) Update(update LoadBalancerUpdate) (bool, error) {
	r := lb.Called(update)
	return false, r.Error(0)
}

func (lb *fakeLb) Start() error {
	r := lb.Called()
	return r.Error(0)
}

func (lb *fakeLb) Stop() error {
	r := lb.Called()
	return r.Error(0)
}

func (lb *fakeLb) String() string {
	return "FakeLoadBalancer"
}

type fakeClient struct {
	mock.Mock
}

func (c *fakeClient) GetIngresses() ([]k8s.Ingress, error) {
	r := c.Called()
	return r.Get(0).([]k8s.Ingress), r.Error(1)
}

func (c *fakeClient) WatchIngresses(w k8s.Watcher) error {
	r := c.Called(w)
	return r.Error(0)
}

func (c *fakeClient) String() string {
	return "FakeClient"
}

func createDefaultStubs() (*fakeLb, *fakeClient) {
	lb := new(fakeLb)
	client := new(fakeClient)

	client.On("GetIngresses").Return([]k8s.Ingress{}, nil)
	client.On("WatchIngresses", mock.Anything).Return(nil)
	lb.On("Start").Return(nil)
	lb.On("Stop").Return(nil)
	lb.On("Update", mock.Anything).Return(nil)

	return lb, client
}

func TestControllerCanBeStopped(t *testing.T) {
	lb, client := createDefaultStubs()
	controller := New(lb, client)

	go waitThenStop(controller)
	assert.NoError(t, controller.Start())
	lb.AssertCalled(t, "Stop")
}

func TestControllerCannotBeRestarted(t *testing.T) {
	// given
	lb, client := createDefaultStubs()
	controller := New(lb, client)

	// and
	go waitThenStop(controller)
	controller.Start()

	// then
	go waitThenStop(controller)
	assert.Error(t, controller.Start())
}

func TestControllerStartCannotBeCalledTwice(t *testing.T) {
	// given
	lb, client := createDefaultStubs()
	controller := New(lb, client)

	// expect
	go func() {
		time.Sleep(smallWaitTime)
		assert.Error(t, controller.Start())
		controller.Stop()
	}()
	assert.NoError(t, controller.Start())
}

func TestControllerIsUnhealthyUntilStarted(t *testing.T) {
	// given
	assert := assert.New(t)
	lb, client := createDefaultStubs()
	controller := New(lb, client)

	// expect
	assert.False(controller.Healthy(), "should be unhealthy until started")
	go func() { controller.Start() }()
	time.Sleep(smallWaitTime)
	assert.True(controller.Healthy(), "should be healthy after started")
	controller.Stop()
	time.Sleep(smallWaitTime)
	assert.False(controller.Healthy(), "should be unhealthy after stopped")
}

func TestLoadBalancerReturnsErrorIfWatcherFails(t *testing.T) {
	// given
	lb, _ := createDefaultStubs()
	client := new(fakeClient)
	controller := New(lb, client)
	client.On("WatchIngresses", mock.Anything).Return(fmt.Errorf("failed to watch ingresses"))

	// when
	go waitThenStop(controller)
	assert.Error(t, controller.Start())
}

func TestLoadBalancerReturnsErrorIfLoadBalancerFails(t *testing.T) {
	// given
	_, client := createDefaultStubs()
	lb := new(fakeLb)
	controller := New(lb, client)
	lb.On("Start").Return(fmt.Errorf("kaboooom"))
	lb.On("Stop").Return(nil)

	// when
	go waitThenStop(controller)
	assert.Error(t, controller.Start())
}

func TestLoadBalancerUpdatesOnIngressUpdates(t *testing.T) {
	//setup
	assert := assert.New(t)

	//given
	lb, _ := createDefaultStubs()
	client := new(fakeClient)
	ingresses := createIngressesFixture()
	controller := New(lb, client)
	watcherChan := make(chan k8s.Watcher, 1)

	client.On("GetIngresses").Return(ingresses, nil).Once()
	client.On("WatchIngresses", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		watcherChan <- args.Get(0).(k8s.Watcher)
	})

	//when
	go controller.Start()

	// wait a bit for it to start up
	time.Sleep(smallWaitTime)

	// return new set of ingresses, which should be used on update
	watcher, err := getWatcher(watcherChan, smallWaitTime)
	assert.Nil(err)
	err = sendUpdate(watcher, ingresses[0], smallWaitTime)
	assert.Nil(err)

	//then
	entries := createLbEntriesFixture()
	lb.AssertCalled(t, "Update", entries)

	//cleanup
	controller.Stop()
}

func waitThenStop(controller Controller) {
	time.Sleep(smallWaitTime)
	controller.Stop()
}

func getWatcher(watcher <-chan k8s.Watcher, d time.Duration) (k8s.Watcher, error) {
	timeoutChan := make(chan struct{})
	go func() {
		time.Sleep(d)
		close(timeoutChan)
	}()

	select {
	case w := <-watcher:
		return w, nil
	case <-timeoutChan:
		return nil, fmt.Errorf("timed out sending update")
	}
}

func sendUpdate(watcher k8s.Watcher, value interface{}, d time.Duration) error {
	timeoutChan := make(chan struct{})
	go func() {
		time.Sleep(d)
		close(timeoutChan)
	}()

	select {
	case watcher.Updates() <- value:
		time.Sleep(smallWaitTime) // give it time to be processed
	case <-timeoutChan:
		return fmt.Errorf("timed out sending update")
	}

	return nil
}

func createLbEntriesFixture() LoadBalancerUpdate {
	return LoadBalancerUpdate{[]LoadBalancerEntry{LoadBalancerEntry{
		Host:        ingressHost,
		Path:        ingressPath,
		ServiceName: ingressSvcName,
		ServicePort: ingressSvcPort,
	}}}
}

const (
	ingressHost    = "foo.sky.com"
	ingressPath    = "/foo"
	ingressSvcName = "foo-svc"
	ingressSvcPort = 80
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
			ObjectMeta: k8s.ObjectMeta{Name: "foo-ingress"},
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
