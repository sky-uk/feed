package ingress

import (
	log "github.com/Sirupsen/logrus"
)

// LoadBalancerUpdate data required to update loadbalancing configuration
type LoadBalancerUpdate struct {
	Entries []LoadBalancerEntry
}

// LoadBalancerEntry describes the ingress for a single host, path, and service.
type LoadBalancerEntry struct {
	Host        string
	Path        string
	ServiceName string
	ServicePort int32
}

// FilterInvalidEntries returns a slice of all the valid LoadBalancer entries
func filterInvalidEntries(entries []LoadBalancerEntry) []LoadBalancerEntry {
	var validEntries []LoadBalancerEntry

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
func (entry LoadBalancerEntry) ValidateEntry() bool {
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
