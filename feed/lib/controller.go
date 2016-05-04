package feed

import (
	"github.com/golang/glog"
)

// Controller for nginx load balancer used for ingress.
type Controller struct {
}

// NewController creates a Controller.
func NewController() *Controller {
	return &Controller{}
}

// Run controller.
func (c *Controller) Run() {
	glog.Info("hello")
}
