package gclb

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
)

// newGLBClient creates a GCP client
func newGLBClient() (GLBClient, error) {
	ctx := context.Background()
	client, err := google.DefaultClient(ctx, compute.ComputeScope)
	if err != nil {
		return nil, fmt.Errorf("unable to create http client with delegate scope: %v", err)
	}
	computeService, err := compute.New(client)
	if err != nil {
		return nil, fmt.Errorf("unable to create delegate service client: %v", err)
	}
	return &lbClient{
		instanceGroups: computeService.InstanceGroups,
		instances:      computeService.Instances,
		metadata:       NewMetadata(),
	}, err
}

// GLBClient represents a group of trimmed down services from GCP
type GLBClient interface {
	GetSelfMetadata() (*Instance, error)
	FindFrontEndInstanceGroups(project, zone, instanceGroupPrefix string) ([]*compute.InstanceGroup, error)
	IsInstanceInGroup(instance *Instance, instanceGroupName string) (bool, error)
	AddInstance(instance *Instance, instanceGroup string) error
	RemoveInstance(instance *Instance, instanceGroup string) error
}

type lbClient struct {
	instanceGroups *compute.InstanceGroupsService
	instances      *compute.InstancesService
	metadata       GCPMetadata
}

// GetSelfMetadata returns the metadata for the host running this process.
func (c *lbClient) GetSelfMetadata() (*Instance, error) {
	project, err := c.metadata.ProjectID()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve Project id from metadata: %v", err)
	}
	name, err := c.metadata.InstanceName()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve Instance name from metadata: %v", err)
	}
	zone, err := c.metadata.Zone()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve Instance zone from metadata: %v", err)
	}
	instanceGetResult, err := c.getInstance(project, zone, name)
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

// FindFrontEndInstanceGroups retrieves the instance groups in the given Project and zone which names start
// with the instanceGroupPrefix
func (c *lbClient) FindFrontEndInstanceGroups(project, zone, instanceGroupPrefix string) ([]*compute.InstanceGroup, error) {
	var page string
	var igs []*compute.InstanceGroup
	for {
		groups, err := c.listInstanceGroups(project, zone, page)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve the list of Instance groups for project %q and zone %q", project, zone)
		}
		for _, group := range groups.Items {
			if strings.HasPrefix(group.Name, instanceGroupPrefix) {
				igs = append(igs, group)
			}
		}
		if page = groups.NextPageToken; page == "" {
			break
		}
	}
	return igs, nil
}

func (c *lbClient) IsInstanceInGroup(instance *Instance, instanceGroupName string) (bool, error) {
	var page string
	for {
		instances, err := c.listInstances(instance.Project, instance.Zone, instanceGroupName, page)
		if err != nil {
			return false, fmt.Errorf("unable to retrieve Instance group: %v", err)
		}

		for _, instanceWithNamedPort := range instances.Items {
			if instanceWithNamedPort.Instance == instance.ID {
				return true, nil
			}
		}
		if page = instances.NextPageToken; page == "" {
			break
		}
	}
	return false, nil
}

func (c *lbClient) RemoveInstance(instance *Instance, instanceGroup string) error {
	removals := []*compute.InstanceReference{{Instance: instance.ID}}
	_, err := c.instanceGroups.RemoveInstances(
		instance.Project,
		instance.Zone,
		instanceGroup,
		&compute.InstanceGroupsRemoveInstancesRequest{
			Instances: removals,
		}).Do()
	return err
}

func (c *lbClient) AddInstance(instance *Instance, instanceGroup string) error {
	additions := &compute.InstanceGroupsAddInstancesRequest{
		Instances: []*compute.InstanceReference{
			{Instance: instance.ID},
		},
	}
	_, err := c.instanceGroups.AddInstances(instance.Project, instance.Zone, instanceGroup, additions).Do()
	return err
}

func (c *lbClient) getInstance(project string, zone string, instance string) (*compute.Instance, error) {
	return c.instances.Get(project, zone, instance).Do()
}

func (c *lbClient) listInstanceGroups(project, zone, pageToken string) (*compute.InstanceGroupList, error) {
	request := c.instanceGroups.List(project, zone)
	if pageToken != "" {
		request.PageToken(pageToken)
	}
	return request.Do()
}

func (c *lbClient) listInstances(project, zone, instanceGroup, pageToken string) (*compute.InstanceGroupsListInstances, error) {
	request := c.instanceGroups.ListInstances(
		project,
		zone,
		instanceGroup,
		&compute.InstanceGroupsListInstancesRequest{InstanceState: "ALL"})
	if pageToken != "" {
		request.PageToken(pageToken)
	}
	return request.Do()
}
