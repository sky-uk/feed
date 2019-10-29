/*
Package controller implements a generic controller for monitoring ingress resources in Kubernetes.
It delegates update logic to an Updater interface.
*/
package controller

import (
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
)

const (
	ingressAllowAnnotation   = "sky.uk/allow"
	frontendSchemeAnnotation = "sky.uk/frontend-scheme"

	// Deprecated: retained to maintain backwards compatibility.
	legacyFrontendElbSchemeAnnotation = "sky.uk/frontend-elb-scheme"
	stripPathAnnotation               = "sky.uk/strip-path"
	exactPathAnnotation               = "sky.uk/exact-path"

	// Deprecated: retained to maintain backwards compatibility.
	legacyBackendKeepaliveSeconds = "sky.uk/backend-keepalive-seconds"
	backendTimeoutSeconds         = "sky.uk/backend-timeout-seconds"
	proxyBufferSizeAnnotation     = "sky.uk/proxy-buffer-size-in-kb"
	proxyBufferBlocksAnnotation   = "sky.uk/proxy-buffer-blocks"

	maxAllowedProxyBufferSize   = 32
	maxAllowedProxyBufferBlocks = 8

	// sets Nginx (http://nginx.org/en/docs/http/ngx_http_upstream_module.html#max_conns)
	backendMaxConnections = "sky.uk/backend-max-connections"

	ingressClassAnnotation = "kubernetes.io/ingress.class"
)

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
	client                       k8s.Client
	updaters                     []Updater
	defaultAllow                 []string
	defaultStripPath             bool
	defaultExactPath             bool
	defaultBackendTimeout        int
	defaultBackendMaxConnections int
	defaultProxyBufferSize       int
	defaultProxyBufferBlocks     int
	watcher                      k8s.Watcher
	doneCh                       chan struct{}
	watcherDone                  sync.WaitGroup
	started                      bool
	updatesHealth                util.SafeError
	sync.Mutex
	name                      string
	includeClasslessIngresses bool
	namespaceSelector         *k8s.NamespaceSelector
}

// Config for creating a new ingress controller.
type Config struct {
	KubernetesClient             k8s.Client
	Updaters                     []Updater
	DefaultAllow                 string
	DefaultStripPath             bool
	DefaultExactPath             bool
	DefaultBackendTimeoutSeconds int
	DefaultBackendMaxConnections int
	DefaultProxyBufferSize       int
	DefaultProxyBufferBlocks     int
	Name                         string
	IncludeClasslessIngresses    bool
	NamespaceSelector            *k8s.NamespaceSelector
}

// New creates an ingress controller.
func New(conf Config) Controller {
	return &controller{
		client:                       conf.KubernetesClient,
		updaters:                     conf.Updaters,
		defaultAllow:                 strings.Split(conf.DefaultAllow, ","),
		defaultStripPath:             conf.DefaultStripPath,
		defaultExactPath:             conf.DefaultExactPath,
		defaultBackendTimeout:        conf.DefaultBackendTimeoutSeconds,
		defaultBackendMaxConnections: conf.DefaultBackendMaxConnections,
		defaultProxyBufferSize:       conf.DefaultProxyBufferSize,
		defaultProxyBufferBlocks:     conf.DefaultProxyBufferBlocks,
		doneCh:                       make(chan struct{}),
		name:                         conf.Name,
		includeClasslessIngresses:    conf.IncludeClasslessIngresses,
		namespaceSelector:            conf.NamespaceSelector,
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

	var startedUpdaters []Updater
	for _, u := range c.updaters {
		if err := u.Start(); err != nil {
			// stop all updaters started so far, to ensure clean up of any state before we bail.
			for _, started := range startedUpdaters {
				if err := started.Stop(); err != nil {
					log.Warnf("unable to stop %s: %v", u, err)
				}
			}
			return fmt.Errorf("unable to start %v: %v", u, err)
		}
		startedUpdaters = append(startedUpdaters, u)
	}

	c.watchForUpdates()

	c.started = true
	return nil
}

func (c *controller) watchForUpdates() {
	ingressWatcher := c.client.WatchIngresses()
	serviceWatcher := c.client.WatchServices()
	namespaceWatcher := c.client.WatchNamespaces()
	c.watcher = k8s.CombineWatchers(ingressWatcher, serviceWatcher, namespaceWatcher)
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

func (c *controller) updateIngresses() (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("unexpected error: %v: %v", value, string(debug.Stack()))
		}
	}()

	var ingresses []*v1beta1.Ingress
	if c.namespaceSelector == nil {
		ingresses, err = c.client.GetAllIngresses()
	} else {
		ingresses, err = c.client.GetIngresses(c.namespaceSelector)
	}
	log.Infof("Found %d ingresses", len(ingresses))
	if err != nil {
		return err
	}
	services, err := c.client.GetServices()
	if err != nil {
		return err
	}

	serviceMap := serviceNamesToClusterIPs(services)

	var skipped []string
	var entries []IngressEntry
	for _, ingress := range ingresses {
		for _, rule := range ingress.Spec.Rules {

			if rule.HTTP != nil {
				for _, path := range rule.HTTP.Paths {

					serviceName := serviceName{namespace: ingress.Namespace, name: path.Backend.ServiceName}

					if address := serviceMap[serviceName]; address == "" {
						skipped = append(skipped, fmt.Sprintf("%s/%s (service doesn't exist)", ingress.Namespace, ingress.Name))
					} else if !c.ingressClassSupported(ingress) {
						skipped = append(skipped, fmt.Sprintf("%s/%s (ingress requests class [%s]; this instance is [%s])",
							ingress.Namespace, ingress.Name, ingress.Annotations[ingressClassAnnotation], c.name))
					} else {
						entry := IngressEntry{
							Namespace:      ingress.Namespace,
							Name:           ingress.Name,
							Host:           rule.Host,
							Path:           path.Path,
							ServiceAddress: address,
							ServicePort:    int32(path.Backend.ServicePort.IntValue()),
							Allow:          c.defaultAllow,
							StripPaths:     c.defaultStripPath,
							ExactPath:      c.defaultExactPath, BackendTimeoutSeconds: c.defaultBackendTimeout,
							BackendMaxConnections: c.defaultBackendMaxConnections,
							ProxyBufferSize:       c.defaultProxyBufferSize,
							ProxyBufferBlocks:     c.defaultProxyBufferBlocks,
							CreationTimestamp:     ingress.CreationTimestamp.Time,
							Ingress:               ingress,
							IngressClass:          ingress.Annotations[ingressClassAnnotation],
						}

						log.Debugf("Found ingress to update: %s/%s", ingress.Namespace, ingress.Name)

						if lbScheme, ok := ingress.Annotations[frontendSchemeAnnotation]; ok {
							entry.LbScheme = lbScheme
						} else if legacyElbScheme, ok := ingress.Annotations[legacyFrontendElbSchemeAnnotation]; ok {
							entry.LbScheme = legacyElbScheme
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
								log.Warnf("Ingress %s/%s has an invalid strip path annotation [%s]. Using default",
									ingress.Namespace, ingress.Name, stripPath)
							}
						}

						if exactPath, ok := ingress.Annotations[exactPathAnnotation]; ok {
							if exactPath == "true" {
								entry.ExactPath = true
							} else if exactPath == "false" {
								entry.ExactPath = false
							} else {
								log.Warnf("Ingress %s/%s has an invalid exact path annotation [%s]. Using default",
									ingress.Namespace, ingress.Name, exactPath)
							}
						}

						if backendKeepAlive, ok := ingress.Annotations[legacyBackendKeepaliveSeconds]; ok {
							tmp, _ := strconv.Atoi(backendKeepAlive)
							entry.BackendTimeoutSeconds = tmp
						}

						if timeout, ok := ingress.Annotations[backendTimeoutSeconds]; ok {
							tmp, _ := strconv.Atoi(timeout)
							entry.BackendTimeoutSeconds = tmp
						}

						if maxConnections, ok := ingress.Annotations[backendMaxConnections]; ok {
							tmp, _ := strconv.Atoi(maxConnections)
							entry.BackendMaxConnections = tmp
						}

						if proxyBufferSizeString, ok := ingress.Annotations[proxyBufferSizeAnnotation]; ok {
							tmp, _ := strconv.Atoi(proxyBufferSizeString)
							entry.ProxyBufferSize = tmp
							if tmp > maxAllowedProxyBufferSize {
								log.Warnf("ProxyBufferSize value %dk exceeds the max permissible value %dk. Using %dk.", tmp, maxAllowedProxyBufferSize, maxAllowedProxyBufferSize)
								entry.ProxyBufferSize = maxAllowedProxyBufferSize
							}
						}

						if proxyBufferBlocksString, ok := ingress.Annotations[proxyBufferBlocksAnnotation]; ok {
							tmp, _ := strconv.Atoi(proxyBufferBlocksString)
							entry.ProxyBufferBlocks = tmp
							if tmp > maxAllowedProxyBufferBlocks {
								log.Warnf("ProxyBufferBlocks value %d exceeds the max permissible value %d. Using %d", tmp, maxAllowedProxyBufferBlocks, maxAllowedProxyBufferBlocks)
								entry.ProxyBufferBlocks = maxAllowedProxyBufferBlocks
							}
						}

						if err := entry.validate(); err == nil {
							entries = append(entries, entry)
						} else {
							skipped = append(skipped, fmt.Sprintf("%s (%v)", entry.NamespaceName(), err))
						}
					}
				}

			} else {
				skipped = append(skipped, fmt.Sprintf("%s/%s (HTTP key doesn't exist in this ingress definition)", ingress.Namespace, ingress.Name))
			}
		}
	}

	log.Infof("Updating with %d entries from %d total ingresses (skipped %d)", len(entries), len(ingresses), len(skipped))
	if len(skipped) > 0 {
		for _, msg := range skipped {
			log.Debugf("Skipped %s", msg)
		}
	}

	for _, u := range c.updaters {
		log.Debugf("Calling updater %v", u)
		if err := u.Update(entries); err != nil {
			return err
		}
	}

	return nil
}

func (c *controller) ingressClassSupported(ingress *v1beta1.Ingress) bool {

	isValid := false

	if ingressClass, ok := ingress.Annotations[ingressClassAnnotation]; ok {
		isValid = ingressClass == c.name
	} else {
		isValid = c.includeClasslessIngresses
	}

	return isValid
}

type serviceName struct {
	namespace string
	name      string
}

func serviceNamesToClusterIPs(services []*v1.Service) map[serviceName]string {
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
