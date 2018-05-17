package util

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/pkg/api/v1"
)

const (
	defaultHostname  = "test.cosmic.sky"
	defaultIPAddress = "127.0.0.1"
)

func createIngressStatus(hostname, ip string) []v1.LoadBalancerIngress {
	return []v1.LoadBalancerIngress{{
		Hostname: hostname,
		IP:       ip,
	}}
}

func TestStatusUnchanged(t *testing.T) {
	assert := assert.New(t)

	var tests = []struct {
		description           string
		existingIngressStatus []v1.LoadBalancerIngress
		newIngressStatus      []v1.LoadBalancerIngress
		expected              bool
	}{
		{
			description:           "identical ingress statuses",
			existingIngressStatus: createIngressStatus(defaultHostname, defaultIPAddress),
			newIngressStatus:      createIngressStatus(defaultHostname, defaultIPAddress),
			expected:              true,
		},
		{
			description:           "different ingress hostname status",
			existingIngressStatus: createIngressStatus(defaultHostname, defaultIPAddress),
			newIngressStatus:      createIngressStatus("changed.cosmic.sky", defaultIPAddress),
			expected:              false,
		},
		{
			description:           "different ingress ip status",
			existingIngressStatus: createIngressStatus(defaultHostname, defaultIPAddress),
			newIngressStatus:      createIngressStatus(defaultHostname, "0.0.0.0"),
			expected:              false,
		},
		{
			description:           "different number of ingress statuses",
			existingIngressStatus: []v1.LoadBalancerIngress{},
			newIngressStatus:      createIngressStatus(defaultHostname, defaultIPAddress),
			expected:              false,
		},
	}
	for _, test := range tests {
		fmt.Printf("test: %s\n", test.description)
		assert.Equal(test.expected, StatusUnchanged(test.existingIngressStatus, test.newIngressStatus))
	}
}

func TestSliceToStatus(t *testing.T) {
	assert := assert.New(t)

	var tests = []struct {
		description string
		endpoints   []string
		expected    []v1.LoadBalancerIngress
	}{
		{
			description: "single hostname",
			endpoints:   []string{defaultHostname},
			expected: []v1.LoadBalancerIngress{{
				Hostname: defaultHostname,
			}},
		},
		{
			description: "single ip address",
			endpoints:   []string{defaultIPAddress},
			expected: []v1.LoadBalancerIngress{{
				IP: defaultIPAddress,
			}},
		},
		{
			description: "mixture of a hostname and ip address",
			endpoints:   []string{defaultHostname, defaultIPAddress},
			expected: []v1.LoadBalancerIngress{
				{
					Hostname: defaultHostname,
				},
				{
					IP: defaultIPAddress,
				},
			},
		},
	}
	for _, test := range tests {
		fmt.Printf("test: %s\n", test.description)
		assert.Equal(test.expected, SliceToStatus(test.endpoints))
	}
}
