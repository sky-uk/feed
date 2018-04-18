package gclb

import (
	"fmt"
	"strings"
	"context"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
	"cloud.google.com/go/compute/metadata"
)

// NewClient creates a GCP client
func NewClient() (*Client, error) {
	ctx := context.Background()
	client, err := google.DefaultClient(ctx, compute.ComputeScope)
	if err != nil {
		return nil, fmt.Errorf("unable to create http client with delegate scope: %v", err)
	}
	computeService, err := compute.New(client)
	if err != nil {
		return nil, fmt.Errorf("unable to create delegate service client: %v", err)
	}
	return &Client{
		InstanceGroupService: &igs{delegate: computeService.InstanceGroups},
		InstanceService:      &is{delegate: computeService.Instances},
		MetadataService:      &gcpMetadata{},
	}, err
}

// Client represents a group of trimmed down services from GCP
type Client struct {
	InstanceGroupService GCPInstanceGroupService
	InstanceService      GCPInstanceService
	MetadataService      GCPMetadata
}

// FindFrontEndInstanceGroups retrieves the instance groups in the given Project and zone which names start
// with the instanceGroupPrefix
func (c *Client) FindFrontEndInstanceGroups(project, zone, instanceGroupPrefix string) ([]*compute.InstanceGroup, error) {
	groups, err := c.InstanceGroupService.List(project, zone)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve the list of Instance groups for project %q and zone %q", project, zone)
	}
	var igs []*compute.InstanceGroup
	for _, group := range groups.Items {
		if strings.HasPrefix(group.Name, instanceGroupPrefix) {
			igs = append(igs, group)
		}
	}
	return igs, nil
}

func (c *Client) isAlreadyAttached(instance *Instance, instanceGroupName string) (bool, error) {
	instances, err := c.InstanceGroupService.ListInstances(instance.Project, instance.Zone, instanceGroupName)
	if err != nil {
		return false, fmt.Errorf("unable to retrieve Instance group: %v", err)
	}

	for _, instanceWithNamedPort := range instances.Items {
		if instanceWithNamedPort.Instance == instance.ID {
			return true, nil
		}
	}
	return false, nil
}

// GetSelfMetadata returns the metadata for the host running this process.
func (c *Client) GetSelfMetadata() (*Instance, error) {
	project, err := metadata.ProjectID()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve Project id from metadata: %v", err)
	}
	name, err := metadata.InstanceName()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve Instance name from metadata: %v", err)
	}
	zone, err := metadata.Zone()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve Instance zone from metadata: %v", err)
	}
	instanceGetResult, err := c.InstanceService.Get(project, zone, name)
	if err != nil {
		return nil, fmt.Errorf("unable to query delegate service for Instance details: %v", err)
	}

	return &Instance{
		Project: project,
		Zone:    zone,
		Name:    name,
		ID:      instanceGetResult.SelfLink,
	}, nil
}

// GCPInstanceGroupService interface to allow mocking of real calls to GCP as well as restricting the underlying API to
// only the instances we use
type GCPInstanceGroupService interface {
	RemoveInstance(instance *Instance, instanceGroup string) error
	AddInstance(instance *Instance, instanceGroup string) error
	List(project string, zone string) (*compute.InstanceGroupList, error)
	ListInstances(project string, zone string, instanceGroup string) (*compute.InstanceGroupsListInstances, error)
}

// GCPInstanceService interface to allow mocking of real calls to GCP as well as restricting the underlying API to
// only the instances we use
type GCPInstanceService interface {
	Get(project string, zone string, instance string) (*compute.Instance, error)
}

// GCPMetadata interface to allow mocking of real calls to GCP as well as restricting the underlying API to
// only the instances we use
type GCPMetadata interface {
	ProjectID() (string, error)
	InstanceName() (string, error)
	Zone() (string, error)
}

type igs struct {
	delegate *compute.InstanceGroupsService
}

func (i *igs) RemoveInstance(instance *Instance, instanceGroup string) error {
	removals := []*compute.InstanceReference{{Instance: instance.ID,}}
	_, err := i.delegate.RemoveInstances(
		instance.Project,
		instance.Zone,
		instanceGroup,
		&compute.InstanceGroupsRemoveInstancesRequest{
			Instances: removals,
		}).Do()
	return err
}

func (i *igs) AddInstance(instance *Instance, instanceGroup string) error {
	additions := &compute.InstanceGroupsAddInstancesRequest{
		Instances: []*compute.InstanceReference{
			{Instance: instance.ID},
		},
	}
	_, err := i.delegate.AddInstances(instance.Project, instance.Zone, instanceGroup, additions).Do()
	return err
}

func (i *igs) List(project string, zone string) (*compute.InstanceGroupList, error) {
	return i.delegate.List(project, zone).Do()
}

func (i *igs) ListInstances(project string, zone string, instanceGroup string) (*compute.InstanceGroupsListInstances, error) {
	return i.delegate.ListInstances(
		project,
		zone,
		instanceGroup,
		&compute.InstanceGroupsListInstancesRequest{InstanceState: "ALL"}).Do()
}

type is struct {
	delegate *compute.InstancesService
}

func (i *is) Get(project string, zone string, instance string) (*compute.Instance, error) {
	return i.delegate.Get(project, zone, instance).Do()
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
