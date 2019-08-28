/*
Package alb provides an updater for an ALB frontend to attach NGINX to.
*/
package alb

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	awselb "github.com/aws/aws-sdk-go/service/elbv2"
	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/util"
)

// New creates a controller.Updater for attaching to ALB target groups on first update.
func New(region string, targetGroupNames []string, targetGroupDeregistrationDelay time.Duration) (controller.Updater, error) {
	if len(targetGroupNames) == 0 {
		return nil, errors.New("unable to create ALB updater: missing target group names")
	}
	initMetrics()
	log.Infof("ALB frontend region: %s target groups: %v", region, targetGroupNames)
	awsSession, err := session.NewSession(&aws.Config{Region: &region})

	if err != nil {
		return nil, fmt.Errorf("unable to create ALB updater: %v", err)
	}

	return &alb{
		metadata:                       ec2metadata.New(awsSession),
		awsALB:                         awselb.New(awsSession),
		targetGroupNames:               targetGroupNames,
		targetGroupDeregistrationDelay: targetGroupDeregistrationDelay,
		region:                         region,
		initialised:                    initialised{},
	}, nil
}

type alb struct {
	awsALB                         ALB
	metadata                       EC2Metadata
	targetGroupNames               []string
	targetGroupDeregistrationDelay time.Duration
	region                         string
	instanceID                     string
	albARNs                        []*string
	registeredFrontends            util.SafeInt
	initialised                    initialised
	readyForHealthCheck            util.SafeBool
}

type initialised struct {
	sync.Mutex
	done bool
}

// ALB interface to allow mocking of real calls to AWS as well as cutting down the methods from the real
// interface to only the ones we use
type ALB interface {
	DescribeTargetGroups(input *awselb.DescribeTargetGroupsInput) (*awselb.DescribeTargetGroupsOutput, error)
	RegisterTargets(input *awselb.RegisterTargetsInput) (*awselb.RegisterTargetsOutput, error)
	DeregisterTargets(input *awselb.DeregisterTargetsInput) (*awselb.DeregisterTargetsOutput, error)
}

// EC2Metadata interface to allow mocking of the real calls to AWS
type EC2Metadata interface {
	Available() bool
	Region() (string, error)
	GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error)
}

func (a *alb) Start() error {
	return nil
}

func (a *alb) Update(controller.IngressEntries) error {
	a.initialised.Lock()
	defer a.initialised.Unlock()
	defer func() { a.readyForHealthCheck.Set(true) }()

	if !a.initialised.done {
		log.Infof("Attaching to ALB target groups: %v", a.targetGroupNames)
		if err := a.attachToFrontEnds(); err != nil {
			return err
		}
		a.initialised.done = true
	}
	return nil
}

// Stop removes this instance from all the front end ALBs
func (a *alb) Stop() error {
	for _, arn := range a.albARNs {
		log.Infof("Deregistering instance %s with ALB target group %s", a.instanceID, *arn)

		_, err := a.awsALB.DeregisterTargets(&awselb.DeregisterTargetsInput{
			TargetGroupArn: arn,
			Targets: []*awselb.TargetDescription{
				{Id: aws.String(a.instanceID)},
			},
		})

		if err != nil {
			log.Warnf("Unable to deregister instance %s with ALB target group %s: %v", a.instanceID, *arn, err)
		}
	}

	log.Infof("Waiting %v to finish ALB target group deregistration", a.targetGroupDeregistrationDelay)
	time.Sleep(a.targetGroupDeregistrationDelay)

	return nil
}

// Health returns nil if attached to all frontends.
func (a *alb) Health() error {
	if !a.readyForHealthCheck.Get() || len(a.targetGroupNames) == a.registeredFrontends.Get() {
		return nil
	}
	return fmt.Errorf("have not attached to all frontends %v yet", a.targetGroupNames)
}

func (a *alb) String() string {
	return "ELB frontend"
}

func (a *alb) attachToFrontEnds() error {
	if len(a.targetGroupNames) == 0 {
		return nil
	}

	instanceDoc, err := a.metadata.GetInstanceIdentityDocument()
	if err != nil {
		return fmt.Errorf("unable to query ec2 metadata service for InstanceId: %v", err)
	}
	instanceID := instanceDoc.InstanceID
	a.instanceID = instanceID

	arns, err := a.findTargetGroupARNs(a.targetGroupNames)
	if err != nil {
		return err
	}
	log.Infof("Found %d front ends", len(arns))
	a.albARNs = arns

	registered := 0
	for _, arn := range arns {
		log.Infof("Registering instance %s with alb %s", instanceID, *arn)

		_, err = a.awsALB.RegisterTargets(&awselb.RegisterTargetsInput{
			TargetGroupArn: arn,
			Targets: []*awselb.TargetDescription{
				{Id: aws.String(instanceID)},
			},
		})

		if err != nil {
			return fmt.Errorf("unable to register instance %s with ALB target group %s: %v", instanceID, *arn, err)
		}
		registered++
	}

	attachedFrontendGauge.Set(float64(registered))
	a.registeredFrontends.Set(registered)

	if len(a.targetGroupNames) != registered {
		return fmt.Errorf("only attached to %d ALBs, expected %d", registered, len(a.targetGroupNames))
	}

	return nil
}

func (a *alb) findTargetGroupARNs(names []string) ([]*string, error) {
	req := &awselb.DescribeTargetGroupsInput{Names: aws.StringSlice(names)}
	var arns []*string

	for {
		resp, err := a.awsALB.DescribeTargetGroups(req)
		if err != nil {
			return nil, err
		}

		for _, targetGroup := range resp.TargetGroups {
			arns = append(arns, targetGroup.TargetGroupArn)
		}

		if resp.NextMarker == nil {
			break
		}

		req.Marker = resp.NextMarker
	}

	return arns, nil
}
