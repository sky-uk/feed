/*
Package elb provides an updater for an ELB frontend to attach nginx to.
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
	awselb "github.com/aws/aws-sdk-go/service/elbv2"
	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/util"
)

// ElbTag is the tag key used for identifying ELBs to attach to for a cluster.
const ElbTag = "sky.uk/KubernetesClusterFrontend"

// IngressClassTag is the tag key used for identifying ELBs to attach to for a given ingress controller.
const IngressClassTag = "sky.uk/KubernetesClusterIngressClass"

// New creates a new ELB frontend
func New(region string, frontendTagValue string, ingressClassTagValue string, expectedNumber int, drainDelay time.Duration) (controller.Updater, error) {
	if frontendTagValue == "" {
		return nil, fmt.Errorf("unable to create ELB updater: missing value for the tag %v", ElbTag)
	}
	if ingressClassTagValue == "" {
		return nil, fmt.Errorf("unable to create ELB updater: missing value for the tag %v", IngressClassTag)
	}

	initMetrics()
	log.Infof("ELB Front end region: %s, cluster: %s, expected frontends: %d, ingress controller: %s", region, frontendTagValue, expectedNumber, ingressClassTagValue)

	session, err := session.NewSession(&aws.Config{Region: &region})
	if err != nil {
		return nil, fmt.Errorf("unable to create ELB updater: %v", err)
	}

	return &elb{
		metadata:             ec2metadata.New(session),
		awsElb:               awselb.New(session),
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
	Arn          string
	DNSName      string
	HostedZoneID string
	Scheme       string
}

type elb struct {
	awsElb               ELB
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

// ELB interface to allow mocking of real calls to AWS as well as cutting down the methods from the real
// interface to only the ones we use
type ELB interface {
	DescribeLoadBalancers(input *awselb.DescribeLoadBalancersInput) (*awselb.DescribeLoadBalancersOutput, error)
	DescribeTags(input *awselb.DescribeTagsInput) (*awselb.DescribeTagsOutput, error)
	RegisterTargets(input *awselb.RegisterTargetsInput) (*awselb.RegisterTargetsOutput, error)
	DeregisterTargets(input *awselb.DeregisterTargetsInput) (*awselb.DeregisterTargetsOutput, error)
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
	clusterFrontEnds, err := FindFrontEndElbsWithIngressClassName(e.awsElb, e.frontendTagValue, e.ingressClassTagValue)

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
		log.Infof("Registering instance %s with elb %s", instance, frontend.Arn)
		_, err = e.awsElb.RegisterTargets(&awselb.RegisterTargetsInput{
			Targets: []*awselb.TargetDescription{
				{
					Id: aws.String(instance),
				}},
			TargetGroupArn: aws.String(frontend.Arn),
		})

		if err != nil {
			return fmt.Errorf("unable to register instance %s with elb %s: %v", instance, frontend.Arn, err)
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

// FindFrontEndElbs supports finding ELBs without ingress class for backwards compatibility
// with feed-dns, which does not support multiple ingress controllers
func FindFrontEndElbs(awsElb ELB, frontendTagValue string) (map[string]LoadBalancerDetails, error) {
	return FindFrontEndElbsWithIngressClassName(awsElb, frontendTagValue, "")
}

// FindFrontEndElbsWithIngressClassName finds all ELBs tagged with frontendTagValue and ingressClassValue
func FindFrontEndElbsWithIngressClassName(awsElb ELB, frontendTagValue string, ingressClassValue string) (map[string]LoadBalancerDetails, error) {
	maxTagQuery := 20
	// Find the load balancers that are tagged with this cluster name
	request := &awselb.DescribeLoadBalancersInput{}
	var lbArns []*string
	allLbs := make(map[string]LoadBalancerDetails)

	for {
		resp, err := awsElb.DescribeLoadBalancers(request)

		if err != nil {
			return nil, fmt.Errorf("unable to describe load balancers: %v", err)
		}

		for _, entry := range resp.LoadBalancers {
			allLbs[*entry.LoadBalancerArn] = LoadBalancerDetails{
				Arn:          aws.StringValue(entry.LoadBalancerArn),
				DNSName:      aws.StringValue(entry.DNSName),
				HostedZoneID: aws.StringValue(entry.CanonicalHostedZoneId),
				Scheme:       aws.StringValue(entry.Scheme),
			}
			lbArns = append(lbArns, entry.LoadBalancerArn)
		}

		if resp.NextMarker == nil {
			break
		}

		// Set the next marker
		request = &awselb.DescribeLoadBalancersInput{
			Marker: resp.NextMarker,
		}
	}

	log.Debugf("Found %d loadbalancers.", len(lbArns))

	requiredTags := map[string]string{ElbTag: frontendTagValue}

	if ingressClassValue != "" {
		requiredTags[IngressClassTag] = ingressClassValue
	}

	clusterFrontEnds := make(map[string]LoadBalancerDetails)
	partitions := util.Partition(len(lbArns), maxTagQuery)
	for _, partition := range partitions {
		arns := lbArns[partition.Low:partition.High]
		output, err := awsElb.DescribeTags(&awselb.DescribeTagsInput{
			ResourceArns: arns,
		})

		if err != nil {
			return nil, fmt.Errorf("unable to describe tags: %v", err)
		}

		// todo cb error out if we already have an internal or public facing elb
		for _, elbDescription := range output.TagDescriptions {
			if tagsDoMatch(elbDescription.Tags, requiredTags) {
				log.Infof("Found frontend elb %s", *elbDescription.ResourceArn)
				lb := allLbs[*elbDescription.ResourceArn]
				clusterFrontEnds[lb.Scheme] = lb
			}
		}
	}
	return clusterFrontEnds, nil
}

func tagsDoMatch(elbTags []*awselb.Tag, tagsToMatch map[string]string) bool {
	matches := 0
	for name, value := range tagsToMatch {
		log.Debugf("Checking for %s tag set to %s", name, value)
		for _, elb := range elbTags {
			if name == *elb.Key && value == *elb.Value {
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
		log.Infof("Deregistering instance %s with elb %s", e.instanceID, elb.Arn)
		_, err := e.awsElb.DeregisterTargets(&awselb.DeregisterTargetsInput{
			Targets:        []*awselb.TargetDescription{{Id: aws.String(e.instanceID)}},
			TargetGroupArn: aws.String(elb.Arn),
		})

		if err != nil {
			log.Warnf("unable to deregister instance %s with elb %s: %v", e.instanceID, elb.Arn, err)
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
