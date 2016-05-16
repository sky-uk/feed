package ingress

import (
	"testing"

	"time"

	"fmt"

	log "github.com/Sirupsen/logrus"
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
	return nil
}

func (lb *fakeLb) Stop() error {
	return nil
}

func (lb *fakeLb) WaitFor() error {
	return nil
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

func TestControllerCanBeStopped(t *testing.T) {
	// given
	lb := new(fakeLb)
	client := new(fakeClient)
	controller := New(lb, client)
	client.On("GetIngresses").Return([]k8s.Ingress{}, nil)
	client.On("WatchIngresses", mock.Anything).Return(nil)
	lb.On("Update", mock.Anything).Return(nil)

	// when
	go waitThenStop(controller)
	controller.Run()
}

func TestLoadBalancerUpdatesWithInitialIngress(t *testing.T) {
	// given
	lb := new(fakeLb)
	client := new(fakeClient)
	ingresses := createIngressesFixture()
	controller := New(lb, client)

	client.On("GetIngresses").Return(ingresses, nil)
	client.On("WatchIngresses", mock.Anything).Return(nil)
	lb.On("Update", mock.Anything).Return(nil)

	// when
	go controller.Run()
	waitThenStop(controller)

	// then
	entries := createLbEntriesFixture()
	lb.AssertCalled(t, "Update", entries)
}

func TestLoadBalancerUpdatesOnIngressUpdates(t *testing.T) {
	//setup
	assert := assert.New(t)
	log.SetLevel(log.DebugLevel)

	//given
	lb := new(fakeLb)
	client := new(fakeClient)
	ingresses := createIngressesFixture()
	controller := New(lb, client)
	watcherChan := make(chan k8s.Watcher, 1)

	client.On("GetIngresses").Return([]k8s.Ingress{}, nil).Once()
	client.On("WatchIngresses", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		watcherChan <- args.Get(0).(k8s.Watcher)
	})
	lb.On("Update", mock.Anything).Return(nil)

	//when
	go controller.Run()

	// wait a bit for initial ingress selection
	time.Sleep(smallWaitTime)

	// return new set of ingresses, which should be used on update
	client.On("GetIngresses").Return(ingresses, nil)
	watcher, err := getWatcher(watcherChan, smallWaitTime)
	assert.Nil(err)
	err = sendUpdate(watcher, ingresses[0], smallWaitTime)
	assert.Nil(err)

	//then
	entries := createLbEntriesFixture()
	lb.AssertCalled(t, "Update", LoadBalancerUpdate{[]LoadBalancerEntry{}})
	lb.AssertCalled(t, "Update", entries)

	//cleanup
	controller.Stop()
	log.SetLevel(log.InfoLevel)
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
