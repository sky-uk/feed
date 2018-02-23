package controller

import (
	"errors"
	"fmt"
	"time"
)

// IngressEntries type
type IngressEntries []IngressEntry

// IngressEntry describes the ingress for a single host, path, and service.
type IngressEntry struct {
	// Namespace of the ingress.
	Namespace string
	// Name of the ingress.
	Name string
	// Host is the fully qualified domain name used for external access.
	Host string
	// Path is the url path after the hostname. Must be non-empty.
	Path string
	// ServiceAddress is a routable address for the Kubernetes backend service to proxy traffic to.
	// Must be non-empty.
	ServiceAddress string
	// ServicePort is the port to proxy traffic to. Must be non-zero.
	ServicePort int32
	// Allow are the ips or cidrs that are allowed to access the service.
	Allow []string
	// ElbScheme internet-facing or internal will dictate which kind of ELB to attach to
	ELbScheme string
	// StripPaths before forwarding to the backend
	StripPaths bool
	// BackendTimeoutSeconds backend timeout
	BackendTimeoutSeconds int
	// BackendMaxConnections maximum backend connections
	BackendMaxConnections int
	// Ingress creation time
	CreationTimestamp time.Time
}

// validate returns error if entry has invalid fields.
func (e IngressEntry) validate() error {
	if e.Host == "" {
		return errors.New("missing host")
	}
	if e.ServiceAddress == "" {
		return errors.New("missing service address")
	}
	if e.ServicePort == 0 {
		return errors.New("missing service port")
	}
	return nil
}

// NamespaceName returns the string "Namespace/Name".
func (e IngressEntry) NamespaceName() string {
	return fmt.Sprintf("%s/%s", e.Namespace, e.Name)
}
