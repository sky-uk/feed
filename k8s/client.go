/*
Package k8s implements a client for communicating with a Kubernetes apiserver. It is intended
to support an ingress controller, so it is limited to the types needed.

The types are copied from the stable api of the Kubernetes 1.3 release.
*/
package k8s

import (
	log "github.com/Sirupsen/logrus"
)

// Client for connecting to a Kubernetes cluster.
type Client interface {
	GetIngresses() ([]Ingress, error)
	WatchIngresses(Watcher) error
}

type noopClient struct {
}

// NewClient creates a new K8 Client
func NewClient() Client {
	return &noopClient{}
}

func (client *noopClient) GetIngresses() ([]Ingress, error) {
	return []Ingress{}, nil
}

func (client *noopClient) WatchIngresses(w Watcher) error {
	log.Infof("Watching ingresses {}", w)

	go func() {
		for i := 0; i < 10; i++ {
			log.Infof("Sending fake update")
			w.Updates() <- Ingress{}
		}
	}()

	return nil
}
