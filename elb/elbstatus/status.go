/*
Package elbstatus provides an updater for an ELB frontend to update ingress statuses.
*/
package elbstatus

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	awselb "github.com/aws/aws-sdk-go/service/elb"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/elb"
	"github.com/sky-uk/feed/k8s"
	k8sStatus "github.com/sky-uk/feed/k8s/status"
	v1 "k8s.io/api/core/v1"
)

// Config for creating a new ELB status updater.
type Config struct {
	Region              string
	Endpoint            string
	FrontendTagValue    string
	IngressNameTagValue string
	KubernetesClient    k8s.Client
}

// New creates a new ELB frontend status updater.
func New(conf Config) (controller.Updater, error) {
	awsSession, err := session.NewSession(&aws.Config{Region: aws.String(conf.Region), Endpoint: aws.String(conf.Endpoint)})
	if err != nil {
		return nil, fmt.Errorf("unable to create ELB status updater: %v", err)
	}

	return &status{
		awsElb:              awselb.New(awsSession),
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
