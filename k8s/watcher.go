package k8s

// Watcher provides channels for receiving updates.
type Watcher interface {
	Updates() chan interface{}
	Done() chan struct{}
}

type watcher struct {
	updates chan interface{}
	done    chan struct{}
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
