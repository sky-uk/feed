package gclb

import (
	"fmt"
	"sync"
	"errors"
	"context"

	"golang.org/x/oauth2/google"
	"github.com/sky-uk/feed/controller"
	"cloud.google.com/go/compute/metadata"
	"google.golang.org/api/compute/v1"
	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/util"
	"strings"
	"time"
)

// Config for GCLB
type Config struct {
	InstanceGroupPrefix string
	ExpectedFrontends   int
	DrainDelay          time.Duration
}

// New GCLB gclb.
func New(config Config) (controller.Updater, error) {
	ctx := context.Background()
	client, err := google.DefaultClient(ctx, compute.ComputeScope)
	if err != nil {
		return nil, fmt.Errorf("unable to create http client with compute scope: %v", err)
	}
	computeService, err := compute.New(client)
	if err != nil {
		return nil, fmt.Errorf("unable to create compute service client: %v", err)
	}
	return &gclb{
		instanceGroupService: &igs{computeService.InstanceGroups,}
		initialised:          initialised{},
		config:               config,
	}, errors.New("not implemented")
}

// GCPInstanceGroupService interface to allow mocking of real calls to GCP as well as restricting the underlying API to
// only the instances we use
type GCPInstanceGroupService interface {
	RemoveInstances(project string, zone string, instanceGroup string, request *compute.InstanceGroupsRemoveInstancesRequest) error
	AddInstances(project string, zone string, instanceGroup string, request *compute.InstanceGroupsAddInstancesRequest) error
	List(project string, zone string)(*compute.InstanceGroupList, error)
    ListInstances(project string, zone string, instanceGroup string, request *compute.InstanceGroupsListInstancesRequest) (*compute.InstanceGroupsListInstances, error)
}

type igs struct {
	*compute.InstanceGroupsService
}

// GCPInstanceService interface to allow mocking of real calls to GCP as well as restricting the underlying API to
// only the instances we use
type GCPInstanceService interface {
}

// GCPMetadata interface to allow mocking of real calls to GCP as well as restricting the underlying API to
// only the instances we use
type GCPMetadata interface {
	ProjectID() (string, error)
	InstanceName() (string, error)
	Zone() (string, error)
}

type gcpMetadata struct {
}

func (m *gcpMetadata) ProjectID() (string, error) {
	return metadata.ProjectID()
}
func (m *gcpMetadata) InstanceName() (string, error) {
	return metadata.InstanceName()
}
func (m *gcpMetadata) Zone() (string, error) {
	return metadata.Zone()
}

type gclb struct {
	instanceGroupService GCPInstanceGroupService
	instanceService      *compute.InstancesService
	initialised          initialised
	readyForHealthCheck  util.SafeBool
	instance             *instance
	instanceGroups       []*compute.InstanceGroup
	registeredFrontends  util.SafeInt
	config               Config
}

type instance struct {
	project string
	zone    string
	name    string
	ID      string
}

type initialised struct {
	sync.Mutex
	done bool
}

// Start is a no-op. Attaching to the GCLB will happen on the first Update,
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
		log.Infof("Deregistering instance %s from gclb instance group %s", g.instance.name, frontend.Name)
		attached, err := g.isAlreadyAttached(frontend.Name)
		if err != nil {
			return err
		}
		if attached {
			removals := []*compute.InstanceReference{{Instance: g.instance.ID,}}
			_, err := g.instanceGroupService.RemoveInstances(
				g.instance.project,
				g.instance.zone,
				frontend.Name,
				&compute.InstanceGroupsRemoveInstancesRequest{
					Instances: removals,
				}).Do()
			if err != nil {
				log.Warnf("unable to deregister instance %s from gclb instance group %s: %v", g.instance.name, frontend.Name, err)
				failed = true
			}
		} else {
			log.Infof("Instance %q is not in the group")
		}
	}
	if failed {
		return errors.New("at least one gclb failed to detach")
	}

	log.Infof("Waiting %v to finish gclb deregistration", g.config.DrainDelay)
	time.Sleep(g.config.DrainDelay)

	return nil
	return errors.New("not implemented")
}

func (g *gclb) attachToFrontEnds() error {
	instance, err := g.getInstance()
	if err != nil {
		return err
	}
	log.Infof("Attaching to GCLBs from instance %s", instance.ID)
	g.instance = instance
	clusterFrontEnds, err := g.findFrontEndInstanceGroups()
	if err != nil {
		return err
	}
	g.instanceGroups = clusterFrontEnds
	registered := 0

	for _, frontend := range clusterFrontEnds {
		log.Infof("Registering instance %s with gclb instance group %s", instance.name, frontend.Name)
		attached, err := g.isAlreadyAttached(frontend.Name)
		if err != nil {
			return err
		}
		if attached {
			// We get an error if we try to attach again
			log.Infof("Instance %q is already attached")
		} else {
			additions := &compute.InstanceGroupsAddInstancesRequest{
				Instances: []*compute.InstanceReference{
					{Instance: instance.ID},
				},
			}
			_, err := g.instanceGroupService.AddInstances(g.instance.project, g.instance.zone, frontend.Name, additions).Do()
			if err != nil {
				return fmt.Errorf("unable to register instance %s with gclb %s: %v", instance.name, frontend.Name, err)
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

func (g *gclb) findFrontEndInstanceGroups() ([]*compute.InstanceGroup, error) {
	groups, err := g.instanceGroupService.List(g.instance.project, g.instance.zone).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve the list of instance groups for project %q and zone %q", g.instance.project, g.instance.zone)
	}
	var igs []*compute.InstanceGroup
	for _, group := range groups.Items {
		if strings.HasPrefix(group.Name, g.config.InstanceGroupPrefix) {
			igs = append(igs, group)
		}
	}
	return igs, nil
}

func (g *gclb) isAlreadyAttached(groupName string) (bool, error) {
	instances, err := g.instanceGroupService.ListInstances(
		g.instance.project,
		g.instance.zone,
		groupName,
		&compute.InstanceGroupsListInstancesRequest{InstanceState: "ALL"}).Do()
	if err != nil {
		return false, fmt.Errorf("unable to retrieve instance group: %v", err)
	}

	for _, instanceWithNamedPort := range instances.Items {
		if instanceWithNamedPort.Instance == g.instance.ID {
			return true, nil
		}
	}
	return false, nil
}

func (g *gclb) getInstance() (*instance, error) {
	project, err := metadata.ProjectID()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve project id from metadata: %v", err)
	}
	name, err := metadata.InstanceName()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve instance name from metadata: %v", err)
	}
	zone, err := metadata.Zone()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve instance zone from metadata: %v", err)
	}
	instanceGetResult, err := g.instanceService.Get(project, zone, name).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to query compute service for instance details: %v", err)
	}

	return &instance{
		project: project,
		zone:    zone,
		name:    name,
		ID:      instanceGetResult.SelfLink,
	}, nil
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
