package ingress

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
