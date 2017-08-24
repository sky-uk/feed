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

	"errors"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/util"
	"k8s.io/client-go/pkg/api/v1"
)

const ingressAllowAnnotation = "sky.uk/allow"
const frontendSchemeAnnotation = "sky.uk/frontend-scheme"
const frontendElbSchemeAnnotation = "sky.uk/frontend-elb-scheme"
const stripPathAnnotation = "sky.uk/strip-path"

// Old annotation - still supported to maintain backwards compatibility.
const legacyBackendKeepAliveSeconds = "sky.uk/backend-keepalive-seconds"
const backendTimeoutSeconds = "sky.uk/backend-timeout-seconds"

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
	doneCh                chan struct{}
	watcherDone           sync.WaitGroup
	started               bool
	updatesHealth         util.SafeError
	sync.Mutex
}

// Config for creating a new ingress controller.
type Config struct {
	KubernetesClient             k8s.Client
	Updaters                     []Updater
	DefaultAllow                 string
	DefaultStripPath             bool
	DefaultBackendTimeoutSeconds int
}

// New creates an ingress controller.
func New(conf Config) Controller {
	return &controller{
		client:                conf.KubernetesClient,
		updaters:              conf.Updaters,
		defaultAllow:          strings.Split(conf.DefaultAllow, ","),
		defaultStripPath:      conf.DefaultStripPath,
		defaultBackendTimeout: conf.DefaultBackendTimeoutSeconds,
		doneCh:                make(chan struct{}),
	}
}

func (c *controller) Start() error {
	c.Lock()
	defer c.Unlock()

	if c.started {
		return errors.New("controller is already started")
	}

	if c.watcher != nil {
		return errors.New("can't restart controller")
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
	defer log.Debug("Controller stopped watching for updates")

	for {
		select {
		case <-c.watcher.Updates():
			log.Info("Received update on watcher")
			if err := c.updateIngresses(); err != nil {
				c.updatesHealth.Set(err)
				log.Errorf("Unable to update ingresses: %v", err)
			} else {
				c.updatesHealth.Set(nil)
			}
		case <-c.doneCh:
			return
		}
	}
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

	var skipped []string
	entries := []IngressEntry{}
	for _, ingress := range ingresses {
		for _, rule := range ingress.Spec.Rules {
			for _, path := range rule.HTTP.Paths {

				serviceName := serviceName{namespace: ingress.Namespace, name: path.Backend.ServiceName}

				if address := serviceMap[serviceName]; address != "" {
					entry := IngressEntry{
						Namespace:               ingress.Namespace,
						Name:                    ingress.Name,
						Host:                    rule.Host,
						Path:                    path.Path,
						ServiceAddress:          address,
						ServicePort:             int32(path.Backend.ServicePort.IntValue()),
						Allow:                   c.defaultAllow,
						StripPaths:              c.defaultStripPath,
						BackendTimeoutSeconds: c.defaultBackendTimeout,
						CreationTimestamp:       ingress.CreationTimestamp.Time,
					}

					log.Infof("Found ingress to update: %s", ingress.Name)

					if elbScheme, ok := ingress.Annotations[frontendSchemeAnnotation]; ok {
						entry.ELbScheme = elbScheme
					} else {
						entry.ELbScheme = ingress.Annotations[frontendElbSchemeAnnotation]
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

					if backendKeepAlive, ok := ingress.Annotations[legacyBackendKeepAliveSeconds]; ok {
						tmp, _ := strconv.Atoi(backendKeepAlive)
						entry.BackendTimeoutSeconds = tmp
					}

					if backendKeepAlive, ok := ingress.Annotations[backendTimeoutSeconds]; ok {
						tmp, _ := strconv.Atoi(backendKeepAlive)
						entry.BackendTimeoutSeconds = tmp
					}

					if err := entry.validate(); err == nil {
						entries = append(entries, entry)
					} else {
						skipped = append(skipped, fmt.Sprintf("%s (%v)", entry.NamespaceName(), err))
					}
				} else {
					skipped = append(skipped, fmt.Sprintf("%s/%s (service doesn't exist)", ingress.Namespace, ingress.Name))
				}
			}
		}
	}

	log.Infof("Updating with %d entries", len(entries))
	if len(skipped) > 0 {
		log.Infof("Skipped %d invalid: %s", len(skipped), strings.Join(skipped, ", "))
	}

	for _, u := range c.updaters {
		if err := u.Update(entries); err != nil {
			return err
		}
	}

	return nil
}

type serviceName struct {
	namespace string
	name      string
}

func mapNamesToAddresses(services []*v1.Service) map[serviceName]string {
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
		return errors.New("cannot stop, not started")
	}

	log.Info("Stopping controller")
	close(c.doneCh)

	for i := range c.updaters {
		u := c.updaters[len(c.updaters)-1-i]
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
		return errors.New("controller has not started")
	}

	for _, u := range c.updaters {
		if err := u.Health(); err != nil {
			return fmt.Errorf("%v: %v", u, err)
		}
	}

	if err := c.updatesHealth.Get(); err != nil {
		return fmt.Errorf("updates failed to apply: %v", err)
	}

	return nil
}
