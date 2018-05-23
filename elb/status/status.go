/*
Package status provides an updater for an ELB frontend to update ingress statuses.
*/
package status

import (
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/elb"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/util"

	aws_elb "github.com/aws/aws-sdk-go/service/elb"
	log "github.com/sirupsen/logrus"
)

// Config for creating a new ELB status updater.
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
	clusterFrontEnds map[string]elb.LoadBalancerDetails
}

// Start discovers the elbs
func (s *status) Start() error {
	clusterFrontEnds, err := elb.FindFrontEndElbs(s.awsElb, s.labelValue)
	if err != nil {
		return err
	}
	s.clusterFrontEnds = clusterFrontEnds
	return nil
}

func (s *status) Stop() error {
	return nil
}

func (s *status) Health() error {
	return nil
}

func (s *status) Update(ingresses controller.IngressEntries) error {
	var updateFailed bool
	for _, ingress := range ingresses {
		if lb, ok := s.clusterFrontEnds[ingress.ELbScheme]; ok {
			newStatus := util.SliceToStatus([]string{lb.DNSName})

			if util.StatusUnchanged(ingress.Ingress.Status.LoadBalancer.Ingress, newStatus) {
				continue
			}

			ingress.Ingress.Status.LoadBalancer.Ingress = newStatus

			if err := s.kubernetesClient.UpdateIngressStatus(ingress.Ingress); err != nil {
				log.Warn("Failed to update ingress status for %s: %e", ingress.Name, err)
				updateFailed = true
			}
		}
	}

	if updateFailed {
		return errors.New("failed to update all ingress statuses")
	}
	return nil
}
