/*
Package status provides an updater for an ELB frontend to update ingress statuses.
*/
package status

import (
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/elb"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/util"

	aws_elb "github.com/aws/aws-sdk-go/service/elb"
)

// Config for creating a new ingress controller.
type Config struct {
	Region           string
	LabelValue       string
	KubernetesClient k8s.Client
}

// New creates a new ELB frontend status updater.
func New(conf Config) (controller.Updater, error) {
	session, err := session.NewSession(&aws.Config{Region: &conf.Region})
	if err != nil {
		return nil, fmt.Errorf("unable to create ELB status updater: %v", err)
	}

	return &status{
		awsElb:           aws_elb.New(session),
		labelValue:       conf.LabelValue,
		kubernetesClient: conf.KubernetesClient,
	}, nil
}

type status struct {
	awsElb           elb.ELB
	labelValue       string
	kubernetesClient k8s.Client
	initialised      sync.Mutex
}

func (s *status) Start() error {
	return nil
}

// Stop removes this instance from all the front end ELBs
func (s *status) Stop() error {
	return nil
}

func (s *status) Health() error {
	return nil
}

func (s *status) Update(ingresses controller.IngressEntries) error {
	s.initialised.Lock()
	defer s.initialised.Unlock()

	clusterFrontEnds, err := elb.FindFrontEndElbs(s.awsElb, s.labelValue)
	if err != nil {
		return err
	}

	for _, ingress := range ingresses {
		if lb, ok := clusterFrontEnds[ingress.ELbScheme]; ok {
			newStatus := util.SliceToStatus([]string{lb.DNSName})

			if util.StatusUnchanged(ingress.Ingress.Status.LoadBalancer.Ingress, newStatus) {
				continue
			}

			ingress.Ingress.Status.LoadBalancer.Ingress = newStatus

			if err := s.kubernetesClient.UpdateIngressStatus(ingress.Ingress); err != nil {
				return err
			}
		}
	}

	return nil
}
