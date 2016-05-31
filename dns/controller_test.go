package dns

import (
	"testing"

	"time"

	"fmt"

	"github.com/sky-uk/feed/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const smallWaitTime = time.Millisecond * 50

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

func createDefaultStubs() (*fakeClient) {
	client := new(fakeClient)

	client.On("GetIngresses").Return([]k8s.Ingress{}, nil)
	client.On("WatchIngresses", mock.Anything).Return(nil)

	return client
}

func TestControllerCanBeStopped(t *testing.T) {
	assert := assert.New(t)
	client := createDefaultStubs()
	controller := New(client)

	assert.NoError(controller.Start())
	assert.NoError(controller.Stop())
}

func TestControllerCannotBeRestarted(t *testing.T) {
	// given
	assert := assert.New(t)
	client := createDefaultStubs()
	controller := New(client)

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
	client := createDefaultStubs()
	controller := New(client)

	// expect
	assert.NoError(controller.Start())
	assert.Error(controller.Start())
	assert.NoError(controller.Stop())
}

func TestControllerIsUnhealthyUntilStarted(t *testing.T) {
	// given
	assert := assert.New(t)
	client := createDefaultStubs()
	controller := New(client)

	// expect
	assert.False(controller.Healthy(), "should be unhealthy until started")
	assert.NoError(controller.Start())
	time.Sleep(smallWaitTime)
	assert.True(controller.Healthy(), "should be healthy after started")
	assert.NoError(controller.Stop())
	time.Sleep(smallWaitTime)
	assert.False(controller.Healthy(), "should be unhealthy after stopped")
}

func TestControllerReturnsErrorIfWatcherFails(t *testing.T) {
	// given
	client := new(fakeClient)
	controller := New(client)
	client.On("WatchIngresses", mock.Anything).Return(fmt.Errorf("failed to watch ingresses"))

	// when
	assert.Error(t, controller.Start())
}
