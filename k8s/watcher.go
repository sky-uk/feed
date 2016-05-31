package k8s

import "github.com/sky-uk/feed/util"

// Watcher provides channels for receiving updates.
type Watcher interface {
	Updates() chan interface{}
	Done() chan struct{}
	// Health returns nil if healthy, error otherwise.
	Health() error
	SetHealth(error)
}

type watcher struct {
	updates chan interface{}
	done    chan struct{}
	health  util.SafeError
}

// NewWatcher returns an initialized watcher.
func NewWatcher() Watcher {
	return &watcher{
		updates: make(chan interface{}),
		done:    make(chan struct{}),
	}
}

func (w *watcher) Updates() chan interface{} {
	return w.updates
}

func (w *watcher) Done() chan struct{} {
	return w.done
}

func (w *watcher) Health() error {
	return w.health.Get()
}

func (w *watcher) SetHealth(err error) {
	w.health.Set(err)
}
