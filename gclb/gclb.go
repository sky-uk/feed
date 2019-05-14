package gclb

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/util"
	"google.golang.org/api/compute/v1"
)

const (
	readyHealthCheckFileName = "ready"
)

// Config for GCLB
type Config struct {
	InstanceGroupPrefix                 string
	TargetPoolPrefix                    string
	ExpectedFrontends                   int
	TargetPoolConnectionDrainingTimeout time.Duration
	NginxWorkingDir                     string
}

// NewUpdater Google Cloud Load Balancer updater.
func NewUpdater(config Config) (controller.Updater, error) {
	client, err := newGLBClient()
	if err != nil {
		return nil, err
	}
	return &gclb{
		glbClient:   client,
		initialised: initialised{},
		config:      config,
	}, err
}

type gclb struct {
	glbClient                           GLBClient
	initialised                         initialised
	readyForHealthCheck                 util.SafeBool
	instance                            *Instance
	instanceGroups                      []*compute.InstanceGroup
	targetPools                         []*compute.TargetPool
	registeredFrontends                 util.SafeInt
	targetPoolConnectionDrainingTimeout time.Duration
	config                              Config
}

// Instance contains information for a given GCP instance
type Instance struct {
	Project string
	Zone    string
	Name    string
	ID      string
	Region  string
}

type initialised struct {
	sync.Mutex
	done bool
}

// Start instances a no-op. Attaching to the GCLB will happen on the first Update,
// after the nginx configuration has been set.
func (g *gclb) Start() error {
	if err := g.createHealthCheckFile(); err != nil {
		log.Errorf("unable to create ready healthcheck file: %e", err)
	}
	return nil
}

func (g *gclb) Update(controller.IngressEntries) error {
	g.initialised.Lock()
	defer g.initialised.Unlock()
	defer func() { g.readyForHealthCheck.Set(true) }()

	if !g.initialised.done {
		log.Info("First update. Attaching to front ends.")
		if g.config.TargetPoolPrefix != "" {
			if err := g.attachToTargetPools(); err != nil {
				return err
			}
		}
		if g.config.InstanceGroupPrefix != "" {
			if err := g.attachToInstanceGroups(); err != nil {
				return err
			}
		}

		g.initialised.done = true
	}
	return nil
}

func (g *gclb) Stop() error {
	var instanceGroupRemoveFailed = false
	for _, instanceGroup := range g.instanceGroups {
		log.Infof("De-registering Instance %s from gclb Instance group %s", g.instance.Name, instanceGroup.Name)
		attached, err := g.glbClient.IsInstanceInInstanceGroup(g.instance, instanceGroup.Name)
		if err != nil {
			return err
		}
		if attached {
			err := g.glbClient.RemoveInstanceFromInstanceGroup(g.instance, instanceGroup.Name)
			if err != nil {
				log.Warnf("unable to deregister Instance %s from gclb Instance group %s: %v", g.instance.Name, instanceGroup.Name, err)
				instanceGroupRemoveFailed = true
			}
		} else {
			log.Infof("Instance %q instances not in the group", g.instance.Name)
		}
	}

	/*
	   The sleep below ensures that the target pool does not close connections before nginx has. nignxUpdater has
	   already been invoked which signals nginx to gracefully shutdown.

	   Source: https://cloud.google.com/load-balancing/docs/target-pools#add_or_remove_a_backup_target_pool
	   "If you remove an instance from a target pool without draining it first, you will break all TCP sessions to that
	   instance. To remove an instance safely, first drain the instance by having it fail its health check. After TCP
	   sessions close naturally, remove the instance from the pool."
	*/
	if len(g.targetPools) > 0 {
		log.Info("gclb: nginx has received sigquit")
		if err := g.removeHealthCheckFile(); err != nil {
			log.Errorf("unable to remove ready healthcheck file: %e", err)
		}
		log.Infof("waiting %s for it to close connections before de-registering from target pool", g.config.TargetPoolConnectionDrainingTimeout.String())
		time.Sleep(g.config.TargetPoolConnectionDrainingTimeout)
	}

	var targetPoolRemoveFailed = false
	for _, targetPool := range g.targetPools {
		log.Infof("De-registering Instance %s from gclb target pool %s", g.instance.Name, targetPool.Name)
		attached, err := g.glbClient.IsInstanceInTargetPool(g.instance, targetPool.Name)
		if err != nil {
			return err
		}
		if attached {
			err := g.glbClient.RemoveInstanceFromTargetPool(g.instance, targetPool.Name)
			if err != nil {
				log.Warnf("unable to deregister instance %s from gclb target pool %s: %v", g.instance.Name, targetPool.Name, err)
				targetPoolRemoveFailed = true
			}
		} else {
			log.Infof("Instance %q instances not in the group", g.instance.Name)
		}
	}

	if instanceGroupRemoveFailed || targetPoolRemoveFailed {
		return errors.New("at least one instance could not be removed from a gclb")
	}

	return nil
}

func (g *gclb) attachToInstanceGroups() error {
	instance, err := g.glbClient.GetSelfMetadata()
	if err != nil {
		return err
	}
	log.Infof("Attaching to GCLBs from Instance %s", instance.ID)
	g.instance = instance
	instanceGroups, err := g.glbClient.FindInstanceGroups(instance.Project, instance.Zone, g.config.InstanceGroupPrefix)
	if err != nil {
		return err
	}
	g.instanceGroups = instanceGroups
	registered := 0

	for _, frontend := range instanceGroups {
		log.Infof("Registering Instance %s with gclb instance group %s", instance.Name, frontend.Name)
		attached, err := g.glbClient.IsInstanceInInstanceGroup(instance, frontend.Name)
		if err != nil {
			return err
		}
		if attached {
			// We get an error if we try to attach again
			log.Infof("Instance %q instances already attached", g.instance.Name)
		} else {
			err := g.glbClient.AddInstanceToInstanceGroup(instance, frontend.Name)
			if err != nil {
				return fmt.Errorf("unable to register instance %s with instance group %s: %v", instance.Name, frontend.Name, err)
			}
		}
		registered++
	}

	g.registeredFrontends.Set(registered)
	if g.config.ExpectedFrontends > 0 && registered != g.config.ExpectedFrontends {
		return fmt.Errorf("expected gclbs: %d actual: %d", g.config.ExpectedFrontends, registered)
	}

	return nil
}

func (g *gclb) attachToTargetPools() error {
	instance, err := g.glbClient.GetSelfMetadata()
	if err != nil {
		return err
	}
	log.Infof("Attaching to GCLBs from Instance %s", instance.ID)
	g.instance = instance
	targetPools, err := g.glbClient.FindTargetPools(instance.Project, instance.Region, g.config.TargetPoolPrefix)
	if err != nil {
		return err
	}
	g.targetPools = targetPools
	registered := 0

	for _, frontend := range targetPools {
		log.Infof("Registering Instance %s with gclb target pool %s", instance.Name, frontend.Name)
		attached, err := g.glbClient.IsInstanceInTargetPool(instance, frontend.Name)
		if err != nil {
			return err
		}
		if attached {
			// We get an error if we try to attach again
			log.Infof("Instance %q instances already attached", g.instance.Name)
		} else {
			err := g.glbClient.AddInstanceToTargetPool(instance, frontend.Name)
			if err != nil {
				return fmt.Errorf("unable to register Instance %s to target pool %s: %v", instance.Name, frontend.Name, err)
			}
		}
		registered++
	}

	g.registeredFrontends.Set(registered)
	if g.config.ExpectedFrontends > 0 && registered != g.config.ExpectedFrontends {
		return fmt.Errorf("expected gclbs: %d actual: %d", g.config.ExpectedFrontends, registered)
	}

	return nil
}

func (g *gclb) Health() error {
	if !g.readyForHealthCheck.Get() || g.config.ExpectedFrontends == g.registeredFrontends.Get() {
		return nil
	}

	return fmt.Errorf("expected gclbs: %d actual: %d", g.config.ExpectedFrontends, g.registeredFrontends.Get())
}

func (g *gclb) String() string {
	return "GCLB frontend"
}

func (g *gclb) createHealthCheckFile() error {
	if _, err := writeFile(g.getHealthCheckFilePath(), []byte{}); err != nil {
		return err
	}
	return nil
}

func (g *gclb) removeHealthCheckFile() error {
	if err := os.Remove(g.getHealthCheckFilePath()); err != nil {
		return err
	}
	return nil
}

func (g *gclb) getHealthCheckFilePath() string {
	return fmt.Sprintf("%s/%s", g.config.NginxWorkingDir, readyHealthCheckFileName)
}
func writeFile(location string, contents []byte) (bool, error) {
	err := ioutil.WriteFile(location, contents, 0644)
	if err != nil {
		return false, err
	}
	return true, nil
}
