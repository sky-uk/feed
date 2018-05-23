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

	sortLoadBalancerIngress(existing)

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
		if net.ParseIP(ep) != nil {
			lbi = append(lbi, v1.LoadBalancerIngress{IP: ep})
		} else {
			lbi = append(lbi, v1.LoadBalancerIngress{Hostname: ep})
		}
	}

	sortLoadBalancerIngress(lbi)

	return lbi
}

func sortLoadBalancerIngress(lbi []v1.LoadBalancerIngress) {
	sort.SliceStable(lbi, func(i, j int) bool {
		if lbi[i].IP < lbi[j].IP {
			return true
		}
		if lbi[i].IP > lbi[j].IP {
			return false
		}
		return lbi[i].Hostname < lbi[j].Hostname
	})
}
