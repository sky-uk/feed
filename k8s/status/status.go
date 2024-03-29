package status

import (
	"fmt"
	"net"
	"sort"

	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/k8s"
	v1 "k8s.io/api/core/v1"
)

// GenerateLoadBalancerStatus to convert a slice of strings to ingress loadbalancer objects.
// Allows hostnames or ip addresses and sets the appropriate field.
func GenerateLoadBalancerStatus(endpoints []string) v1.LoadBalancerStatus {
	lbs := v1.LoadBalancerStatus{}
	for _, ep := range endpoints {
		if net.ParseIP(ep) != nil {
			lbs.Ingress = append(lbs.Ingress, v1.LoadBalancerIngress{IP: ep})
		} else {
			lbs.Ingress = append(lbs.Ingress, v1.LoadBalancerIngress{Hostname: ep})
		}
	}

	return lbs
}

// Update ingresses with current status where unchanged statuses are ignored.
func Update(ingresses controller.IngressEntries, lbs map[string]v1.LoadBalancerStatus, k8sClient k8s.Client) error {
	var updateErrors []error
	for _, ingress := range ingresses {
		if lb, ok := lbs[ingress.LbScheme]; ok {
			if statusUnchanged(ingress.Ingress.Status.LoadBalancer.Ingress, lb.Ingress) {
				continue
			}
			ingress.Ingress.Status.LoadBalancer.Ingress = lb.Ingress

			if err := k8sClient.UpdateIngressStatus(ingress.Ingress); err != nil {
				updateErrors = append(updateErrors, err)
			}
		}
	}

	if totalErrors := len(updateErrors); totalErrors > 0 {
		return fmt.Errorf("failed to update %d ingresses: %v", totalErrors, updateErrors)
	}
	return nil
}

func statusUnchanged(existing, new []v1.LoadBalancerIngress) bool {
	if len(existing) != len(new) {
		return false
	}

	sortLoadBalancerStatus(existing)
	sortLoadBalancerStatus(new)
	for i, loadbalancer := range existing {
		if loadbalancer.IP != new[i].IP {
			return false
		}
		if loadbalancer.Hostname != new[i].Hostname {
			return false
		}
		if len(loadbalancer.Ports) != len(new[i].Ports) {
			return false
		}
		for j := range loadbalancer.Ports {
			if loadbalancer.Ports[j] != new[i].Ports[j] {
				return false
			}
		}
	}

	return true
}

func sortLoadBalancerStatus(lbi []v1.LoadBalancerIngress) {
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
