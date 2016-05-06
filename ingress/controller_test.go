package ingress

import (
	"testing"

	"github.com/sky-uk/feed/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type fakeLb struct {
	mock.Mock
}

func (lb *fakeLb) Update(entries []LoadBalancerEntry) error {
	r := lb.Called(entries)
	return r.Error(0)
}

type fakeClient struct {
	mock.Mock
}

func (c *fakeClient) GetIngresses() ([]k8s.Ingress, error) {
	r := c.Called()
	return r.Get(0).([]k8s.Ingress), r.Error(1)
}

func TestRunIsSuccessful(t *testing.T) {
	// given
	lb := new(fakeLb)
	client := new(fakeClient)
	controller := NewController(lb, client)
	client.On("GetIngresses").Return([]k8s.Ingress{}, nil)
	lb.On("Update", mock.Anything).Return(nil)

	// when
	err := controller.Run()

	// then
	assert.Nil(t, err, "Run should have been successful")
}

func TestLoadBalancerUpdatesWithInitialIngress(t *testing.T) {
	assert := assert.New(t)

	// given
	lb := new(fakeLb)
	client := new(fakeClient)

	paths := []k8s.HTTPIngressPath{k8s.HTTPIngressPath{
		Path: "/foo",
		Backend: k8s.IngressBackend{
			ServiceName: "foo-svc",
			ServicePort: k8s.FromInt(80),
		},
	}}
	ingresses := []k8s.Ingress{
		k8s.Ingress{
			ObjectMeta: k8s.ObjectMeta{Name: "foo-ingress"},
			Spec: k8s.IngressSpec{
				Rules: []k8s.IngressRule{k8s.IngressRule{
					Host: "foo.sky.com",
					IngressRuleValue: k8s.IngressRuleValue{HTTP: &k8s.HTTPIngressRuleValue{
						Paths: paths,
					}},
				}},
			},
		},
	}
	controller := NewController(lb, client)

	client.On("GetIngresses").Return(ingresses, nil)
	lb.On("Update", mock.Anything).Return(nil)

	// when
	err := controller.Run()

	// then
	assert.Nil(err)

	entries := []LoadBalancerEntry{LoadBalancerEntry{
		Host:        "foo.sky.com",
		Path:        "/foo",
		ServiceName: "foo-svc",
		ServicePort: 80,
	}}
	lb.AssertCalled(t, "Update", entries)
}
