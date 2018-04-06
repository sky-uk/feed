package k8s

import (
	"sync"

	"time"

	log "github.com/sirupsen/logrus"
)

// Watcher provides channels for receiving updates. It tries its best to run forever, retrying
// if the underlying connection fails.
type Watcher interface {
	// Updates provides update notification.
	Updates() <-chan interface{}
}

type baseWatcher struct {
	updates chan interface{}
}

type watcher struct {
	baseWatcher
	sync.Mutex
}

func newBaseWatcher() baseWatcher {
	return baseWatcher{
		updates: make(chan interface{}, 1),
	}
}

// NewWatcher returns an initialized watcher.
func newWatcher() *watcher {
	return &watcher{baseWatcher: newBaseWatcher()}
}

func (w *watcher) Updates() <-chan interface{} {
	return w.updates
}

type bufferedWatcher struct {
	*watcher
	shouldUpdate bool
	sync.Mutex
}

func newBufferedWatcher(bufferTime time.Duration) *bufferedWatcher {
	b := &bufferedWatcher{watcher: newWatcher()}

	go func() {
		tick := time.Tick(bufferTime)
		for range tick {
			b.sendUpdate()
		}
	}()

	return b
}

func (b *bufferedWatcher) bufferUpdate() {
	b.Lock()
	defer b.Unlock()
	b.shouldUpdate = true
}

func (b *bufferedWatcher) sendUpdate() {
	b.Lock()
	defer b.Unlock()
	if b.shouldUpdate {
		b.shouldUpdate = false
		go func() { b.updates <- struct{}{} }()
	}
}

type combinedWatcher struct {
	baseWatcher
	watchers []Watcher
}

// CombineWatchers returns a watcher that watches all. The combined watcher becomes the owner
// of the passed in watchers and so clients should not attempt to use or stop the individual watchers.
func CombineWatchers(watchers ...Watcher) Watcher {
	combined := &combinedWatcher{baseWatcher: newBaseWatcher(), watchers: watchers}

	combiner := func(w Watcher) {
		for {
			select {
			case update := <-w.Updates():
				if update == nil {
					log.Panic("update should not be nil, did you close the watcher?")
				}
				combined.updates <- update
			}
		}
	}

	for _, w := range watchers {
		go combiner(w)
	}

	return combined
}

func (w *combinedWatcher) Updates() <-chan interface{} {
	return w.updates
}
