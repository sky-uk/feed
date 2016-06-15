package controller

import "sort"

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
	// Path is the url path after the hostname.
	Path string
	// Service is a routable address for the Kubernetes backend service to proxy traffic to.
	ServiceAddress string
	// ServicePort is the port to proxy traffic to.
	ServicePort int32
	// Allow are the ips or cidrs that are allowed to access the service.
	Allow []string
}

// isEmpty returns true if Host, ServiceAddress, or ServicePort are empty.
func (entry IngressEntry) isEmpty() bool {
	if entry.Host == "" {
		return true
	}
	if entry.ServiceAddress == "" {
		return true
	}
	if entry.ServicePort == 0 {
		return true
	}
	return false
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
