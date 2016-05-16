package ingress

import (
	log "github.com/Sirupsen/logrus"
)

// LoadBalancer that the controller will modify.
type LoadBalancer interface {
	Update([]LoadBalancerEntry) error
}

// LoadBalancerEntry describes the ingress for a single host, path, and service.
type LoadBalancerEntry struct {
	Host        string
	Path        string
	ServiceName string
	ServicePort int32
}

type noopLoadBalancer struct {
}

func (lb *noopLoadBalancer) Update(entries []LoadBalancerEntry) error {
	log.Infof("Updating loadbalancer {}", entries)
	return nil
}

// NewLB creates a new LoadBalancer
func NewLB() LoadBalancer {
	return &noopLoadBalancer{}
}
