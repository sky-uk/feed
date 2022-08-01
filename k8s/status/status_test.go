package status

import (
	"errors"
	"fmt"
	"testing"

	"github.com/sky-uk/feed/controller"
	fake "github.com/sky-uk/feed/util/test"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

const (
	defaultHostname    = "test.cosmic.sky"
	defaultIPAddress   = "127.0.0.1"
	defaultLBLabel     = "internal"
	defaultIngressName = "test"
)

func createDefaultLoadBalancerStatus() corev1.LoadBalancerStatus {
	return createLoadBalancerStatus(defaultHostname, defaultIPAddress)
}

func createLoadBalancerStatus(hostname, ip string) corev1.LoadBalancerStatus {
	return corev1.LoadBalancerStatus{
		Ingress: []corev1.LoadBalancerIngress{{
			Hostname: hostname,
			IP:       ip,
		}},
	}
}

func createDefaultLBs() map[string]corev1.LoadBalancerStatus {
	return createLBs(defaultLBLabel, createDefaultLoadBalancerStatus())
}

func createLBs(lbLabel string, lbStatus corev1.LoadBalancerStatus) map[string]corev1.LoadBalancerStatus {
	return map[string]corev1.LoadBalancerStatus{
		lbLabel: lbStatus,
	}
}

func createDefaultIngresses() controller.IngressEntries {
	return createIngresses(defaultIngressName, defaultLBLabel, createDefaultLoadBalancerStatus())
}

func createIngresses(name, lbScheme string, lbStatus corev1.LoadBalancerStatus) controller.IngressEntries {
	return controller.IngressEntries{
		{
			Name:     name,
			LbScheme: lbScheme,
			Ingress: &networkingv1.Ingress{
				Status: networkingv1.IngressStatus{
					LoadBalancer: lbStatus,
				},
			},
		},
	}
}

func TestGenerateLoadBalancerStatus(t *testing.T) {
	asserter := assert.New(t)

	var tests = []struct {
		description string
		endpoints   []string
		expected    corev1.LoadBalancerStatus
	}{
		{
			description: "single hostname",
			endpoints:   []string{defaultHostname},
			expected: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{
					Hostname: defaultHostname,
				}},
			},
		},
		{
			description: "single ip address",
			endpoints:   []string{defaultIPAddress},
			expected: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{
					IP: defaultIPAddress,
				}},
			},
		},
		{
			description: "mixture of a hostname and ip address",
			endpoints:   []string{defaultHostname, defaultIPAddress},
			expected: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{Hostname: defaultHostname},
					{IP: defaultIPAddress},
				},
			},
		},
	}
	for _, test := range tests {
		fmt.Printf("test: %s\n", test.description)
		asserter.Equal(test.expected, GenerateLoadBalancerStatus(test.endpoints))
	}
}

func TestStatusUnchanged(t *testing.T) {
	asserter := assert.New(t)

	var tests = []struct {
		description           string
		existingIngressStatus []corev1.LoadBalancerIngress
		newIngressStatus      []corev1.LoadBalancerIngress
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
			existingIngressStatus: []corev1.LoadBalancerIngress{},
			newIngressStatus:      createDefaultLoadBalancerStatus().Ingress,
			expected:              false,
		},
	}
	for _, test := range tests {
		fmt.Printf("test: %s\n", test.description)
		asserter.Equal(test.expected, statusUnchanged(test.existingIngressStatus, test.newIngressStatus))
	}
}

func TestSortLoadBalancerStatus(t *testing.T) {
	asserter := assert.New(t)

	var tests = []struct {
		description string
		lbi         []corev1.LoadBalancerIngress
		expected    []corev1.LoadBalancerIngress
	}{
		{
			description: "reorder hostname",
			lbi: []corev1.LoadBalancerIngress{
				{Hostname: "b-" + defaultHostname},
				{Hostname: "a-" + defaultHostname},
			},
			expected: []corev1.LoadBalancerIngress{
				{Hostname: "a-" + defaultHostname},
				{Hostname: "b-" + defaultHostname},
			},
		},
		{
			description: "reorder ip addresses",
			lbi: []corev1.LoadBalancerIngress{
				{IP: "127.0.0.2"},
				{IP: defaultIPAddress},
			},
			expected: []corev1.LoadBalancerIngress{
				{IP: defaultIPAddress},
				{IP: "127.0.0.2"},
			},
		},
		{
			description: "reorder hostnames and ip addresses",
			lbi: []corev1.LoadBalancerIngress{
				{IP: "127.0.0.2"},
				{Hostname: "b-" + defaultHostname},
				{IP: defaultIPAddress},
				{Hostname: "a-" + defaultHostname},
			},
			expected: []corev1.LoadBalancerIngress{
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
		asserter.Equal(test.expected, test.lbi)
	}
}

func TestUpdate(t *testing.T) {
	asserter := assert.New(t)

	lbs := createDefaultLBs()
	ingresses := createIngresses(defaultIngressName, defaultLBLabel, corev1.LoadBalancerStatus{})

	client := new(fake.FakeClient)
	client.On("UpdateIngressStatus").Return(nil)

	err := Update(ingresses, lbs, client)

	asserter.NoError(err)
}

func TestUpdateFails(t *testing.T) {
	asserter := assert.New(t)

	lbs := createDefaultLBs()
	ingresses := createIngresses(defaultIngressName, defaultLBLabel, corev1.LoadBalancerStatus{})

	client := new(fake.FakeClient)
	client.On("UpdateIngressStatus").Return(errors.New("failed"))

	err := Update(ingresses, lbs, client)

	asserter.Error(err)
}

func TestUpdateDoesNotRunWithNoChange(t *testing.T) {
	asserter := assert.New(t)

	lbs := createDefaultLBs()
	ingresses := createDefaultIngresses()

	client := new(fake.FakeClient)
	client.On("UpdateIngressStatus").Return(errors.New("failed"))

	asserter.NoError(Update(ingresses, lbs, client))
}
