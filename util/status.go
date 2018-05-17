package util

import (
	"net"
	"sort"

	"k8s.io/client-go/pkg/api/v1"
)

// StatusUnchanged to check if the new ingress state matches the existing state.
func StatusUnchanged(existing, new []v1.LoadBalancerIngress) bool {
	if len(existing) != len(new) {
		return false
	}
	for i, loadbalancer := range existing {
		if loadbalancer != new[i] {
			return false
		}
	}
	return true
}

// SliceToStatus to convert a slice of strings to ingress loadbalancer objects.
// Allows hostnames or ip addresses and sets the appropriate field.
func SliceToStatus(endpoints []string) []v1.LoadBalancerIngress {
	lbi := []v1.LoadBalancerIngress{}
	for _, ep := range endpoints {
		if net.ParseIP(ep) == nil {
			lbi = append(lbi, v1.LoadBalancerIngress{Hostname: ep})
		} else {
			lbi = append(lbi, v1.LoadBalancerIngress{IP: ep})
		}
	}

	sort.SliceStable(lbi, func(a, b int) bool {
		return lbi[a].Hostname < lbi[b].Hostname
	})

	sort.SliceStable(lbi, func(a, b int) bool {
		return lbi[a].IP < lbi[b].IP
	})

	return lbi
}
