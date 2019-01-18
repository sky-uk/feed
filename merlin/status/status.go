/*
Package status provides an updater for a Merlin frontend to update ingress statuses.
*/
package status

import (
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/k8s"
	k8s_status "github.com/sky-uk/feed/k8s/status"
	v1 "k8s.io/client-go/pkg/api/v1"
)

const (
	internalLabelValue       = "internal"
	internetFacingLabelValue = "internet-facing"
)

// Config for creating a new Merlin status updater.
type Config struct {
	InternalHostname       string
	InternetFacingHostname string
	KubernetesClient       k8s.Client
}

// New creates a new Merlin frontend status updater.
func New(conf Config) (controller.Updater, error) {
	return &status{
		cnames: map[string]string{
			internalLabelValue:       conf.InternalHostname,
			internetFacingLabelValue: conf.InternetFacingHostname,
		},
		loadBalancers:    make(map[string]v1.LoadBalancerStatus),
		kubernetesClient: conf.KubernetesClient,
	}, nil
}

type status struct {
	cnames           map[string]string
	loadBalancers    map[string]v1.LoadBalancerStatus
	kubernetesClient k8s.Client
}

// Start generates loadBalancer statuses from valid vips.
func (s *status) Start() error {
	for lbLabel, cname := range s.cnames {
		if cname != "" {
			s.loadBalancers[lbLabel] = k8s_status.GenerateLoadBalancerStatus([]string{cname})
		}
	}
	return nil
}

func (s *status) Stop() error {
	return nil
}

func (s *status) Health() error {
	return nil
}

func (s *status) Update(ingresses controller.IngressEntries) error {
	return k8s_status.Update(ingresses, s.loadBalancers, s.kubernetesClient)
}
