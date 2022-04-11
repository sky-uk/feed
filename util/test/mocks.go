package test

import (
	"github.com/sky-uk/feed/k8s"
	"github.com/stretchr/testify/mock"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

// FakeClient mocks out the Kubernetes client
type FakeClient struct {
	mock.Mock
}

// GetAllIngresses mocks out calls to GetAllIngresses
func (c *FakeClient) GetAllIngresses() ([]*networkingv1.Ingress, error) {
	r := c.Called()
	return r.Get(0).([]*networkingv1.Ingress), r.Error(1)
}

// GetIngresses mocks out calls to GetIngresses
func (c *FakeClient) GetIngresses(selectors []*k8s.NamespaceSelector, matchAllSelectors bool) ([]*networkingv1.Ingress, error) {
	r := c.Called(selectors, matchAllSelectors)
	return r.Get(0).([]*networkingv1.Ingress), r.Error(1)
}

// WatchIngresses mocks out calls to WatchIngresses
func (c *FakeClient) WatchIngresses() k8s.Watcher {
	r := c.Called()
	return r.Get(0).(k8s.Watcher)
}

// GetServices mocks out calls to GetServices
func (c *FakeClient) GetServices() ([]*v1.Service, error) {
	r := c.Called()
	return r.Get(0).([]*v1.Service), r.Error(1)
}

// WatchServices mocks out calls to WatchServices
func (c *FakeClient) WatchServices() k8s.Watcher {
	r := c.Called()
	return r.Get(0).(k8s.Watcher)
}

// WatchNamespaces mocks out calls to WatchNamespaces
func (c *FakeClient) WatchNamespaces() k8s.Watcher {
	r := c.Called()
	return r.Get(0).(k8s.Watcher)
}

// UpdateIngressStatus mocks out calls to UpdateIngressStatus
func (c *FakeClient) UpdateIngressStatus(*networkingv1.Ingress) error {
	r := c.Called()
	return r.Error(0)
}

func (c *FakeClient) String() string {
	return "FakeClient"
}
