package dns

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

func createDefaultStubs() *test.FakeClient {
	client := new(test.FakeClient)

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
	assert.Error(controller.Health(), "should be unhealthy until started")
	assert.NoError(controller.Start())
	time.Sleep(smallWaitTime)
	assert.NoError(controller.Health(), "should be healthy after started")
	assert.NoError(controller.Stop())
	time.Sleep(smallWaitTime)
	assert.Error(controller.Health(), "should be unhealthy after stopped")
}

func TestControllerReturnsErrorIfWatcherFails(t *testing.T) {
	// given
	client := new(test.FakeClient)
	controller := New(client)
	client.On("WatchIngresses", mock.Anything).Return(fmt.Errorf("failed to watch ingresses"))

	// when
	assert.Error(t, controller.Start())
}
