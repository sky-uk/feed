package k8s

import (
	"testing"
	"time"

	"github.com/sky-uk/feed/util"
	"github.com/stretchr/testify/assert"
)

const smallWaitTime = time.Millisecond * 50

func TestWatcherUpdates(t *testing.T) {
	asserter := assert.New(t)

	w := newWatcher()
	defer close(w.updates)
	called := &util.SafeInt{}
	go func() {
		for range w.Updates() {
			called.Add(1)
		}
	}()

	w.updates <- struct{}{}
	time.Sleep(smallWaitTime)

	asserter.Equal(1, called.Get())
}

func TestBufferedWatcherBuffersUpdates(t *testing.T) {
	asserter := assert.New(t)

	b := newBufferedWatcher(smallWaitTime)
	defer close(b.updates)
	timesCalled := &util.SafeInt{}
	go func() {
		for range b.Updates() {
			timesCalled.Add(1)
		}
	}()

	for i := 0; i < 10; i++ {
		b.bufferUpdate()
	}
	time.Sleep(smallWaitTime * 2)

	asserter.Equal(1, timesCalled.Get())
}

func TestCombinedWatcherUpdates(t *testing.T) {
	asserter := assert.New(t)

	w1 := newWatcher()
	w2 := newWatcher()
	cw := CombineWatchers(w1, w2)
	called := &util.SafeInt{}
	go func() {
		for range cw.Updates() {
			called.Add(1)
		}
	}()

	w1.updates <- struct{}{}
	w2.updates <- struct{}{}
	w2.updates <- struct{}{}
	time.Sleep(smallWaitTime)

	asserter.Equal(3, called.Get())
}
