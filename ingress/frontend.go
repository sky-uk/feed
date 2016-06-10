package ingress

import "github.com/sky-uk/feed/controller"

// Frontend controls an external load balancer that serves traffic to the local proxy
// controlled by the ingress controller. The frontend load balances across multiple
// replicas of the controller and proxy.
// For instance, an AWS ELB that load balances traffic to several nginx proxies running
// inside a Kubernetes cluster.
type Frontend controller.Updater
