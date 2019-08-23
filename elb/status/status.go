/*
Package status provides an updater for an ELB frontend to update ingress statuses.
*/
package status

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	aws_elb "github.com/aws/aws-sdk-go/service/elb"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/elb"
	"github.com/sky-uk/feed/k8s"
	k8s_status "github.com/sky-uk/feed/k8s/status"
	v1 "k8s.io/client-go/pkg/api/v1"
)

// Config for creating a new ELB status updater.
type Config struct {
	Region              string
	FrontendTagValue    string
	IngressNameTagValue string
	KubernetesClient    k8s.Client
}

// New creates a new ELB frontend status updater.
func New(conf Config) (controller.Updater, error) {
	session, err := session.NewSession(&aws.Config{Region: &conf.Region})
	if err != nil {
		return nil, fmt.Errorf("unable to create ELB status updater: %v", err)
	}

	return &status{
		awsElb:              aws_elb.New(session),
		frontendTagValue:    conf.FrontendTagValue,
		ingressNameTagValue: conf.IngressNameTagValue,
		loadBalancers:       make(map[string]v1.LoadBalancerStatus),
		kubernetesClient:    conf.KubernetesClient,
	}, nil
}

type status struct {
	awsElb              elb.ELB
	frontendTagValue    string
	ingressNameTagValue string
	loadBalancers       map[string]v1.LoadBalancerStatus
	kubernetesClient    k8s.Client
}

// Start discovers the elbs and generates loadBalancer statuses.
func (s *status) Start() error {
	clusterFrontEnds, err := elb.FindFrontEndElbsWithIngressClassName(s.awsElb, s.frontendTagValue, s.ingressNameTagValue)
	if err != nil {
		return err
	}

	for lbLabel, clusterFrontEnd := range clusterFrontEnds {
		s.loadBalancers[lbLabel] = k8s_status.GenerateLoadBalancerStatus([]string{clusterFrontEnd.DNSName})
	}
	return nil
}

func (s *status) Stop() error {
	return nil
}

func (s *status) Health() error {
	return nil
}

func (s *status) Update(ingresses controller.IngressEntries) error {
	return k8s_status.Update(ingresses, s.loadBalancers, s.kubernetesClient)
}
