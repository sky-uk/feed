package types

// LoadBalancer that the controller will modify.
type LoadBalancer interface {
	// Start the load balancer, returning immediately after it's started.
	Start() error
	// Stop the load balancer. Blocks until the load balancer stops or an error occurs.
	Stop() error
	// Update the loadbalancer configuration. Returns true if the LB was required to reload
	// its configuration.
	Update(LoadBalancerUpdate) (bool, error)
	// Health returns nil if healthy, otherwise an error.
	Health() error
}
