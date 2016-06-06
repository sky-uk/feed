/*
Package controller implements a generic controller for monitoring ingress resources in Kubernetes.
It delegates update logic to an Updater interface.
*/
package controller

import (
	"sync"

	"fmt"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/k8s"
)

const ingressAllowAnnotation = "sky.uk/allow"

// Controller operates on ingress resources.
type Controller interface {
	// Run the controller, returning immediately after it starts or an error occurs.
	Start() error
	// Stop the controller, blocking until it stops or an error occurs.
	Stop() error
	// Healthy returns true for a healthy controller, false for unhealthy.
	Health() error
}

type controller struct {
	updater       Updater
	client        k8s.Client
	watcher       k8s.Watcher
	started       bool
	startStopLock sync.Mutex
	serviceDomain string
}

// Config for creating a new ingress controller.
type Config struct {
	Updater          Updater
	KubernetesClient k8s.Client
	ServiceDomain    string
}

// New creates an ingress controller.
func New(conf Config) Controller {
	return &controller{
		updater:       conf.Updater,
		client:        conf.KubernetesClient,
		serviceDomain: conf.ServiceDomain,
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

	err := c.updater.Start()
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

	entries := []IngressEntry{}
	for _, ingress := range ingresses {
		for _, rule := range ingress.Spec.Rules {
			for _, path := range rule.HTTP.Paths {
				serviceName := fmt.Sprintf("%s.%s.%s",
					path.Backend.ServiceName, ingress.Namespace, c.serviceDomain)
				entry := IngressEntry{
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
	if err := c.updater.Update(IngressUpdate{Entries: entries}); err != nil {
		return err
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

	if err := c.updater.Stop(); err != nil {
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

	if err := c.updater.Health(); err != nil {
		return err
	}

	if err := c.watcher.Health(); err != nil {
		return err
	}

	return nil
}
