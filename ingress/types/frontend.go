package types

// Frontend allows updating an external load balancer or proxy such as Nginx or an ELB with the
// host and port the ingress controller is listening on
type Frontend interface {
	// Attach should register this node with the external load balancer
	// It should block until attaching is complete
	// Returns the number of front ends attached
	Attach(frontend FrontendInput) (int, error)
	// Detach should de-register the external load balancer
	// It should block until detaching is complete
	Detach(frontend FrontendInput) error
}

// FrontendInput contains the information for attaching to a Frontend
type FrontendInput struct {
	Cluster string
}
