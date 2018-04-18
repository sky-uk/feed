package gclb

import (
	"cloud.google.com/go/compute/metadata"
)

// NewMetadata creates a wrapper for the GC Metadata package.
func NewMetadata() GCPMetadata {
	return &gcpMetadata{}
}

// GCPMetadata interface to allow mocking of real calls to GCP as well as restricting the underlying API to
// only the methods we use.
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
