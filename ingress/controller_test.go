package ingress

import (
	"testing"

	"time"

	"fmt"

	"github.com/sky-uk/feed/ingress/api"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const smallWaitTime = time.Millisecond * 50

type fakeLb struct {
	mock.Mock
}

func (lb *fakeLb) Update(update api.LoadBalancerUpdate) (bool, error) {
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
	lb.On("Health").Return(nil)

	return lb, client
}

func newController(lb api.LoadBalancer, client k8s.Client) util.Controller {
	return New(Config{LoadBalancer: lb, KubernetesClient: client, ServiceDomain: serviceDomain})
}

func TestControllerCanBeStopped(t *testing.T) {
	assert := assert.New(t)
	lb, client := createDefaultStubs()
	controller := newController(lb, client)

	assert.NoError(controller.Start())
	assert.NoError(controller.Stop())
	lb.AssertCalled(t, "Stop")
}

func TestControllerCannotBeRestarted(t *testing.T) {
	// given
	assert := assert.New(t)
	lb, client := createDefaultStubs()
	controller := newController(lb, client)

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
	lb, client := createDefaultStubs()
	controller := newController(lb, client)

	// expect
	assert.NoError(controller.Start())
	assert.Error(controller.Start())
	assert.NoError(controller.Stop())
}

func TestControllerIsUnhealthyUntilStarted(t *testing.T) {
	// given
	assert := assert.New(t)
	lb, client := createDefaultStubs()
	controller := newController(lb, client)

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
	_, client := createDefaultStubs()
	lb := new(fakeLb)
	controller := newController(lb, client)

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
	lb, _ := createDefaultStubs()
	client := new(fakeClient)
	controller := newController(lb, client)
	client.On("WatchIngresses", mock.Anything).Return(fmt.Errorf("failed to watch ingresses"))

	// when
	assert.Error(t, controller.Start())
}

func TestLoadBalancerReturnsErrorIfLoadBalancerFails(t *testing.T) {
	// given
	_, client := createDefaultStubs()
	lb := new(fakeLb)
	controller := newController(lb, client)
	lb.On("Start").Return(fmt.Errorf("kaboooom"))
	lb.On("Stop").Return(nil)

	// when
	assert.Error(t, controller.Start())
}

func TestUnhealthyIfNotWatchingForUpdates(t *testing.T) {
	// given
	assert := assert.New(t)
	lb, _ := createDefaultStubs()
	client := new(fakeClient)
	controller := newController(lb, client)

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
	lb, _ := createDefaultStubs()
	client := new(fakeClient)
	ingresses := createIngressesFixture()
	controller := newController(lb, client)
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

func createLbEntriesFixture() api.LoadBalancerUpdate {
	return api.LoadBalancerUpdate{Entries: []api.LoadBalancerEntry{api.LoadBalancerEntry{
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
