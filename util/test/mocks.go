package test

import (
	"github.com/sky-uk/feed/k8s"
	"github.com/stretchr/testify/mock"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
)

// FakeClient mocks out the Kubernetes client
type FakeClient struct {
	mock.Mock
}

// GetIngresses mocks out calls to GetIngresses
func (c *FakeClient) GetIngresses() ([]*v1beta1.Ingress, error) {
	r := c.Called()
	return r.Get(0).([]*v1beta1.Ingress), r.Error(1)
}

// WatchIngresses mocks out calls to WatchIngresses
func (c *FakeClient) WatchIngresses() k8s.Watcher {
	r := c.Called()
	return r.Get(0).(k8s.Watcher)
}

// GetEndpoints mocks out calls to GetEndpoints
func (c *FakeClient) GetEndpoints() ([]*v1.Endpoints, error) {
	r := c.Called()
	return r.Get(0).([]*v1.Endpoints), r.Error(1)
}

// WatchEndpoints mocks out calls to WatchEndpoints
func (c *FakeClient) WatchEndpoints() k8s.Watcher {
	r := c.Called()
	return r.Get(0).(k8s.Watcher)
}

func (c *FakeClient) String() string {
	return "FakeClient"
}
