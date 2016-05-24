/*
Package ingress contains the ingress controller which monitors Kubernetes
and updates the ingress load balancer. It also runs the load balancer.
*/
package ingress

import (
	"sync"

	"fmt"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/k8s"
)

// Controller for Kubernetes ingress.
type Controller interface {
	// Run the controller, blocking until it stops. Returns an error if it failed to start.
	Start() error
	// Stop the controller, blocking until it stops. Returns an error if unable to stop.
	Stop() error
	// Healthy returns true for a healthy controller, false for unhealthy.
	Healthy() bool
}

type controller struct {
	lb            LoadBalancer
	client        k8s.Client
	stopCh        chan struct{}
	doneCh        chan struct{}
	started       safeBool
	startStopLock sync.Mutex
}

// New creates an ingress controller.
func New(loadBalancer LoadBalancer, kubernetesClient k8s.Client) Controller {
	return &controller{
		lb:     loadBalancer,
		client: kubernetesClient,
	}
}

func (c *controller) Start() error {
	watcher := k8s.NewWatcher()
	defer close(watcher.Done())

	err := c.startIt(watcher)
	if err != nil {
		return err
	}

	<-c.stopCh
	close(c.doneCh)
	return nil
}

func (c *controller) startIt(watcher k8s.Watcher) error {
	c.startStopLock.Lock()
	defer c.startStopLock.Unlock()

	if c.started.get() {
		return fmt.Errorf("controller is already started")
	}

	if c.stopCh != nil || c.doneCh != nil {
		return fmt.Errorf("can't restart controller")
	}

	err := c.lb.Start()
	if err != nil {
		return fmt.Errorf("unable to start load balancer: %v", err)
	}

	err = c.client.WatchIngresses(watcher)
	if err != nil {
		return fmt.Errorf("unable to watch ingresses: %v", err)
	}

	c.stopCh = make(chan struct{})
	c.doneCh = make(chan struct{})

	go c.watchForUpdates(watcher)

	c.started.set(true)
	return nil
}

func (c *controller) watchForUpdates(watcher k8s.Watcher) {
	for {
		select {
		case <-c.stopCh:
			return
		case <-watcher.Updates():
			log.Info("Received update on watcher")
			err := c.updateLoadBalancer()
			if err != nil {
				log.Errorf("Unable to update load balancer: %v", err)
			}
		}
	}
}

func (c *controller) updateLoadBalancer() error {
	ingresses, err := c.client.GetIngresses()
	log.Infof("Found %d ingress(es)", len(ingresses))
	if err != nil {
		return err
	}

	entries := []LoadBalancerEntry{}
	for _, ingress := range ingresses {
		for _, rule := range ingress.Spec.Rules {
			for _, path := range rule.HTTP.Paths {
				entry := LoadBalancerEntry{
					Host:        rule.Host,
					Path:        path.Path,
					ServiceName: path.Backend.ServiceName,
					ServicePort: int32(path.Backend.ServicePort.IntValue()),
				}

				entries = append(entries, entry)
			}
		}
	}

	log.Infof("Updating load balancer with %d entry(s)", len(entries))
	updated, err := c.lb.Update(LoadBalancerUpdate{entries})
	if err != nil {
		return err
	}

	if updated {
		log.Info("Load balancer updated")
	} else {
		log.Info("No changes")
	}

	return nil
}

func (c *controller) Stop() error {
	err := c.stopIt()
	if err != nil {
		return err
	}

	<-c.doneCh
	log.Infof("Controller has stopped")
	return nil
}

func (c *controller) stopIt() error {
	c.startStopLock.Lock()
	defer c.startStopLock.Unlock()

	if !c.started.get() {
		return fmt.Errorf("cannot stop, not started")
	}

	log.Info("Stopping controller")

	err := c.lb.Stop()
	if err != nil {
		log.Warn("Error while stopping load balancer: ", err)
	}

	close(c.stopCh)
	c.started.set(false)

	return nil
}

func (c *controller) Healthy() bool {
	return c.started.get()
}

type safeBool struct {
	lock sync.Mutex
	val  bool
}

func (b *safeBool) get() bool {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.val
}

func (b *safeBool) set(newVal bool) {
	b.lock.Lock()
	b.val = newVal
	b.lock.Unlock()
}
