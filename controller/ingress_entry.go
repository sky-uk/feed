package controller

import (
	"sort"

	log "github.com/Sirupsen/logrus"
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
	// Path is the url path after the hostname.
	Path string
	// ServiceName is the Kubernetes backend service to proxy traffic to.
	ServiceName string
	// ServicePort is the port to proxy traffic to.
	ServicePort int32
	// SourceRange is the ip or cidr that is allowed to access the service.
	Allow string
}

// FilterInvalidEntries returns a slice of all the valid LoadBalancer entries
func FilterInvalidEntries(entries []IngressEntry) []IngressEntry {
	var validEntries []IngressEntry

	for _, entry := range entries {
		if entry.ValidateEntry() {
			validEntries = append(validEntries, entry)
		} else {
			log.Warnf("Removing invalid load balancer entry for service '%s' host '%s'", entry.ServiceName, entry.Host)
		}
	}

	return validEntries
}

// ValidateEntry returns whether the given entry is valid
func (entry IngressEntry) ValidateEntry() bool {
	if entry.Host == "" {
		return false
	}
	if entry.Path == "" {
		return false
	}
	if entry.ServiceName == "" {
		return false
	}
	if entry.ServicePort == 0 {
		return false
	}
	return true
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
