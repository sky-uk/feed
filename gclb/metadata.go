package gclb

import (
	"fmt"
	"regexp"

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
	Region() (string, error)
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

func (m *gcpMetadata) Region() (string, error) {
	zone, err := metadata.Zone()
	if err != nil {
		return "", err
	}

	r := regexp.MustCompile(`(.+)-.+`)
	matches := r.FindStringSubmatch(zone)
	if len(matches) == 2 {
		return matches[1], nil
	}
	return "", fmt.Errorf("error: could not parse region from zone %s", zone)
}
