package dns

import (
	"fmt"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/k8s"
)

// Controller for dns controller
type Controller interface {
	// Run the controller, returning immediately after it starts or an error occurs.
	Start() error
	// Stop the controller, blocking until it stops or an error occurs.
	Stop() error
	// Healthy returns true for a healthy controller, false for unhealthy.
	Healthy() bool
}

type controller struct {
	client        k8s.Client
	watcher       k8s.Watcher
	started       bool
	startStopLock sync.Mutex
}

// New creates an dns controller.
func New(kubernetesClient k8s.Client) Controller {
	return &controller{
		client: kubernetesClient,
	}
}

func (c *controller) Start() error {
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

func (c *controller) Stop() error {
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

func (c *controller) Healthy() bool {
	c.startStopLock.Lock()
	defer c.startStopLock.Unlock()
	return true
}

func (c *controller) watchForUpdates() {
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

func (c *controller) updateDNSRecords() error {
	log.Info("Logic to update DNS records")
	return nil
}
