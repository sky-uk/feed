/*
Package status provides an updater for a Merlin frontend to update ingress statuses.
*/
package status

import (
	"errors"

	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/util"
	"k8s.io/client-go/pkg/api/v1"

	log "github.com/sirupsen/logrus"
)

const (
	internalLabelValue = "internal"
	externalLabelValue = "internet-facing"
)

// Config for creating a new Merlin status updater.
type Config struct {
	InternalHostname string
	ExternalHostname string
	KubernetesClient k8s.Client
}

// New creates a new Merlin frontend status updater.
func New(conf Config) (controller.Updater, error) {
	loadBalancers := make(map[string][]v1.LoadBalancerIngress)
	loadBalancers[internalLabelValue] = util.GenerateLoadBalancerStatus([]string{conf.InternalHostname})
	loadBalancers[externalLabelValue] = util.GenerateLoadBalancerStatus([]string{conf.ExternalHostname})

	return &status{
		loadBalancers:    loadBalancers,
		kubernetesClient: conf.KubernetesClient,
	}, nil
}

type status struct {
	loadBalancers    map[string][]v1.LoadBalancerIngress
	kubernetesClient k8s.Client
}

func (s *status) Start() error {
	return nil
}

func (s *status) Stop() error {
	return nil
}

func (s *status) Health() error {
	return nil
}

func (s *status) Update(ingresses controller.IngressEntries) error {
	var updateFailed bool
	for _, ingress := range ingresses {
		if lb, ok := s.loadBalancers[ingress.ELbScheme]; ok {
			if util.StatusUnchanged(ingress.Ingress.Status.LoadBalancer.Ingress, lb) {
				continue
			}

			ingress.Ingress.Status.LoadBalancer.Ingress = lb

			if err := s.kubernetesClient.UpdateIngressStatus(ingress.Ingress); err != nil {
				log.Warn("Failed to update ingress status for %s: %e", ingress.Name, err)
				updateFailed = true
			}
		}
	}

	if updateFailed {
		return errors.New("failed to update all ingress statuses")
	}
	return nil
}
