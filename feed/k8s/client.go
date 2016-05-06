package k8s

// Client for connecting to a Kubernetes cluster.
type Client interface {
	GetIngresses() ([]Ingress, error)
}
