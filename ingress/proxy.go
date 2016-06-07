package ingress

import "github.com/sky-uk/feed/controller"

// Proxy proxies traffic from external clients to the internal services in Kubernetes.
type Proxy interface {
	// Start the proxy, returning immediately after it's started.
	Start() error
	// Stop the proxy. Blocks until the proxy stops or an error occurs.
	Stop() error
	// Update the proxy configuration. Returns true if the LB was required to reload
	// its configuration.
	Update(controller.IngressUpdate) (bool, error)
	// Health returns nil if healthy, otherwise an error.
	Health() error
}
