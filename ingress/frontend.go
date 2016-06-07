package ingress

// Frontend controls an external load balancer that serves traffic to the local proxy
// controlled by the ingress controller. The frontend load balances across multiple
// replicas of the controller and proxy.
// For instance, an AWS ELB that load balances traffic to several nginx proxies running
// inside a Kubernetes cluster.
type Frontend interface {
	// Attach should register the local proxy with the frontend.
	// It should block until attaching is complete.
	// Returns the number of load balancers attached, which depends on the frontend implementation.
	Attach() (int, error)
	// Detach should de-register the local proxy from the frontend.
	// It should block until detaching is complete.
	Detach() error
}
