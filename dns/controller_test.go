package dns

import (
	"testing"

	"time"

	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/util/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const smallWaitTime = time.Millisecond * 50

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

func createDefaultStubs() *test.FakeClient {
	client := new(test.FakeClient)
	watcher, updateCh, doneCh := createFakeWatcher()

	client.On("GetIngresses").Return([]k8s.Ingress{}, nil)
	client.On("WatchIngresses").Return(watcher)

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
