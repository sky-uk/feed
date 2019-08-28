package status

import (
	"errors"
	"fmt"
	"testing"

	"github.com/sky-uk/feed/controller"
	fake "github.com/sky-uk/feed/util/test"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
)

const (
	defaultHostname    = "test.cosmic.sky"
	defaultIPAddress   = "127.0.0.1"
	defaultLBLabel     = "internal"
	defaultIngressName = "test"
)

func createDefaultLoadBalancerStatus() v1.LoadBalancerStatus {
	return createLoadBalancerStatus(defaultHostname, defaultIPAddress)
}

func createLoadBalancerStatus(hostname, ip string) v1.LoadBalancerStatus {
	return v1.LoadBalancerStatus{
		Ingress: []v1.LoadBalancerIngress{{
			Hostname: hostname,
			IP:       ip,
		}},
	}
}

func createDefaultLBs() map[string]v1.LoadBalancerStatus {
	return createLBs(defaultLBLabel, createDefaultLoadBalancerStatus())
}

func createLBs(lbLabel string, lbStatus v1.LoadBalancerStatus) map[string]v1.LoadBalancerStatus {
	return map[string]v1.LoadBalancerStatus{
		lbLabel: lbStatus,
	}
}

func createDefaultIngresses() controller.IngressEntries {
	return createIngresses(defaultIngressName, defaultLBLabel, createDefaultLoadBalancerStatus())
}

func createIngresses(name, lbScheme string, lbStatus v1.LoadBalancerStatus) controller.IngressEntries {
	return controller.IngressEntries{
		{
			Name:     name,
			LbScheme: lbScheme,
			Ingress: &v1beta1.Ingress{
				Status: v1beta1.IngressStatus{
					LoadBalancer: lbStatus,
				},
			},
		},
	}
}

func TestGenerateLoadBalancerStatus(t *testing.T) {
	assert := assert.New(t)

	var tests = []struct {
		description string
		endpoints   []string
		expected    v1.LoadBalancerStatus
	}{
		{
			description: "single hostname",
			endpoints:   []string{defaultHostname},
			expected: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{{
					Hostname: defaultHostname,
				}},
			},
		},
		{
			description: "single ip address",
			endpoints:   []string{defaultIPAddress},
			expected: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{{
					IP: defaultIPAddress,
				}},
			},
		},
		{
			description: "mixture of a hostname and ip address",
			endpoints:   []string{defaultHostname, defaultIPAddress},
			expected: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{
					{Hostname: defaultHostname},
					{IP: defaultIPAddress},
				},
			},
		},
	}
	for _, test := range tests {
		fmt.Printf("test: %s\n", test.description)
		assert.Equal(test.expected, GenerateLoadBalancerStatus(test.endpoints))
	}
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
			existingIngressStatus: createDefaultLoadBalancerStatus().Ingress,
			newIngressStatus:      createDefaultLoadBalancerStatus().Ingress,
			expected:              true,
		},
		{
			description:           "different ingress hostname status",
			existingIngressStatus: createDefaultLoadBalancerStatus().Ingress,
			newIngressStatus:      createLoadBalancerStatus("changed.cosmic.sky", defaultIPAddress).Ingress,
			expected:              false,
		},
		{
			description:           "different ingress ip status",
			existingIngressStatus: createDefaultLoadBalancerStatus().Ingress,
			newIngressStatus:      createLoadBalancerStatus(defaultHostname, "0.0.0.0").Ingress,
			expected:              false,
		},
		{
			description:           "different number of ingress statuses",
			existingIngressStatus: []v1.LoadBalancerIngress{},
			newIngressStatus:      createDefaultLoadBalancerStatus().Ingress,
			expected:              false,
		},
	}
	for _, test := range tests {
		fmt.Printf("test: %s\n", test.description)
		assert.Equal(test.expected, statusUnchanged(test.existingIngressStatus, test.newIngressStatus))
	}
}

func TestSortLoadBalancerStatus(t *testing.T) {
	assert := assert.New(t)

	var tests = []struct {
		description string
		lbi         []v1.LoadBalancerIngress
		expected    []v1.LoadBalancerIngress
	}{
		{
			description: "reorder hostname",
			lbi: []v1.LoadBalancerIngress{
				{Hostname: "b-" + defaultHostname},
				{Hostname: "a-" + defaultHostname},
			},
			expected: []v1.LoadBalancerIngress{
				{Hostname: "a-" + defaultHostname},
				{Hostname: "b-" + defaultHostname},
			},
		},
		{
			description: "reorder ip addresses",
			lbi: []v1.LoadBalancerIngress{
				{IP: "127.0.0.2"},
				{IP: defaultIPAddress},
			},
			expected: []v1.LoadBalancerIngress{
				{IP: defaultIPAddress},
				{IP: "127.0.0.2"},
			},
		},
		{
			description: "reorder hostnames and ip addresses",
			lbi: []v1.LoadBalancerIngress{
				{IP: "127.0.0.2"},
				{Hostname: "b-" + defaultHostname},
				{IP: defaultIPAddress},
				{Hostname: "a-" + defaultHostname},
			},
			expected: []v1.LoadBalancerIngress{
				{Hostname: "a-" + defaultHostname},
				{Hostname: "b-" + defaultHostname},
				{IP: defaultIPAddress},
				{IP: "127.0.0.2"},
			},
		},
	}
	for _, test := range tests {
		fmt.Printf("test: %s\n", test.description)
		sortLoadBalancerStatus(test.lbi)
		assert.Equal(test.expected, test.lbi)
	}
}

func TestUpdate(t *testing.T) {
	assert := assert.New(t)

	lbs := createDefaultLBs()
	ingresses := createIngresses(defaultIngressName, defaultLBLabel, v1.LoadBalancerStatus{})

	client := new(fake.FakeClient)
	client.On("UpdateIngressStatus").Return(nil)

	err := Update(ingresses, lbs, client)

	assert.NoError(err)
}

func TestUpdateFails(t *testing.T) {
	assert := assert.New(t)

	lbs := createDefaultLBs()
	ingresses := createIngresses(defaultIngressName, defaultLBLabel, v1.LoadBalancerStatus{})

	client := new(fake.FakeClient)
	client.On("UpdateIngressStatus").Return(errors.New("failed"))

	err := Update(ingresses, lbs, client)

	assert.Error(err)
}

func TestUpdateDoesNotRunWithNoChange(t *testing.T) {
	assert := assert.New(t)

	lbs := createDefaultLBs()
	ingresses := createDefaultIngresses()

	client := new(fake.FakeClient)
	client.On("UpdateIngressStatus").Return(errors.New("failed"))

	assert.NoError(Update(ingresses, lbs, client))
}
