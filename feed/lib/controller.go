package feed

import (
	"github.com/golang/glog"
	"github.com/sky-uk/umc-ingress/feed/k8s"
)

// Controller for Kubernetes ingress.
type Controller interface {
	Run() error
}

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

type impl struct {
	lb     LoadBalancer
	client k8s.Client
}

// NewController creates a Controller.
func NewController(loadBalancer LoadBalancer, kubernetesClient k8s.Client) Controller {
	return &impl{
		lb:     loadBalancer,
		client: kubernetesClient,
	}
}

// Run controller.
func (c *impl) Run() error {
	glog.Infof("Starting controller for %v and %v", c.lb, c.client)

	ingresses, err := c.client.GetIngresses()
	if err != nil {
		return err
	}

	var entries []LoadBalancerEntry
	for _, ingress := range ingresses {
		for _, rule := range ingress.Spec.Rules {
			for _, path := range rule.HTTP.Paths {
				entry := LoadBalancerEntry{
					Host:        rule.Host,
					Path:        path.Path,
					ServiceName: path.Backend.ServiceName,
					ServicePort: path.Backend.ServicePort.IntValue(),
				}

				entries = append(entries, entry)
			}
		}
	}

	glog.Infof("Updating load balancer for %d entries", len(entries))
	err = c.lb.Update(entries)
	return err
}
