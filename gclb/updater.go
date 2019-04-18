package gclb

import (
	"errors"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/util"
	"google.golang.org/api/compute/v1"
)

// Config for GCLB
type Config struct {
	InstanceGroupPrefix string
	ExpectedFrontends   int
	DrainDelay          time.Duration
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
	glbClient           GLBClient
	initialised         initialised
	readyForHealthCheck util.SafeBool
	instance            *Instance
	instanceGroups      []*compute.InstanceGroup
	registeredFrontends util.SafeInt
	config              Config
}

// Instance contains information for a given GCP instance
type Instance struct {
	Project string
	Zone    string
	Name    string
	ID      string
}

type initialised struct {
	sync.Mutex
	done bool
}

// Start instances a no-op. Attaching to the GCLB will happen on the first Update,
// after the nginx configuration has been set.
func (g *gclb) Start() error {
	return nil
}

func (g *gclb) Update(controller.IngressEntries) error {
	g.initialised.Lock()
	defer g.initialised.Unlock()
	defer func() { g.readyForHealthCheck.Set(true) }()

	if !g.initialised.done {
		log.Info("First update. Attaching to front ends.")
		if err := g.attachToFrontEnds(); err != nil {
			return err
		}
		g.initialised.done = true
	}
	return nil
}

func (g *gclb) Stop() error {
	var failed = false
	for _, frontend := range g.instanceGroups {
		log.Infof("De-registering Instance %s from gclb Instance group %s", g.instance.Name, frontend.Name)
		attached, err := g.glbClient.IsInstanceInGroup(g.instance, frontend.Name)
		if err != nil {
			return err
		}
		if attached {
			err := g.glbClient.RemoveInstance(g.instance, frontend.Name)
			if err != nil {
				log.Warnf("unable to deregister Instance %s from gclb Instance group %s: %v", g.instance.Name, frontend.Name, err)
				failed = true
			}
		} else {
			log.Infof("Instance %q instances not in the group", g.instance.Name)
		}
	}
	if failed {
		return errors.New("at least one gclb failed to detach")
	}

	log.Infof("Waiting %v to finish gclb deregistration", g.config.DrainDelay)
	time.Sleep(g.config.DrainDelay)

	return nil
}

func (g *gclb) attachToFrontEnds() error {
	instance, err := g.glbClient.GetSelfMetadata()
	if err != nil {
		return err
	}
	log.Infof("Attaching to GCLBs from Instance %s", instance.ID)
	g.instance = instance
	clusterFrontEnds, err := g.glbClient.FindFrontEndInstanceGroups(instance.Project, instance.Zone, g.config.InstanceGroupPrefix)
	if err != nil {
		return err
	}
	g.instanceGroups = clusterFrontEnds
	registered := 0

	for _, frontend := range clusterFrontEnds {
		log.Infof("Registering Instance %s with gclb Instance group %s", instance.Name, frontend.Name)
		attached, err := g.glbClient.IsInstanceInGroup(instance, frontend.Name)
		if err != nil {
			return err
		}
		if attached {
			// We get an error if we try to attach again
			log.Infof("Instance %q instances already attached", g.instance.Name)
		} else {
			err := g.glbClient.AddInstance(instance, frontend.Name)
			if err != nil {
				return fmt.Errorf("unable to register Instance %s with gclb %s: %v", instance.Name, frontend.Name, err)
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

	return fmt.Errorf("expected gclbss: %d actual: %d", g.config.ExpectedFrontends, g.registeredFrontends.Get())
}

func (g *gclb) String() string {
	return "GCLB frontend"
}
