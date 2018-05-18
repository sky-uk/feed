/*
Package status provides an updater for a Merlin frontend to update ingress statuses.
*/
package status

import (
	"sync"

	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/util"
	"k8s.io/client-go/pkg/api/v1"
)

const (
	internalLabelValue = "internal"
	externalLabelValue = "internet-facing"
)

// Config for creating a new ingress controller.
type Config struct {
	InternalHostname string
	ExternalHostname string
	KubernetesClient k8s.Client
}

// New creates a new ELB frontend status updater.
func New(conf Config) (controller.Updater, error) {
	lbs := make(map[string][]v1.LoadBalancerIngress)
	lbs[internalLabelValue] = util.SliceToStatus([]string{conf.InternalHostname})
	lbs[externalLabelValue] = util.SliceToStatus([]string{conf.ExternalHostname})
	return &status{
		lbs:              lbs,
		kubernetesClient: conf.KubernetesClient,
	}, nil
}

type status struct {
	lbs              map[string][]v1.LoadBalancerIngress
	kubernetesClient k8s.Client
	initialised      sync.Mutex
}

func (s *status) Start() error {
	return nil
}

// Stop removes this instance from all the front end ELBs
func (s *status) Stop() error {
	return nil
}

func (s *status) Health() error {
	return nil
}

func (s *status) Update(ingresses controller.IngressEntries) error {
	s.initialised.Lock()
	defer s.initialised.Unlock()

	for _, ingress := range ingresses {
		if lb, ok := s.lbs[ingress.ELbScheme]; ok {
			if util.StatusUnchanged(ingress.Ingress.Status.LoadBalancer.Ingress, lb) {
				continue
			}

			ingress.Ingress.Status.LoadBalancer.Ingress = lb

			if err := s.kubernetesClient.UpdateIngressStatus(ingress.Ingress); err != nil {
				return err
			}
		}
	}

	return nil
}
