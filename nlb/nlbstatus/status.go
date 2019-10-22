/*
Package nlbstatus provides an updater for an NLB frontend to update ingress statuses.
*/
package nlbstatus

import (
	"fmt"

	"github.com/sky-uk/feed/nlb"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/k8s"
	k8sStatus "github.com/sky-uk/feed/k8s/status"
	v1 "k8s.io/api/core/v1"
)

// Config for creating a new NLB status updater.
type Config struct {
	Region              string
	FrontendTagValue    string
	IngressNameTagValue string
	KubernetesClient    k8s.Client
}

// New creates a new NLB frontend status updater.
func New(conf Config) (controller.Updater, error) {
	awsSession, err := session.NewSession(&aws.Config{Region: &conf.Region})
	if err != nil {
		return nil, fmt.Errorf("unable to create NLB status updater: %v", err)
	}

	return &status{
		awsElb:              elbv2.New(awsSession),
		frontendTagValue:    conf.FrontendTagValue,
		ingressNameTagValue: conf.IngressNameTagValue,
		loadBalancers:       make(map[string]v1.LoadBalancerStatus),
		kubernetesClient:    conf.KubernetesClient,
	}, nil
}

type status struct {
	awsElb              nlb.ELBV2
	frontendTagValue    string
	ingressNameTagValue string
	loadBalancers       map[string]v1.LoadBalancerStatus
	kubernetesClient    k8s.Client
}

// Start discovers the NLBs and generates loadBalancer statuses.
func (s *status) Start() error {
	clusterFrontEnds, err := nlb.FindFrontEndLoadBalancersWithIngressClassName(s.awsElb, s.frontendTagValue, s.ingressNameTagValue)
	if err != nil {
		return err
	}

	for lbLabel, clusterFrontEnd := range clusterFrontEnds {
		s.loadBalancers[lbLabel] = k8sStatus.GenerateLoadBalancerStatus([]string{clusterFrontEnd.DNSName})
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
	return k8sStatus.Update(ingresses, s.loadBalancers, s.kubernetesClient)
}
