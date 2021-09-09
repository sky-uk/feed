package external

import (
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/k8s"
	k8sStatus "github.com/sky-uk/feed/k8s/status"
	v1 "k8s.io/api/core/v1"
)

// Config for External status updater.
type Config struct {
	InternalHostname string
	ExternalHostname string
	KubernetesClient k8s.Client
}
type status struct {
	internalHostname string
	externalHostname string
	loadBalancers    map[string]v1.LoadBalancerStatus
	kubernetesClient k8s.Client
}

// New creates a new External status updater.
func New(conf Config) (controller.Updater, error) {
	return &status{
		internalHostname: conf.InternalHostname,
		externalHostname: conf.ExternalHostname,
		loadBalancers:    make(map[string]v1.LoadBalancerStatus),
		kubernetesClient: conf.KubernetesClient,
	}, nil
}

func (s *status) Start() error {
	s.loadBalancers["internal"] = k8sStatus.GenerateLoadBalancerStatus([]string{s.internalHostname})
	s.loadBalancers["internet-facing"] = k8sStatus.GenerateLoadBalancerStatus([]string{s.externalHostname})
	return nil
}

func (s *status) Stop() error {
	return nil
}

func (s *status) Health() error {
	return nil
}

func (s *status) Update(ingresses controller.IngressEntries) error {
	return k8sStatus.Update(ingresses, s.loadBalancers, s.kubernetesClient)
}
