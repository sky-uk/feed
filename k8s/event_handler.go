package k8s

import (
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/cache"
)

type handlerWatcher struct {
	*bufferedWatcher
}

// Implement cache.ResourceEventHandler
var _ cache.ResourceEventHandler = &handlerWatcher{}

func (w *handlerWatcher) notify() {
	w.bufferUpdate()
}

func (w *handlerWatcher) OnAdd(obj interface{}) {
	log.Debugf("OnAdd called for %v - updating watcher", obj)
	go w.notify()
}

func (w *handlerWatcher) OnUpdate(old interface{}, new interface{}) {
	log.Debugf("OnUpdate called for %v to %v - updating watcher", old, new)
	go w.notify()
}

func (w *handlerWatcher) OnDelete(obj interface{}) {
	log.Debugf("OnDelete called for %v - updating watcher", obj)
	go w.notify()
}

type eventHandlerFactory interface {
	createBufferedHandler(bufferTime time.Duration) *handlerWatcher
}

type bufferedEventHandlerFactory struct {
}

// Implement eventHandlerFactory
var _ eventHandlerFactory = &bufferedEventHandlerFactory{}

func (hf *bufferedEventHandlerFactory) createBufferedHandler(bufferTime time.Duration) *handlerWatcher {
	return &handlerWatcher{bufferedWatcher: newBufferedWatcher(bufferTime)}
}
