package dns

import (
	"fmt"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/k8s"
)

type dns struct {
	client        k8s.Client
	watcher       k8s.Watcher
	started       bool
	startStopLock sync.Mutex
}

// New creates a dns controller.
func New(kubernetesClient k8s.Client) controller.Controller {
	return &dns{
		client: kubernetesClient,
	}
}

func (c *dns) Start() error {
	c.startStopLock.Lock()
	defer c.startStopLock.Unlock()

	if c.started {
		return fmt.Errorf("controller is already started")
	}

	if c.watcher != nil {
		return fmt.Errorf("can't restart controller")
	}

	c.watcher = k8s.NewWatcher()
	err := c.client.WatchIngresses(c.watcher)
	if err != nil {
		return fmt.Errorf("unable to watch ingresses: %v", err)
	}

	go c.watchForUpdates()

	c.started = true
	return nil
}

func (c *dns) Stop() error {
	c.startStopLock.Lock()
	defer c.startStopLock.Unlock()

	if !c.started {
		return fmt.Errorf("cannot stop, not started")
	}

	log.Info("Stopping controller")

	close(c.watcher.Done())

	c.started = false
	log.Info("Controller has stopped")
	return nil
}

func (c *dns) Health() error {
	c.startStopLock.Lock()
	defer c.startStopLock.Unlock()

	if !c.started {
		return fmt.Errorf("controller has not started")
	}

	if err := c.watcher.Health(); err != nil {
		return err
	}

	return nil
}

func (c *dns) watchForUpdates() {
	for {
		select {
		case <-c.watcher.Done():
			return
		case <-c.watcher.Updates():
			log.Info("Received update on watcher")
			err := c.updateDNSRecords()
			if err != nil {
				log.Errorf("Unable to update dns records: %v", err)
			}
		}
	}
}

func (c *dns) updateDNSRecords() error {
	log.Info("Logic to update DNS records")
	return nil
}
