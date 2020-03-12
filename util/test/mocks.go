package test

import (
	"github.com/sky-uk/feed/k8s"
	"github.com/stretchr/testify/mock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
)

// FakeClient mocks out the Kubernetes client
type FakeClient struct {
	mock.Mock
}

// GetAllIngresses mocks out calls to GetAllIngresses
func (c *FakeClient) GetAllIngresses() ([]*v1beta1.Ingress, error) {
	r := c.Called()
	return r.Get(0).([]*v1beta1.Ingress), r.Error(1)
}

// GetIngresses mocks out calls to GetIngresses
func (c *FakeClient) GetIngresses(selector *k8s.NamespaceSelector) ([]*v1beta1.Ingress, error) {
	r := c.Called(selector)
	return r.Get(0).([]*v1beta1.Ingress), r.Error(1)
}

// WatchIngresses mocks out calls to WatchIngresses
func (c *FakeClient) WatchIngresses() (k8s.Watcher, error) {
	r := c.Called()
	return r.Get(0).(k8s.Watcher), r.Error(1)
}

// GetServices mocks out calls to GetServices
func (c *FakeClient) GetServices() ([]*v1.Service, error) {
	r := c.Called()
	return r.Get(0).([]*v1.Service), r.Error(1)
}

// WatchServices mocks out calls to WatchServices
func (c *FakeClient) WatchServices() (k8s.Watcher, error) {
	r := c.Called()
	return r.Get(0).(k8s.Watcher), r.Error(1)
}

// WatchNamespaces mocks out calls to WatchNamespaces
func (c *FakeClient) WatchNamespaces() (k8s.Watcher, error) {
	r := c.Called()
	return r.Get(0).(k8s.Watcher), r.Error(1)
}

// UpdateIngressStatus mocks out calls to UpdateIngressStatus
func (c *FakeClient) UpdateIngressStatus(*v1beta1.Ingress) error {
	r := c.Called()
	return r.Error(0)
}

func (c *FakeClient) String() string {
	return "FakeClient"
}
