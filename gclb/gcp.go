package gclb

import (
	"cloud.google.com/go/compute/metadata"
	"google.golang.org/api/compute/v1"
)

// GCPInstanceGroupService interface to allow mocking of real calls to GCP as well as restricting the underlying API to
// only the instances we use
type GCPInstanceGroupService interface {
	RemoveInstance(instance *instance, instanceGroup string) error
	AddInstance(instance *instance, instanceGroup string) error
	List(project string, zone string) (*compute.InstanceGroupList, error)
	ListInstances(project string, zone string, instanceGroup string) (*compute.InstanceGroupsListInstances, error)
}

// GCPInstanceService interface to allow mocking of real calls to GCP as well as restricting the underlying API to
// only the instances we use
type GCPInstanceService interface {
	Get(project string, zone string, instance string)  (*compute.Instance, error)
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

func (i *igs) RemoveInstance(instance *instance, instanceGroup string) error {
	removals := []*compute.InstanceReference{{Instance: instance.ID,}}
	_, err := i.delegate.RemoveInstances(
		instance.project,
		instance.zone,
		instanceGroup,
		&compute.InstanceGroupsRemoveInstancesRequest{
			Instances: removals,
		}).Do()
	return err
}

func (i *igs) AddInstance(instance *instance, instanceGroup string) error {
	additions := &compute.InstanceGroupsAddInstancesRequest{
		Instances: []*compute.InstanceReference{
			{Instance: instance.ID},
		},
	}
	_, err := i.delegate.AddInstances(instance.project, instance.zone, instanceGroup, additions).Do()
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

func (i* is) Get(project string, zone string, instance string) (*compute.Instance, error) {
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
