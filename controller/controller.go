/*
Package controller implements a generic controller for monitoring ingress resources in Kubernetes.
It delegates update logic to an Updater interface.
*/
package controller

import (
	"sync"

	"fmt"

	"strings"

	"strconv"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/util"
)

const ingressAllowAnnotation = "sky.uk/allow"
const frontendElbSchemeAnnotation = "sky.uk/frontend-elb-scheme"
const stripPathAnnotation = "sky.uk/strip-path"
const backendKeepAliveSeconds = "sky.uk/backend-keepalive-seconds"

// Controller operates on ingress resources, listening for updates and notifying its Updaters.
type Controller interface {
	// Run the controller, returning immediately after it starts or an error occurs.
	Start() error
	// Stop the controller, blocking until it stops or an error occurs.
	Stop() error
	// Healthy returns true for a healthy controller, false for unhealthy.
	Health() error
}

type controller struct {
	client                k8s.Client
	updaters              []Updater
	defaultAllow          []string
	defaultStripPath      bool
	defaultBackendTimeout int
	watcher               k8s.Watcher
	watcherDone           sync.WaitGroup
	started               bool
	updatesHealth         util.SafeError
	sync.Mutex
}

// Config for creating a new ingress controller.
type Config struct {
	KubernetesClient        k8s.Client
	Updaters                []Updater
	DefaultAllow            string
	DefaultStripPath        bool
	DefaultBackendKeepAlive int
}

// New creates an ingress controller.
func New(conf Config) Controller {
	return &controller{
		client:                conf.KubernetesClient,
		updaters:              conf.Updaters,
		defaultAllow:          strings.Split(conf.DefaultAllow, ","),
		defaultStripPath:      conf.DefaultStripPath,
		defaultBackendTimeout: conf.DefaultBackendKeepAlive,
	}
}

func (c *controller) Start() error {
	c.Lock()
	defer c.Unlock()

	if c.started {
		return fmt.Errorf("controller is already started")
	}

	if c.watcher != nil {
		return fmt.Errorf("can't restart controller")
	}

	for _, u := range c.updaters {
		if err := u.Start(); err != nil {
			return fmt.Errorf("unable to start %v: %v", u, err)
		}
	}

	c.watchForUpdates()

	c.started = true
	return nil
}

func (c *controller) watchForUpdates() {
	ingressWatcher := c.client.WatchIngresses()
	serviceWatcher := c.client.WatchServices()
	c.watcher = k8s.CombineWatchers(ingressWatcher, serviceWatcher)
	c.watcherDone.Add(1)
	go c.handleUpdates()
}

func (c *controller) handleUpdates() {
	defer c.watcherDone.Done()

	for range c.watcher.Updates() {
		log.Info("Received update on watcher")
		if err := c.updateIngresses(); err != nil {
			c.updatesHealth.Set(err)
			log.Errorf("Unable to update ingresses: %v", err)
		} else {
			c.updatesHealth.Set(nil)
		}
	}

	log.Debug("Controller stopped watching for updates")
}

func (c *controller) updateIngresses() error {
	ingresses, err := c.client.GetIngresses()
	log.Infof("Found %d ingresses", len(ingresses))
	if err != nil {
		return err
	}
	services, err := c.client.GetServices()
	if err != nil {
		return err
	}

	serviceMap := mapNamesToAddresses(services)

	var skipped int
	entries := []IngressEntry{}
	for _, ingress := range ingresses {
		for _, rule := range ingress.Spec.Rules {
			for _, path := range rule.HTTP.Paths {

				serviceName := serviceName{namespace: ingress.Namespace, name: path.Backend.ServiceName}

				if address := serviceMap[serviceName]; address != "" {
					entry := IngressEntry{
						Name:                    ingress.Namespace + "/" + ingress.Name,
						Host:                    rule.Host,
						Path:                    path.Path,
						ServiceAddress:          address,
						ServicePort:             int32(path.Backend.ServicePort.IntValue()),
						Allow:                   c.defaultAllow,
						ELbScheme:               ingress.Annotations[frontendElbSchemeAnnotation],
						StripPaths:              c.defaultStripPath,
						BackendKeepAliveSeconds: c.defaultBackendTimeout,
					}

					if allow, ok := ingress.Annotations[ingressAllowAnnotation]; ok {
						if allow == "" {
							entry.Allow = []string{}
						} else {
							entry.Allow = strings.Split(allow, ",")
						}
					}

					if stripPath, ok := ingress.Annotations[stripPathAnnotation]; ok {
						if stripPath == "true" {
							entry.StripPaths = true
						} else if stripPath == "false" {
							entry.StripPaths = false
						} else {
							log.Warnf("Ingress %s has an invalid strip path annotation: %s. Uing default", ingress.Name, stripPath)
						}
					}

					if backendKeepAlive, ok := ingress.Annotations[backendKeepAliveSeconds]; ok {
						tmp, _ := strconv.Atoi(backendKeepAlive)
						entry.BackendKeepAliveSeconds = tmp
					}

					if err := entry.validate(); err == nil {
						entries = append(entries, entry)
					} else {
						log.Debugf("Skipping entry: %v", err)
						skipped++
					}
				} else {
					log.Debugf("Skipping ingress as service doesn't exist: %s", serviceName)
					skipped++
				}
			}
		}
	}

	log.Infof("Updating with %d entries, skipping %d invalid", len(entries), skipped)
	update := IngressUpdate{Entries: entries}
	for _, u := range c.updaters {
		if err := u.Update(update); err != nil {
			return err
		}
	}

	return nil
}

type serviceName struct {
	namespace string
	name      string
}

func mapNamesToAddresses(services []k8s.Service) map[serviceName]string {
	m := make(map[serviceName]string)

	for _, svc := range services {
		name := serviceName{namespace: svc.Namespace, name: svc.Name}
		m[name] = svc.Spec.ClusterIP
	}

	return m
}

func (c *controller) Stop() error {
	c.Lock()
	defer c.Unlock()

	if !c.started {
		return fmt.Errorf("cannot stop, not started")
	}

	log.Info("Stopping controller")

	close(c.watcher.Done())
	c.watcherDone.Wait()

	for _, u := range c.updaters {
		if err := u.Stop(); err != nil {
			log.Warnf("Error while stopping %v: %v", u, err)
		}
	}

	c.started = false
	log.Info("Controller has stopped")
	return nil
}

func (c *controller) Health() error {
	c.Lock()
	defer c.Unlock()

	if !c.started {
		return fmt.Errorf("controller has not started")
	}

	for _, u := range c.updaters {
		if err := u.Health(); err != nil {
			return fmt.Errorf("%v: %v", u, err)
		}
	}

	if err := c.watcher.Health(); err != nil {
		return err
	}

	if err := c.updatesHealth.Get(); err != nil {
		return fmt.Errorf("updates failed to apply: %v", err)
	}

	return nil
}
