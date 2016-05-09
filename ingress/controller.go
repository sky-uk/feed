package ingress

import (
	"github.com/golang/glog"
	"github.com/sky-uk/feed/k8s"
)

// Controller for Kubernetes ingress.
type Controller interface {
	Run() error
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
