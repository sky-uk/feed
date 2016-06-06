/*
Package ingress contains the ingress controller which monitors Kubernetes
and updates the ingress load balancer. It also runs the load balancer.
*/
package ingress

import (
	"sync"

	"fmt"

	log "github.com/Sirupsen/logrus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sky-uk/feed/api"
	"github.com/sky-uk/feed/ingress/types"
	"github.com/sky-uk/feed/k8s"
)

const ingressAllowAnnotation = "sky.uk/allow"

var attachedFrontends = prometheus.NewGauge(prometheus.GaugeOpts{
	Namespace: "feed",
	Subsystem: "ingress",
	Name:      "frontends_attached",
	Help:      "The total number of frontends attached",
})

type controller struct {
	lb            types.LoadBalancer
	client        k8s.Client
	serviceDomain string
	frontend      types.Frontend
	watcher       k8s.Watcher
	started       bool
	startStopLock sync.Mutex
}

// Config for creating a new ingress controller.
type Config struct {
	LoadBalancer     types.LoadBalancer
	KubernetesClient k8s.Client
	Frontend         types.Frontend
	ServiceDomain    string
}

// New creates an ingress controller.
func New(conf Config) api.Controller {
	return &controller{
		lb:            conf.LoadBalancer,
		client:        conf.KubernetesClient,
		serviceDomain: conf.ServiceDomain,
		frontend:      conf.Frontend,
	}
}

func (c *controller) Start() error {
	c.startStopLock.Lock()
	defer c.startStopLock.Unlock()

	prometheus.Register(attachedFrontends)

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

	frontends, err := c.frontend.Attach()
	attachedFrontends.Set(float64(frontends))

	if err != nil {
		return fmt.Errorf("unable to attach to front end %v", err)
	}

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

	entries := []types.LoadBalancerEntry{}
	for _, ingress := range ingresses {
		for _, rule := range ingress.Spec.Rules {
			for _, path := range rule.HTTP.Paths {
				serviceName := fmt.Sprintf("%s.%s.%s",
					path.Backend.ServiceName, ingress.Namespace, c.serviceDomain)
				entry := types.LoadBalancerEntry{
					Name:        ingress.Namespace + "/" + ingress.Name,
					Host:        rule.Host,
					Path:        path.Path,
					ServiceName: serviceName,
					ServicePort: int32(path.Backend.ServicePort.IntValue()),
					Allow:       ingress.Annotations[ingressAllowAnnotation],
				}

				entries = append(entries, entry)
			}
		}
	}

	log.Infof("Updating load balancer with %d entry(s)", len(entries))
	updated, err := c.lb.Update(types.LoadBalancerUpdate{Entries: entries})
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

	err := c.frontend.Detach()
	if err != nil {
		log.Warn("Error while detaching front end: ", err)
	}

	close(c.watcher.Done())

	if err = c.lb.Stop(); err != nil {
		log.Warn("Error while stopping load balancer: ", err)
	}

	c.started = false
	log.Info("Controller has stopped")
	return nil
}

func (c *controller) Health() error {
	c.startStopLock.Lock()
	defer c.startStopLock.Unlock()

	if !c.started {
		return fmt.Errorf("controller has not started")
	}

	if err := c.lb.Health(); err != nil {
		return err
	}

	if err := c.watcher.Health(); err != nil {
		return err
	}

	return nil
}
