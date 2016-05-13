/*
Package ingress contains the ingress controller which monitors Kubernetes
and updates the ingress load balancer. It also runs the load balancer.
*/
package ingress

import (
	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/k8s"
)

// Controller for Kubernetes ingress.
type Controller interface {
	Run()
	Stop()
}

type controller struct {
	lb     LoadBalancer
	client k8s.Client
	stop   chan struct{}
}

// New creates an ingress controller.
func New(loadBalancer LoadBalancer, kubernetesClient k8s.Client) Controller {
	return &controller{
		lb:     loadBalancer,
		client: kubernetesClient,
		stop:   make(chan struct{}),
	}
}

func (c *controller) Run() {
	log.Infof("Starting controller for %v and %v", c.lb, c.client)

	watcher := k8s.NewWatcher()
	defer close(watcher.Done())

	err := c.client.WatchIngresses(watcher)
	if err != nil {
		log.Fatalf("Unable to watch ingresses: %v", err)
		c.Stop()
	} else {
		go c.watchForUpdates(watcher)
	}

	// initialize with current state of ingress
	err = c.updateLoadBalancer()
	if err != nil {
		log.Fatalf("Unable to update load balancer: %v", err)
		c.Stop()
	}

	<-c.stop
	log.Infof("Controller has stopped")
}

func (c *controller) watchForUpdates(watcher k8s.Watcher) {
	for {
		select {
		case <-c.stop:
			return
		case update := <-watcher.Updates():
			if val, ok := update.(k8s.Ingress); ok {
				log.Infof("Received ingress update for %s", val.Name)
				err := c.updateLoadBalancer()
				if err != nil {
					log.Errorf("Unable to update load balancer: %v", err)
				}
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
	return c.lb.Update(entries)
}

func (c *controller) Stop() {
	log.Info("Stopping controller")
	close(c.stop)
}
