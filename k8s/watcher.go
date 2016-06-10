package k8s

import (
	"fmt"

	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/util"
)

// Watcher provides channels for receiving updates. It tries its best to run forever, retrying
// if the underlying connection fails. Use the Health() method to check the state of watcher.
type Watcher interface {
	// Updates provides update notification.
	Updates() <-chan interface{}
	// Done should be closed to stop watching.
	Done() chan<- struct{}
	// Health returns nil if healthy, error otherwise such as if the watch fails.
	Health() error
}

type baseWatcher struct {
	updates chan interface{}
	done    chan struct{}
}

type watcher struct {
	baseWatcher
	health util.SafeError
}

func newBaseWatcher() baseWatcher {
	return baseWatcher{
		updates: make(chan interface{}),
		done:    make(chan struct{}),
	}
}

// NewWatcher returns an initialized watcher.
func newWatcher() *watcher {
	return &watcher{baseWatcher: newBaseWatcher()}
}

func (w *watcher) Updates() <-chan interface{} {
	return w.updates
}

func (w *watcher) Done() chan<- struct{} {
	return w.done
}

func (w *watcher) Health() error {
	return w.health.Get()
}

func (w *watcher) watching() {
	w.health.Set(nil)
}

func (w *watcher) notWatching() {
	w.health.Set(fmt.Errorf("not watching"))
}

type combinedWatcher struct {
	baseWatcher
	watchers []Watcher
}

// CombineWatchers returns a watcher that watches all. The combined watcher becomes the owner
// of the passed in watchers and so clients should not attempt to use or stop the individual watchers.
func CombineWatchers(watchers ...Watcher) Watcher {
	var wg sync.WaitGroup
	combined := &combinedWatcher{baseWatcher: newBaseWatcher(), watchers: watchers}

	combiner := func(w Watcher) {
		defer wg.Done()
		defer close(w.Done())
		for {
			select {
			case update := <-w.Updates():
				if update == nil {
					log.Panic("update should not be nil, did you close the watcher?")
				}
				combined.updates <- update
			case <-combined.done:
				return
			}
		}
	}

	for _, w := range watchers {
		wg.Add(1)
		go combiner(w)
	}

	go func() {
		wg.Wait()
		close(combined.updates)
	}()

	return combined
}

func (w *combinedWatcher) Updates() <-chan interface{} {
	return w.updates
}

func (w *combinedWatcher) Done() chan<- struct{} {
	return w.done
}

func (w *combinedWatcher) Health() error {
	for _, w := range w.watchers {
		if h := w.Health(); h != nil {
			return h
		}
	}
	return nil
}
