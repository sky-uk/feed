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
func (c *FakeClient) WatchIngresses(w k8s.Watcher) error {
	r := c.Called(w)
	return r.Error(0)
}

func (c *FakeClient) String() string {
	return "FakeClient"
}
