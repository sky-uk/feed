package controller

import (
	"fmt"
)

// IngressUpdate data
type IngressUpdate struct {
	Entries []IngressEntry
}

// Service represents a collection of backends for serving traffic of a particular service.
type Service struct {
	// Name of the service.
	Name string
	// Port to access the service on.
	Port int32
	// Addresses to access the service at.
	Addresses []string
}

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
	// Service to serve traffic.
	Service Service
	// Allow are the ips or cidrs that are allowed to access the service.
	Allow []string
	// ElbScheme internet-facing or internal will dictate which kind of ELB to attach to
	ELbScheme string
	// StripPaths before forwarding to the backend
	StripPaths bool
	// BackendKeepAliveSeconds backend timeout
	BackendKeepAliveSeconds int
}

// NamespaceName returns the string "Namespace/Name".
func (e *IngressEntry) NamespaceName() string {
	return fmt.Sprintf("%s/%s", e.Namespace, e.Name)
}

// String representation of an IngressEntry.
func (e IngressEntry) String() string {
	return fmt.Sprintf("%s/%s: %s%s backends:%+v allow:%v lbScheme:%s stripPaths:%v BackendKeepAlive:%d",
		e.Namespace, e.Name, e.Host, e.Path, e.Service, e.Allow, e.ELbScheme, e.StripPaths, e.BackendKeepAliveSeconds)
}
