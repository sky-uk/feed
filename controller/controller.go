/*
Package controller implements a generic controller for monitoring ingress resources in Kubernetes.
It delegates update logic to an Updater interface.
*/
package controller

import (
	"strconv"
	"sync"

	"fmt"

	"strings"

	"errors"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/util"
	"k8s.io/client-go/pkg/api/v1"
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
	doneCh                chan struct{}
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
	endpointWatcher := c.client.WatchEndpoints()
	c.watcher = k8s.CombineWatchers(ingressWatcher, endpointWatcher)
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
	endpoints, err := c.client.GetEndpoints()
	if err != nil {
		return err
	}

	services := mapEndpointsToServices(endpoints)

	var skipped []string
	var entries []IngressEntry
	for _, ingress := range ingresses {
		for _, rule := range ingress.Spec.Rules {
			for _, path := range rule.HTTP.Paths {

				serviceKey := serviceKey{namespace: ingress.Namespace, name: path.Backend.ServiceName,
					port: int32(path.Backend.ServicePort.IntValue())}

				service, exists := services[serviceKey]
				if !exists {
					skipped = append(skipped, fmt.Sprintf("%s/%s (service doesn't exist)", ingress.Namespace, ingress.Name))
					continue
				}

				entry := IngressEntry{
					Namespace:               ingress.Namespace,
					Name:                    ingress.Name,
					Host:                    rule.Host,
					Path:                    path.Path,
					Service:                 service,
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
					b, err := strconv.ParseBool(stripPath)
					if err != nil {
						log.Warnf("Ingress %s has an invalid strip path annotation: %s. Using default", ingress.Name, stripPath)
					} else {
						entry.StripPaths = b
					}
				}

				if backendKeepAlive, ok := ingress.Annotations[backendKeepAliveSeconds]; ok {
					tmp, _ := strconv.Atoi(backendKeepAlive)
					entry.BackendKeepAliveSeconds = tmp
				}

				if err := validate(&entry); err != nil {
					skipped = append(skipped, fmt.Sprintf("%s (%v)", entry.NamespaceName(), err))
					continue
				}

				entries = append(entries, entry)
			}
		}
	}

	log.Infof("Updating with %d entries", len(entries))
	if len(skipped) > 0 {
		log.Infof("Skipped %d invalid: %s", len(skipped), strings.Join(skipped, ", "))
	}

	update := IngressUpdate{Entries: entries}
	for _, u := range c.updaters {
		if err := u.Update(update); err != nil {
			return err
		}
	}

	return nil
}

func validate(e *IngressEntry) error {
	if e.Host == "" {
		return errors.New("missing host")
	}
	if len(e.Service.Addresses) == 0 {
		return errors.New("no service endpoints")
	}
	if e.Service.Port == 0 {
		return errors.New("missing service port")
	}
	return nil
}

type serviceKey struct {
	namespace string
	name      string
	port      int32
}

func mapEndpointsToServices(multipleEndpoints []*v1.Endpoints) map[serviceKey]Service {
	m := make(map[serviceKey]Service)

	for _, endpoints := range multipleEndpoints {
		for _, subset := range endpoints.Subsets {
			for _, port := range subset.Ports {

				if port.Protocol == v1.ProtocolUDP {
					continue
				}

				key := serviceKey{namespace: endpoints.Namespace, name: endpoints.Name, port: port.Port}

				var addresses []string
				for _, address := range subset.Addresses {
					addresses = append(addresses, address.IP)
				}

				service := Service{
					Name:      endpoints.Name,
					Port:      port.Port,
					Addresses: addresses,
				}

				m[key] = service
			}
		}
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
