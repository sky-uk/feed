package test

import (
	"github.com/sky-uk/feed/k8s"
	"github.com/stretchr/testify/mock"
)

// FakeClient mocks out the Kubernetes client
type FakeClient struct {
	mock.Mock
}

// GetIngresses mocks out calls to GetIngresses
func (c *FakeClient) GetIngresses() ([]k8s.Ingress, error) {
	r := c.Called()
	return r.Get(0).([]k8s.Ingress), r.Error(1)
}

// WatchIngresses mocks out calls to WatchIngresses
func (c *FakeClient) WatchIngresses() k8s.Watcher {
	r := c.Called()
	return r.Get(0).(k8s.Watcher)
}

// GetServices mocks out calls to GetServices
func (c *FakeClient) GetServices() ([]k8s.Service, error) {
	r := c.Called()
	return r.Get(0).([]k8s.Service), r.Error(1)
}

// WatchServices mocks out calls to WatchServices
func (c *FakeClient) WatchServices() k8s.Watcher {
	r := c.Called()
	return r.Get(0).(k8s.Watcher)
}

func (c *FakeClient) String() string {
	return "FakeClient"
}
