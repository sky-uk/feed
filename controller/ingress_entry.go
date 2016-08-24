package controller

import (
	"fmt"
	"sort"
)

// IngressUpdate data
type IngressUpdate struct {
	Entries []IngressEntry
}

// IngressEntry describes the ingress for a single host, path, and service.
type IngressEntry struct {
	// Name of the entry.
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
	// BackendKeepAliveSeconds backend timeout
	BackendKeepAliveSeconds int
}

// validate returns error if entry has invalid fields.
func (entry IngressEntry) validate() error {
	if entry.Host == "" {
		return fmt.Errorf("%s had empty Host", entry.Name)
	}
	if entry.ServiceAddress == "" {
		return fmt.Errorf("%s had empty ServiceAddress", entry.Name)
	}
	if entry.ServicePort == 0 {
		return fmt.Errorf("%s had 0 ServicePort", entry.Name)
	}
	return nil
}

// SortedByName returns the update with entries ordered by their Name.
func (u IngressUpdate) SortedByName() IngressUpdate {
	sortedEntries := make([]IngressEntry, len(u.Entries))
	copy(sortedEntries, u.Entries)
	sort.Sort(byName(sortedEntries))
	return IngressUpdate{Entries: sortedEntries}
}

type byName []IngressEntry

func (a byName) Len() int           { return len(a) }
func (a byName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byName) Less(i, j int) bool { return a[i].Name < a[j].Name }
