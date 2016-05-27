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
	// Run the controller, returning immediately after it starts or an error occurs.
	Start() error
	// Stop the controller, blocking until it stops or an error occurs.
	Stop() error
	// Healthy returns true for a healthy controller, false for unhealthy.
	Healthy() bool
}

type controller struct {
	lb            LoadBalancer
	client        k8s.Client
	watcher       k8s.Watcher
	started       bool
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
	c.startStopLock.Lock()
	defer c.startStopLock.Unlock()

	if c.started {
		return fmt.Errorf("controller is already started")
	}

	if c.watcher != nil {
		return fmt.Errorf("can't restart controller")
	}

	err := c.lb.Start()
	if err != nil {
		return fmt.Errorf("unable to start load balancer: %v", err)
	}

	c.watcher = k8s.NewWatcher()
	err = c.client.WatchIngresses(c.watcher)
	if err != nil {
		return fmt.Errorf("unable to watch ingresses: %v", err)
	}

	go c.watchForUpdates()

	c.started = true
	return nil
}

func (c *controller) watchForUpdates() {
	for {
		select {
		case <-c.watcher.Done():
			return
		case <-c.watcher.Updates():
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
	c.startStopLock.Lock()
	defer c.startStopLock.Unlock()

	if !c.started {
		return fmt.Errorf("cannot stop, not started")
	}

	log.Info("Stopping controller")

	close(c.watcher.Done())

	if err := c.lb.Stop(); err != nil {
		log.Warn("Error while stopping load balancer: ", err)
	}

	c.started = false
	log.Info("Controller has stopped")
	return nil
}

func (c *controller) Healthy() bool {
	c.startStopLock.Lock()
	defer c.startStopLock.Unlock()
	return c.started && c.lb.Healthy()
}
