/*
Package alb provides an updater for an ALB frontend to attach nginx to.
*/
package alb

import (
	"fmt"

	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	aws_alb "github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/sky-uk/feed/controller"
)

// New creates a controller.Updater for attaching to ALB target groups on first update.
func New(region string, targetGroupNames []string) controller.Updater {
	initMetrics()
	log.Infof("Will attach to ALBs %v in %s", targetGroupNames, region)
	session := session.New(&aws.Config{Region: &region})
	return &alb{
		metadata:         ec2metadata.New(session),
		awsALB:           aws_alb.New(session),
		targetGroupNames: targetGroupNames,
		region:           region,
		initialised:      initialised{},
	}
}

type alb struct {
	awsALB              ALB
	metadata            EC2Metadata
	targetGroupNames    []string
	region              string
	instanceID          string
	albARNs             []*string
	registeredFrontends int
	initialised         initialised
}

type initialised struct {
	sync.Mutex
	done bool
}

// ALB interface to allow mocking of real calls to AWS as well as cutting down the methods from the real
// interface to only the ones we use
type ALB interface {
	DescribeTargetGroups(input *aws_alb.DescribeTargetGroupsInput) (*aws_alb.DescribeTargetGroupsOutput, error)
	RegisterTargets(input *aws_alb.RegisterTargetsInput) (*aws_alb.RegisterTargetsOutput, error)
	DeregisterTargets(input *aws_alb.DeregisterTargetsInput) (*aws_alb.DeregisterTargetsOutput, error)
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

func (a *alb) Update(controller.IngressUpdate) error {
	a.initialised.Lock()
	defer a.initialised.Unlock()
	if !a.initialised.done {
		log.Info("First update. Attaching to front ends...")
		if err := a.attachToFrontEnds(); err != nil {
			return err
		}
		a.initialised.done = true
		log.Info("Attached to front ends.")
	}
	return nil
}

// Stop removes this instance from all the front end ALBs
func (a *alb) Stop() error {
	for _, arn := range a.albARNs {
		log.Infof("Deregistering instance %s with ALB target group %s", a.instanceID, *arn)

		_, err := a.awsALB.DeregisterTargets(&aws_alb.DeregisterTargetsInput{
			TargetGroupArn: arn,
			Targets: []*aws_alb.TargetDescription{
				{Id: aws.String(a.instanceID)},
			},
		})

		if err != nil {
			log.Warnf("Unable to deregister instance %s with ALB target group %s: %v", a.instanceID, *arn, err)
		}
	}

	return nil
}

// Health returns nil if attached to all frontends.
func (a *alb) Health() error {
	if len(a.targetGroupNames) == a.registeredFrontends {
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

		_, err = a.awsALB.RegisterTargets(&aws_alb.RegisterTargetsInput{
			TargetGroupArn: arn,
			Targets: []*aws_alb.TargetDescription{
				{Id: aws.String(instanceID)},
			},
		})

		if err != nil {
			return fmt.Errorf("unable to register instance %s with ALB target group %s: %v", instanceID, *arn, err)
		}
		registered++
	}

	attachedFrontendGauge.Set(float64(registered))
	a.registeredFrontends = registered

	if len(a.targetGroupNames) != registered {
		return fmt.Errorf("only attached to %d ALBs, expected %d", registered, len(a.targetGroupNames))
	}

	return nil
}

func (a *alb) findTargetGroupARNs(names []string) ([]*string, error) {
	req := &aws_alb.DescribeTargetGroupsInput{Names: aws.StringSlice(names)}
	var arns []*string

	for {
		resp, err := a.awsALB.DescribeTargetGroups(req)
		if err != nil {
			return nil, err
		}

		for _, targetGroup := range resp.TargetGroups {
			arns = append(arns, targetGroup.TargetGroupArn)
		}

		fmt.Printf("Setting marker: %v\n", resp.NextMarker)

		if resp.NextMarker == nil {
			break
		}

		req.Marker = resp.NextMarker
	}

	return arns, nil
}
