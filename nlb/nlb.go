/*
Package nlb provides an updater for an ELB frontend to attach NGINX to.
*/
package nlb

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sky-uk/feed/elb"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elbv2"
	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/util"
)

// New creates a new NLB frontend
func New(region string, frontendTagValue string, ingressClassTagValue string,
	expectedNumber int, drainDelay time.Duration) (controller.Updater, error) {
	if frontendTagValue == "" {
		return nil, fmt.Errorf("unable to create NLB updater: missing value for the tag %v", elb.FrontendTag)
	}
	if ingressClassTagValue == "" {
		return nil, fmt.Errorf("unable to create NLB updater: missing value for the tag %v", elb.IngressClassTag)
	}

	initMetrics()
	log.Infof("NLB Front end region: %s, cluster: %s, expected frontends: %d, ingress controller: %s",
		region, frontendTagValue, expectedNumber, ingressClassTagValue)

	awsSession, err := session.NewSession(&aws.Config{Region: &region})
	if err != nil {
		return nil, fmt.Errorf("unable to create NLB updater: %v", err)
	}

	return &nlb{
		metadata:             ec2metadata.New(awsSession),
		awsElb:               elbv2.New(awsSession),
		frontendTagValue:     frontendTagValue,
		ingressClassTagValue: ingressClassTagValue,
		region:               region,
		expectedNumber:       expectedNumber,
		initialised:          initialised{},
		drainDelay:           drainDelay,
	}, nil
}

// LoadBalancerDetails stores all the nlb information we use.
type LoadBalancerDetails struct {
	Name            string
	TargetGroupArns []string
	DNSName         string
	HostedZoneID    string
	Scheme          string
}

type nlb struct {
	awsElb               ELBV2
	metadata             EC2Metadata
	frontendTagValue     string
	ingressClassTagValue string
	region               string
	expectedNumber       int
	privateIPAddress     string
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

// ELBV2 interface to allow mocking of real calls to AWS as well as cutting down the methods from the real
// interface to only the ones we use.
type ELBV2 interface {
	DescribeLoadBalancers(input *elbv2.DescribeLoadBalancersInput) (*elbv2.DescribeLoadBalancersOutput, error)
	DescribeTargetGroups(input *elbv2.DescribeTargetGroupsInput) (*elbv2.DescribeTargetGroupsOutput, error)
	DescribeTags(input *elbv2.DescribeTagsInput) (*elbv2.DescribeTagsOutput, error)
	RegisterTargets(input *elbv2.RegisterTargetsInput) (*elbv2.RegisterTargetsOutput, error)
	DeregisterTargets(input *elbv2.DeregisterTargetsInput) (*elbv2.DeregisterTargetsOutput, error)
}

// EC2Metadata interface to allow mocking of the real calls to AWS
type EC2Metadata interface {
	Available() bool
	Region() (string, error)
	GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error)
}

func (e *nlb) Start() error {
	return nil
}

func (e *nlb) attachToFrontEnds() error {
	id, err := e.metadata.GetInstanceIdentityDocument()
	if err != nil {
		return fmt.Errorf("unable to query ec2 metadata service for InstanceId: %v", err)
	}

	privateIP := id.PrivateIP
	log.Infof("Attaching to NLBs from instance %s", privateIP)
	clusterFrontEnds, err := FindFrontEndLoadBalancersWithIngressClassName(e.awsElb, e.frontendTagValue, e.ingressClassTagValue)

	if err != nil {
		return err
	}

	log.Infof("Found %d front ends", len(clusterFrontEnds))

	// Save these now so we can always know what we might have done
	// up until this point we have only read data
	e.elbs = clusterFrontEnds
	e.privateIPAddress = privateIP
	registered := 0

	for _, frontend := range clusterFrontEnds {
		log.Infof("Registering instance %s with nlb %s", privateIP, frontend.Name)
		err = registerWithLoadBalancer(e.awsElb, privateIP, frontend)
		if err != nil {
			return fmt.Errorf("unable to register instance %s with nlb %s: %v", privateIP, frontend.Name, err)
		}
		registered++
	}

	attachedFrontendGauge.Set(float64(registered))
	e.registeredFrontends.Set(registered)

	if e.expectedNumber > 0 && registered != e.expectedNumber {
		return fmt.Errorf("expected NLBs: %d actual: %d", e.expectedNumber, registered)
	}

	return nil
}

func findFrontendLoadBalancers(awsElb ELBV2) (map[string]LoadBalancerDetails, []*string, error) {
	lbRequest := &elbv2.DescribeLoadBalancersInput{}
	var lbArns []*string
	allLbsByArn := make(map[string]LoadBalancerDetails)

	for {
		lbResp, err := awsElb.DescribeLoadBalancers(lbRequest)

		if err != nil {
			return nil, nil, fmt.Errorf("unable to describe load balancers: %v", err)
		}

		for _, entry := range lbResp.LoadBalancers {
			allLbsByArn[*entry.LoadBalancerArn] = LoadBalancerDetails{
				Name:            aws.StringValue(entry.LoadBalancerArn),
				TargetGroupArns: findTargetGroupArns(awsElb, entry),
				DNSName:         aws.StringValue(entry.DNSName),
				HostedZoneID:    aws.StringValue(entry.CanonicalHostedZoneId),
				Scheme:          aws.StringValue(entry.Scheme),
			}
			lbArns = append(lbArns, entry.LoadBalancerArn)
		}

		if lbResp.NextMarker == nil {
			return allLbsByArn, lbArns, nil
		}

		// Set the next marker
		lbRequest = &elbv2.DescribeLoadBalancersInput{
			Marker: lbResp.NextMarker,
		}
	}
}

func findTargetGroupArns(awsElb ELBV2, loadBalancer *elbv2.LoadBalancer) []string {
	request := &elbv2.DescribeTargetGroupsInput{
		LoadBalancerArn: loadBalancer.LoadBalancerArn,
	}

	response, err := awsElb.DescribeTargetGroups(request)
	if err != nil {
		log.Errorf("Could not query Target Groups for %s: %v", *loadBalancer.LoadBalancerArn, err)
		return nil
	}

	var arns []string
	for _, tg := range response.TargetGroups {
		arns = append(arns, *tg.TargetGroupArn)
	}

	return arns
}

func registerWithLoadBalancer(awsElb ELBV2, privateIP string, lb LoadBalancerDetails) error {
	var failedArns []string

	for _, arn := range lb.TargetGroupArns {
		log.Infof("Registering instance %s with Target Group %s", privateIP, arn)
		_, err := awsElb.RegisterTargets(&elbv2.RegisterTargetsInput{
			Targets:        []*elbv2.TargetDescription{{Id: aws.String(privateIP)}},
			TargetGroupArn: aws.String(arn),
		})
		if err != nil {
			log.Errorf("Could not register instance %s with Target Group %s: %v", privateIP, arn, err)
			failedArns = append(failedArns, arn)
		}
	}

	if failedArns != nil {
		return fmt.Errorf("could not register Target Group(s) with Instance %s: %v", privateIP, failedArns)
	}

	return nil
}

// FindFrontEndLoadBalancersWithIngressClassName finds all NLBs tagged with frontendTagValue and ingressClassValue
func FindFrontEndLoadBalancersWithIngressClassName(awsElb ELBV2,
	frontendTagValue string, ingressClassValue string) (map[string]LoadBalancerDetails, error) {
	maxTagQuery := 20
	var allLbs map[string]LoadBalancerDetails
	var lbNames []*string
	var err error

	allLbs, lbNames, err = findFrontendLoadBalancers(awsElb)
	if err != nil {
		return nil, err
	}

	log.Debugf("Found %d loadbalancers.", len(lbNames))

	requiredTags := map[string]string{elb.FrontendTag: frontendTagValue}

	if ingressClassValue != "" {
		requiredTags[elb.IngressClassTag] = ingressClassValue
	}

	clusterFrontEnds := make(map[string]LoadBalancerDetails)
	partitions := util.Partition(len(lbNames), maxTagQuery)
	for _, partition := range partitions {
		names := lbNames[partition.Low:partition.High]
		tagsByLbName, err := findTags(awsElb, names)

		if err != nil {
			return nil, fmt.Errorf("unable to describe tags: %v", err)
		}

		// todo cb error out if we already have an internal or public facing nlb
		for lbName, tags := range tagsByLbName {
			if tagsDoMatch(tags, requiredTags) {
				log.Infof("Found frontend nlb %s", lbName)
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

func findTags(awsElb ELBV2, lbArns []*string) (map[string][]tag, error) {
	output, err := awsElb.DescribeTags(&elbv2.DescribeTagsInput{
		ResourceArns: lbArns,
	})
	if err != nil {
		return nil, err
	}

	tagsByLbArn := make(map[string][]tag)
	for _, elbDescription := range output.TagDescriptions {
		var tags []tag
		for _, elbTag := range elbDescription.Tags {
			tags = append(tags, tag{Key: *elbTag.Key, Value: *elbTag.Value})
		}
		tagsByLbArn[*elbDescription.ResourceArn] = tags
	}

	return tagsByLbArn, nil
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

// Stop removes this instance from all the front end NLBs
func (e *nlb) Stop() error {
	var failed = false
	for _, elb := range e.elbs {
		log.Infof("Deregistering instance %s with nlb %s", e.privateIPAddress, elb.Name)
		err := deregisterFromLoadBalancer(e.awsElb, e.privateIPAddress, elb)
		if err != nil {
			log.Warnf("unable to deregister instance %s with nlb %s: %v", e.privateIPAddress, elb.Name, err)
			failed = true
		}
	}
	if failed {
		return errors.New("at least one NLB failed to detach")
	}

	log.Infof("Waiting %v to finish NLB deregistration", e.drainDelay)
	time.Sleep(e.drainDelay)

	return nil
}

func deregisterFromLoadBalancer(awsElb ELBV2, privateIP string, lb LoadBalancerDetails) error {
	var failedArns []string

	for _, arn := range lb.TargetGroupArns {
		log.Infof("Deregistering instance %s from Target Group %s", privateIP, arn)
		_, err := awsElb.DeregisterTargets(&elbv2.DeregisterTargetsInput{
			Targets:        []*elbv2.TargetDescription{{Id: aws.String(privateIP)}},
			TargetGroupArn: aws.String(arn),
		})
		if err != nil {
			log.Errorf("Could not deregister instance %s from Target Group %s: %v", privateIP, arn, err)
			failedArns = append(failedArns, arn)
		}
	}

	if failedArns != nil {
		return fmt.Errorf("could not deregister Target Group(s) from Instance %s: %v", privateIP, failedArns)
	}

	return nil
}

func (e *nlb) Health() error {
	if !e.readyForHealthCheck.Get() || e.expectedNumber == e.registeredFrontends.Get() {
		return nil
	}

	return fmt.Errorf("expected NLBs: %d actual: %d", e.expectedNumber, e.registeredFrontends.Get())
}

func (e *nlb) Update(controller.IngressEntries) error {
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

func (e *nlb) String() string {
	return "NLB frontend"
}
