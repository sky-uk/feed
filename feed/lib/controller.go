package feed

import (
	"github.com/golang/glog"
)

// Controller for Kubernetes ingress.
type Controller interface {
	Run()
}

// LoadBalancer that the controller will modify.
type LoadBalancer interface {
}

// KubernetesClient for observing changes to the Kubernetes cluster.
type KubernetesClient interface {
}

type impl struct {
	lb     LoadBalancer
	client KubernetesClient
}

// NewController creates a Controller.
func NewController(loadBalancer LoadBalancer, kubernetesClient KubernetesClient) Controller {
	return &impl{
		lb:     loadBalancer,
		client: kubernetesClient,
	}
}

// Run controller.
func (c *impl) Run() {
	glog.Infof("hello %v and %v", c.lb, c.client)
}
