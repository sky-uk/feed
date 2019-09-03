/*
Package elb provides an updater for an ELB frontend to attach NGINX to.
*/
package elb

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	awselbv1 "github.com/aws/aws-sdk-go/service/elb"
	awselbv2 "github.com/aws/aws-sdk-go/service/elbv2"
	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/util"
)

// FrontendTag is the tag key used for identifying ELBs to attach to for a cluster.
const FrontendTag = "sky.uk/KubernetesClusterFrontend"

// IngressClassTag is the tag key used for identifying ELBs to attach to for a given ingress controller.
const IngressClassTag = "sky.uk/KubernetesClusterIngressClass"

// LoadBalancerType defines which type of AWS load balancer is being used for the frontend
// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/load-balancer-types.html
type LoadBalancerType int

const (
	// Classic represents the first generation of AWS ELB (Classic Load Balancer)
	Classic LoadBalancerType = iota
	// Standard represents the current generation of AWS ELB (Network or Application Load Balancer)
	Standard
)

// New creates a new ELB frontend
func New(lbType LoadBalancerType, region string, frontendTagValue string, ingressClassTagValue string,
	expectedNumber int, drainDelay time.Duration) (controller.Updater, error) {
	if frontendTagValue == "" {
		return nil, fmt.Errorf("unable to create ELB updater: missing value for the tag %v", FrontendTag)
	}
	if ingressClassTagValue == "" {
		return nil, fmt.Errorf("unable to create ELB updater: missing value for the tag %v", IngressClassTag)
	}

	initMetrics()
	log.Infof("ELB Front end region: %s, cluster: %s, expected frontends: %d, ingress controller: %s",
		region, frontendTagValue, expectedNumber, ingressClassTagValue)

	awsSession, err := session.NewSession(&aws.Config{Region: &region})
	if err != nil {
		return nil, fmt.Errorf("unable to create ELB updater: %v", err)
	}

	return &elb{
		metadata:             ec2metadata.New(awsSession),
		awsElbV1:             awselbv1.New(awsSession),
		awsElbV2:             awselbv2.New(awsSession),
		lbType:               lbType,
		frontendTagValue:     frontendTagValue,
		ingressClassTagValue: ingressClassTagValue,
		region:               region,
		expectedNumber:       expectedNumber,
		initialised:          initialised{},
		drainDelay:           drainDelay,
	}, nil
}

// LoadBalancerDetails stores all the elb information we use.
type LoadBalancerDetails struct {
	Name            string
	TargetGroupArns []string
	DNSName         string
	HostedZoneID    string
	Scheme          string
}

type elb struct {
	awsElbV1             V1ELB
	awsElbV2             V2ELB
	lbType               LoadBalancerType
	metadata             EC2Metadata
	frontendTagValue     string
	ingressClassTagValue string
	region               string
	expectedNumber       int
	instanceID           string
	elbs                 map[string]LoadBalancerDetails
	registeredFrontends  util.SafeInt
	initialised          initialised
	drainDelay           time.Duration
	readyForHealthCheck  util.SafeBool
}

type initialised struct {
	sync.Mutex
	done bool
}

// V1ELB interface to allow mocking of real calls to AWS as well as cutting down the methods from the real
// interface to only the ones we use. V1 for Classic Load Balancers.
type V1ELB interface {
	DescribeLoadBalancers(input *awselbv1.DescribeLoadBalancersInput) (*awselbv1.DescribeLoadBalancersOutput, error)
	DescribeTags(input *awselbv1.DescribeTagsInput) (*awselbv1.DescribeTagsOutput, error)
	RegisterInstancesWithLoadBalancer(input *awselbv1.RegisterInstancesWithLoadBalancerInput) (*awselbv1.RegisterInstancesWithLoadBalancerOutput, error)
	DeregisterInstancesFromLoadBalancer(input *awselbv1.DeregisterInstancesFromLoadBalancerInput) (*awselbv1.DeregisterInstancesFromLoadBalancerOutput, error)
}

// V2ELB interface to allow mocking of real calls to AWS as well as cutting down the methods from the real
// interface to only the ones we use. V2 for NLBs and ALBs.
type V2ELB interface {
	DescribeLoadBalancers(input *awselbv2.DescribeLoadBalancersInput) (*awselbv2.DescribeLoadBalancersOutput, error)
	DescribeTargetGroups(input *awselbv2.DescribeTargetGroupsInput) (*awselbv2.DescribeTargetGroupsOutput, error)
	DescribeTags(input *awselbv2.DescribeTagsInput) (*awselbv2.DescribeTagsOutput, error)
	RegisterTargets(input *awselbv2.RegisterTargetsInput) (*awselbv2.RegisterTargetsOutput, error)
	DeregisterTargets(input *awselbv2.DeregisterTargetsInput) (*awselbv2.DeregisterTargetsOutput, error)
}

// EC2Metadata interface to allow mocking of the real calls to AWS
type EC2Metadata interface {
	Available() bool
	Region() (string, error)
	GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error)
}

func (e *elb) Start() error {
	return nil
}

func (e *elb) attachToFrontEnds() error {
	id, err := e.metadata.GetInstanceIdentityDocument()
	if err != nil {
		return fmt.Errorf("unable to query ec2 metadata service for InstanceId: %v", err)
	}

	instance := id.InstanceID
	log.Infof("Attaching to ELBs from instance %s", instance)
	clusterFrontEnds, err := FindFrontEndElbsWithIngressClassName(e.lbType, e.awsElbV1, e.awsElbV2, e.frontendTagValue, e.ingressClassTagValue)

	if err != nil {
		return err
	}

	log.Infof("Found %d front ends", len(clusterFrontEnds))

	// Save these now so we can always know what we might have done
	// up until this point we have only read data
	e.elbs = clusterFrontEnds
	e.instanceID = instance
	registered := 0

	for _, frontend := range clusterFrontEnds {
		log.Infof("Registering instance %s with elb %s", instance, frontend.Name)

		if e.lbType == Classic {
			err = registerWithLoadBalancerV1(e.awsElbV1, instance, frontend)
		} else {
			err = registerWithLoadBalancerV2(e.awsElbV2, instance, frontend)
		}

		if err != nil {
			return fmt.Errorf("unable to register instance %s with elb %s: %v", instance, frontend.Name, err)
		}
		registered++
	}

	attachedFrontendGauge.Set(float64(registered))
	e.registeredFrontends.Set(registered)

	if e.expectedNumber > 0 && registered != e.expectedNumber {
		return fmt.Errorf("expected ELBs: %d actual: %d", e.expectedNumber, registered)
	}

	return nil
}

// FindFrontEndElbsV1 supports finding ELBs without ingress class for backwards compatibility
// with feed-dns, which does not support multiple ingress controllers
func FindFrontEndElbsV1(awsElb V1ELB, frontendTagValue string) (map[string]LoadBalancerDetails, error) {
	return FindFrontEndElbsWithIngressClassName(Classic, awsElb, nil, frontendTagValue, "")
}

// FindFrontEndElbsWithIngressClassName finds all ELBs tagged with frontendTagValue and ingressClassValue
func FindFrontEndElbsWithIngressClassName(lbType LoadBalancerType, awsElbV1 V1ELB, awsElbV2 V2ELB,
	frontendTagValue string, ingressClassValue string) (map[string]LoadBalancerDetails, error) {
	maxTagQuery := 20
	var allLbs map[string]LoadBalancerDetails
	var lbNames []*string
	var err error

	if lbType == Classic {
		allLbs, lbNames, err = findFrontendElbsV1(awsElbV1)
	} else {
		allLbs, lbNames, err = findFrontendElbsV2(awsElbV2)
	}
	if err != nil {
		return nil, err
	}

	log.Debugf("Found %d loadbalancers.", len(lbNames))

	requiredTags := map[string]string{FrontendTag: frontendTagValue}

	if ingressClassValue != "" {
		requiredTags[IngressClassTag] = ingressClassValue
	}

	clusterFrontEnds := make(map[string]LoadBalancerDetails)
	partitions := util.Partition(len(lbNames), maxTagQuery)
	for _, partition := range partitions {
		names := lbNames[partition.Low:partition.High]
		var tagsByLbName map[string][]tag
		if lbType == Classic {
			tagsByLbName, err = findTagsV1(awsElbV1, names)
		} else {
			tagsByLbName, err = findTagsV2(awsElbV2, names)
		}

		if err != nil {
			return nil, fmt.Errorf("unable to describe tags: %v", err)
		}

		// todo cb error out if we already have an internal or public facing elb
		for lbName, tags := range tagsByLbName {
			if tagsDoMatch(tags, requiredTags) {
				log.Infof("Found frontend elb %s", lbName)
				lb := allLbs[lbName]
				clusterFrontEnds[lb.Scheme] = lb
			}
		}
	}
	return clusterFrontEnds, nil
}

type tag struct {
	Key   string
	Value string
}

func tagsDoMatch(elbTags []tag, tagsToMatch map[string]string) bool {
	matches := 0
	for name, value := range tagsToMatch {
		log.Debugf("Checking for %s tag set to %s", name, value)
		for _, tag := range elbTags {
			if name == tag.Key && value == tag.Value {
				matches++
			}
		}
	}

	return matches == len(tagsToMatch)
}

// Stop removes this instance from all the front end ELBs
func (e *elb) Stop() error {
	var failed = false
	for _, elb := range e.elbs {
		log.Infof("Deregistering instance %s with elb %s", e.instanceID, elb.Name)
		var err error
		if e.lbType == Classic {
			err = deregisterFromLoadBalancerV1(e.awsElbV1, e.instanceID, elb)
		} else {
			err = deregisterFromLoadBalancerV2(e.awsElbV2, e.instanceID, elb)
		}

		if err != nil {
			log.Warnf("unable to deregister instance %s with elb %s: %v", e.instanceID, elb.Name, err)
			failed = true
		}
	}
	if failed {
		return errors.New("at least one ELB failed to detach")
	}

	log.Infof("Waiting %v to finish ELB deregistration", e.drainDelay)
	time.Sleep(e.drainDelay)

	return nil
}

func (e *elb) Health() error {
	if !e.readyForHealthCheck.Get() || e.expectedNumber == e.registeredFrontends.Get() {
		return nil
	}

	return fmt.Errorf("expected ELBs: %d actual: %d", e.expectedNumber, e.registeredFrontends.Get())
}

func (e *elb) Update(controller.IngressEntries) error {
	e.initialised.Lock()
	defer e.initialised.Unlock()
	defer func() { e.readyForHealthCheck.Set(true) }()

	if !e.initialised.done {
		log.Info("First update. Attaching to front ends.")
		if err := e.attachToFrontEnds(); err != nil {
			return err
		}
		e.initialised.done = true
	}
	return nil
}

func (e *elb) String() string {
	return "ELB frontend"
}
