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
		targetPools:    computeService.TargetPools,
		instances:      computeService.Instances,
		metadata:       NewMetadata(),
	}, err
}

// GLBClient represents a group of trimmed down services from GCP
type GLBClient interface {
	GetSelfMetadata() (*Instance, error)
	FindInstanceGroups(project, zone, instanceGroupPrefix string) ([]*compute.InstanceGroup, error)
	FindTargetPools(project, region, targetPoolPrefix string) ([]*compute.TargetPool, error)
	IsInstanceInInstanceGroup(instance *Instance, instanceGroupName string) (bool, error)
	IsInstanceInTargetPool(instance *Instance, targetPoolName string) (bool, error)
	AddInstanceToInstanceGroup(instance *Instance, instanceGroup string) error
	AddInstanceToTargetPool(instance *Instance, targetPool string) error
	RemoveInstanceFromInstanceGroup(instance *Instance, instanceGroup string) error
	RemoveInstanceFromTargetPool(instance *Instance, targetPool string) error
}

type lbClient struct {
	instanceGroups *compute.InstanceGroupsService
	instances      *compute.InstancesService
	targetPools    *compute.TargetPoolsService
	metadata       GCPMetadata
}

// GetSelfMetadata returns the metadata for the host running this process.
func (c *lbClient) GetSelfMetadata() (*Instance, error) {
	project, err := c.metadata.ProjectID()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve project id from metadata: %v", err)
	}
	name, err := c.metadata.InstanceName()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve instance name from metadata: %v", err)
	}
	zone, err := c.metadata.Zone()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve instance zone from metadata: %v", err)
	}
	region, err := c.metadata.Region()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve instance region from metadata: %v", err)
	}
	instanceGetResult, err := c.getInstance(project, zone, name)
	if err != nil {
		return nil, fmt.Errorf("unable to query delegate service for instance details: %v", err)
	}

	return &Instance{
		Project: project,
		Zone:    zone,
		Name:    name,
		ID:      instanceGetResult.SelfLink,
		Region:  region,
	}, nil
}

// FindInstanceGroups retrieves the instance groups in the given Project and zone which names start
// with the instanceGroupPrefix
func (c *lbClient) FindInstanceGroups(project, zone, instanceGroupPrefix string) ([]*compute.InstanceGroup, error) {
	var page string
	var igs []*compute.InstanceGroup
	for {
		groups, err := c.listInstanceGroups(project, zone, page)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve the list of instance groups for project %q and zone %q", project, zone)
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

func (c *lbClient) FindTargetPools(project, region, targetPoolPrefix string) ([]*compute.TargetPool, error) {
	var page string
	var targetPools []*compute.TargetPool
	for {
		pools, err := c.listTargetPools(project, region, page)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve the list of target pools for project %q and zone %q", project, region)
		}
		for _, pool := range pools.Items {
			if strings.HasPrefix(pool.Name, targetPoolPrefix) {
				targetPools = append(targetPools, pool)
			}
		}
		if page = pools.NextPageToken; page == "" {
			break
		}
	}
	return targetPools, nil
}

func (c *lbClient) IsInstanceInInstanceGroup(instance *Instance, instanceGroupName string) (bool, error) {
	var page string
	for {
		instances, err := c.listInstancesInInstanceGroup(instance.Project, instance.Zone, instanceGroupName, page)
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

func (c *lbClient) IsInstanceInTargetPool(instance *Instance, targetPoolName string) (bool, error) {
	var page string
	instances, err := c.listInstancesInTargetPool(instance.Project, instance.Region, targetPoolName, page)
	if err != nil {
		return false, fmt.Errorf("unable to retrieve target pool: %v", err)
	}

	for _, instanceResourceURL := range instances {
		if instanceResourceURL == instance.ID {
			return true, nil
		}
	}
	return false, nil
}

func (c *lbClient) RemoveInstanceFromInstanceGroup(instance *Instance, instanceGroup string) error {
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

func (c *lbClient) RemoveInstanceFromTargetPool(instance *Instance, targetPool string) error {
	removals := []*compute.InstanceReference{{Instance: instance.ID}}
	_, err := c.targetPools.RemoveInstance(
		instance.Project,
		instance.Region,
		targetPool,
		&compute.TargetPoolsRemoveInstanceRequest{
			Instances: removals,
		}).Do()
	return err
}

func (c *lbClient) AddInstanceToInstanceGroup(instance *Instance, instanceGroup string) error {
	additions := &compute.InstanceGroupsAddInstancesRequest{
		Instances: []*compute.InstanceReference{
			{Instance: instance.ID},
		},
	}
	_, err := c.instanceGroups.AddInstances(instance.Project, instance.Zone, instanceGroup, additions).Do()
	return err
}

func (c *lbClient) AddInstanceToTargetPool(instance *Instance, targetPool string) error {
	additions := &compute.TargetPoolsAddInstanceRequest{
		Instances: []*compute.InstanceReference{
			{Instance: instance.ID},
		},
	}
	_, err := c.targetPools.AddInstance(instance.Project, instance.Region, targetPool, additions).Do()
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

func (c *lbClient) listTargetPools(project, region, pageToken string) (*compute.TargetPoolList, error) {
	request := c.targetPools.List(project, region)
	if pageToken != "" {
		request.PageToken(pageToken)
	}
	return request.Do()
}

func (c *lbClient) listInstancesInInstanceGroup(project, zone, instanceGroup, pageToken string) (*compute.InstanceGroupsListInstances, error) {
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

func (c *lbClient) listInstancesInTargetPool(project, region, targetPool, pageToken string) ([]string, error) {
	request := c.targetPools.Get(project, region, targetPool)
	result, err := request.Do()
	if err != nil {
		return nil, fmt.Errorf("could not find target pool %s: %v", targetPool, err)
	}
	return result.Instances, nil
}
