package k8s

import (
	"fmt"

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

type watcher struct {
	updates chan interface{}
	done    chan struct{}
	health  util.SafeError
}

// NewWatcher returns an initialized watcher.
func newWatcher() *watcher {
	return &watcher{
		updates: make(chan interface{}),
		done:    make(chan struct{}),
	}
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
