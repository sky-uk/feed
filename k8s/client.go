/*
Package k8s implements a client for communicating with a Kubernetes apiserver. It is intended
to support an ingress controller, so it is limited to the types needed.

The types are copied from the stable api of the Kubernetes 1.3 release.
 */
package k8s

// Client for connecting to a Kubernetes cluster.
type Client interface {
	GetIngresses() ([]Ingress, error)
	WatchIngresses(Watcher) error
}
