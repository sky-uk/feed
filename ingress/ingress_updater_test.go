package ingress

import (
	"fmt"
	"testing"

	"errors"

	"github.com/sky-uk/feed/controller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type fakeProxy struct {
	mock.Mock
}

func (lb *fakeProxy) Update(update controller.IngressUpdate) (bool, error) {
	r := lb.Called(update)
	return false, r.Error(0)
}

func (lb *fakeProxy) Start() error {
	r := lb.Called()
	return r.Error(0)
}

func (lb *fakeProxy) Stop() error {
	r := lb.Called()
	return r.Error(0)
}

func (lb *fakeProxy) Health() error {
	r := lb.Called()
	return r.Error(0)
}

func (lb *fakeProxy) String() string {
	return "FakeLoadBalancer"
}

type fakeFrontend struct {
	mock.Mock
}

func (f *fakeFrontend) Attach() error {
	args := f.Called()
	return args.Error(0)
}

func (f *fakeFrontend) Detach() error {
	args := f.Called()
	return args.Error(0)
}

func (f *fakeFrontend) Health() error {
	args := f.Called()
	return args.Error(0)
}

func createDefaultStubs() (*fakeFrontend, *fakeProxy) {
	frontend := new(fakeFrontend)
	proxy := new(fakeProxy)

	frontend.On("Attach").Return(nil)
	frontend.On("Detach").Return(nil)
	frontend.On("Health").Return(nil)
	proxy.On("Start").Return(nil)
	proxy.On("Stop").Return(nil)
	proxy.On("Update", mock.Anything).Return(nil)
	proxy.On("Health").Return(nil)

	return frontend, proxy
}

func TestAttachesFrontEndOnStart(t *testing.T) {
	_, proxy := createDefaultStubs()
	frontend := new(fakeFrontend)
	lb := New(frontend, proxy)
	frontend.On("Attach").Return(nil)

	assert.NoError(t, lb.Start())
	mock.AssertExpectationsForObjects(t, frontend.Mock)
}

func TestDetachOnStop(t *testing.T) {
	_, proxy := createDefaultStubs()
	frontend := new(fakeFrontend)
	lb := New(frontend, proxy)
	frontend.On("Attach").Return(nil)
	frontend.On("Detach").Return(nil)
	lb.Start()

	assert.NoError(t, lb.Stop())
	mock.AssertExpectationsForObjects(t, frontend.Mock)
}

func TestUpdaterReturnsErrorIfProxyFails(t *testing.T) {
	// given
	frontend, _ := createDefaultStubs()
	proxy := new(fakeProxy)
	controller := New(frontend, proxy)
	proxy.On("Start").Return(fmt.Errorf("kaboooom"))
	proxy.On("Stop").Return(nil)

	// when
	assert.Error(t, controller.Start())
}

func TestUpdaterReturnsErrorIfFrontendFails(t *testing.T) {
	// given
	_, proxy := createDefaultStubs()
	frontend := new(fakeFrontend)
	controller := New(frontend, proxy)
	frontend.On("Attach").Return(fmt.Errorf("kaboooom"))

	// when
	assert.Error(t, controller.Start())
}

func TestUpdaterReturnsHealthOfProxy(t *testing.T) {
	frontend, _ := createDefaultStubs()
	proxy := new(fakeProxy)
	controller := New(frontend, proxy)
	proxy.On("Start").Return(nil)
	proxy.On("Stop").Return(nil)
	proxy.On("Health").Return(nil).Once()
	proxy.On("Health").Return(errors.New("AURGHGA"))

	assert := assert.New(t)
	assert.NoError(controller.Start())

	assert.NoError(controller.Health(), "first it's healthy")
	assert.Error(controller.Health(), "then it's not")
}

func TestUpdaterReturnsHealthOfFrontEnd(t *testing.T) {
	_, proxy := createDefaultStubs()
	frontend := new(fakeFrontend)
	controller := New(frontend, proxy)
	frontend.On("Attach").Return(nil)
	frontend.On("Health").Return(nil).Once()
	frontend.On("Health").Return(errors.New("Oh dear oh dear"))

	assert := assert.New(t)
	assert.NoError(controller.Start())

	assert.NoError(controller.Health(), "first it's healthy")
	assert.Error(controller.Health(), "then it's not")
}
