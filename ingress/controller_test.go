package ingress

import (
	"testing"

	"time"

	"fmt"

	"github.com/sky-uk/feed/api"
	"github.com/sky-uk/feed/ingress/types"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/util/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const smallWaitTime = time.Millisecond * 50

type fakeLb struct {
	mock.Mock
}

func (lb *fakeLb) Update(update types.LoadBalancerUpdate) (bool, error) {
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

func (lb *fakeLb) Health() error {
	r := lb.Called()
	return r.Error(0)
}

func (lb *fakeLb) String() string {
	return "FakeLoadBalancer"
}

type fakeFrontend struct {
	mock.Mock
}

func (f *fakeFrontend) Attach(i types.FrontendInput) (int, error) {
	return 0, nil
}

func (f *fakeFrontend) Detach(i types.FrontendInput) error {
	return nil
}

func createDefaultStubs() (*fakeLb, *test.FakeClient, *fakeFrontend) {
	lb := new(fakeLb)
	client := new(test.FakeClient)
	frontend := new(fakeFrontend)

	client.On("GetIngresses").Return([]k8s.Ingress{}, nil)
	client.On("WatchIngresses", mock.Anything).Return(nil)
	lb.On("Start").Return(nil)
	lb.On("Stop").Return(nil)
	lb.On("Update", mock.Anything).Return(nil)
	lb.On("Health").Return(nil)

	return lb, client, frontend
}

func newController(lb types.LoadBalancer, client k8s.Client, frontend types.Frontend) api.Controller {
	return New(Config{LoadBalancer: lb, KubernetesClient: client, ServiceDomain: serviceDomain, Frontend: frontend})
}

func TestControllerCanBeStopped(t *testing.T) {
	assert := assert.New(t)
	lb, client, frontend := createDefaultStubs()
	controller := newController(lb, client, frontend)

	assert.NoError(controller.Start())
	assert.NoError(controller.Stop())
	lb.AssertCalled(t, "Stop")
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

func TestControllerIsUnhealthyIfLBIsUnhealthy(t *testing.T) {
	assert := assert.New(t)
	_, client, frontend := createDefaultStubs()
	lb := new(fakeLb)
	controller := newController(lb, client, frontend)

	lb.On("Start").Return(nil)
	lb.On("Stop").Return(nil)
	lb.On("Update", mock.Anything).Return(nil)
	// first return healthy, then unhealthy for lb
	lb.On("Health").Return(nil).Once()
	lbErr := fmt.Errorf("dead")
	lb.On("Health").Return(fmt.Errorf("dead")).Once()

	assert.NoError(controller.Start())
	assert.NoError(controller.Health())
	assert.Equal(lbErr, controller.Health())
}

func TestLoadBalancerReturnsErrorIfWatcherFails(t *testing.T) {
	// given
	lb, _, frontend := createDefaultStubs()
	client := new(test.FakeClient)
	controller := newController(lb, client, frontend)
	client.On("WatchIngresses", mock.Anything).Return(fmt.Errorf("failed to watch ingresses"))

	// when
	assert.Error(t, controller.Start())
}

func TestLoadBalancerReturnsErrorIfLoadBalancerFails(t *testing.T) {
	// given
	_, client, frontend := createDefaultStubs()
	lb := new(fakeLb)
	controller := newController(lb, client, frontend)
	lb.On("Start").Return(fmt.Errorf("kaboooom"))
	lb.On("Stop").Return(nil)

	// when
	assert.Error(t, controller.Start())
}

func TestUnhealthyIfNotWatchingForUpdates(t *testing.T) {
	// given
	assert := assert.New(t)
	lb, _, frontend := createDefaultStubs()
	client := new(test.FakeClient)
	controller := newController(lb, client, frontend)

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

func TestLoadBalancerUpdatesOnIngressUpdates(t *testing.T) {
	//setup
	assert := assert.New(t)

	//given
	client := new(test.FakeClient)
	lb, _, frontend := createDefaultStubs()
	ingresses := createIngressesFixture()
	controller := newController(lb, client, frontend)
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
	lb.AssertCalled(t, "Update", entries)

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

func createLbEntriesFixture() types.LoadBalancerUpdate {
	return types.LoadBalancerUpdate{Entries: []types.LoadBalancerEntry{types.LoadBalancerEntry{
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
				Name:        "foo-ingress",
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
