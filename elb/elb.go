/*
Package elb provides an updater for an ELB frontend to attach nginx to.
*/
package elb

import (
	"fmt"

	"errors"

	"sync"

	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	aws_elb "github.com/aws/aws-sdk-go/service/elb"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/util"
)

// ElbTag is the tag key used for identifying ELBs to attach to.
const ElbTag = "sky.uk/KubernetesClusterFrontend"

// New creates a new ELB frontend
func New(region string, labelValue string, expectedNumber int, drainDelay time.Duration) controller.Updater {
	initMetrics()
	log.Infof("ELB Front end region: %s cluster: %s expected frontends: %d", region, labelValue, expectedNumber)
	metadata := ec2metadata.New(session.New())
	return &elb{
		metadata:       metadata,
		awsElb:         aws_elb.New(session.New(&aws.Config{Region: &region})),
		labelValue:     labelValue,
		region:         region,
		expectedNumber: expectedNumber,
		initialised:    initialised{},
		drainDelay:     drainDelay,
	}
}

// LoadBalancerDetails stores all the elb information we use.
type LoadBalancerDetails struct {
	Name         string
	DNSName      string
	HostedZoneID string
	Scheme       string
}

type elb struct {
	awsElb              ELB
	metadata            EC2Metadata
	labelValue          string
	region              string
	expectedNumber      int
	instanceID          string
	elbs                map[string]LoadBalancerDetails
	registeredFrontends util.SafeInt
	initialised         initialised
	drainDelay          time.Duration
	readyForHealthCheck util.SafeBool
}

type initialised struct {
	sync.Mutex
	done bool
}

// ELB interface to allow mocking of real calls to AWS as well as cutting down the methods from the real
// interface to only the ones we use
type ELB interface {
	DescribeLoadBalancers(input *aws_elb.DescribeLoadBalancersInput) (*aws_elb.DescribeLoadBalancersOutput, error)
	DescribeTags(input *aws_elb.DescribeTagsInput) (*aws_elb.DescribeTagsOutput, error)
	RegisterInstancesWithLoadBalancer(input *aws_elb.RegisterInstancesWithLoadBalancerInput) (*aws_elb.RegisterInstancesWithLoadBalancerOutput, error)
	DeregisterInstancesFromLoadBalancer(input *aws_elb.DeregisterInstancesFromLoadBalancerInput) (*aws_elb.DeregisterInstancesFromLoadBalancerOutput, error)
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
	if e.labelValue == "" {
		return nil
	}

	id, err := e.metadata.GetInstanceIdentityDocument()
	if err != nil {
		return fmt.Errorf("unable to query ec2 metadata service for InstanceId: %v", err)
	}

	instance := id.InstanceID
	log.Infof("Attaching to ELBs from instance %s", instance)
	clusterFrontEnds, err := FindFrontEndElbs(e.awsElb, e.labelValue)

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
		_, err = e.awsElb.RegisterInstancesWithLoadBalancer(&aws_elb.RegisterInstancesWithLoadBalancerInput{
			Instances: []*aws_elb.Instance{
				{
					InstanceId: aws.String(instance),
				}},
			LoadBalancerName: aws.String(frontend.Name),
		})

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

// FindFrontEndElbs finds all elbs tagged with 'sky.uk/KubernetesClusterFrontend=<labelValue>'
func FindFrontEndElbs(awsElb ELB, labelValue string) (map[string]LoadBalancerDetails, error) {
	maxTagQuery := 20
	// Find the load balancers that are tagged with this cluster name
	request := &aws_elb.DescribeLoadBalancersInput{}
	var lbNames []*string
	allLbs := make(map[string]LoadBalancerDetails)

	for {
		resp, err := awsElb.DescribeLoadBalancers(request)

		if err != nil {
			return nil, fmt.Errorf("unable to describe load balancers: %v", err)
		}

		for _, entry := range resp.LoadBalancerDescriptions {
			allLbs[*entry.LoadBalancerName] = LoadBalancerDetails{
				Name:         aws.StringValue(entry.LoadBalancerName),
				DNSName:      aws.StringValue(entry.DNSName),
				HostedZoneID: aws.StringValue(entry.CanonicalHostedZoneNameID),
				Scheme:       aws.StringValue(entry.Scheme),
			}
			lbNames = append(lbNames, entry.LoadBalancerName)
		}

		if resp.NextMarker == nil {
			break
		}

		// Set the next marker
		request = &aws_elb.DescribeLoadBalancersInput{
			Marker: resp.NextMarker,
		}
	}

	log.Debugf("Found %d loadbalancers. Checking for %s tag set to %s", len(lbNames), ElbTag, labelValue)
	clusterFrontEnds := make(map[string]LoadBalancerDetails)
	partitions := util.Partition(len(lbNames), maxTagQuery)
	for _, partition := range partitions {
		names := lbNames[partition.Low:partition.High]
		output, err := awsElb.DescribeTags(&aws_elb.DescribeTagsInput{
			LoadBalancerNames: names,
		})

		if err != nil {
			return nil, fmt.Errorf("unable to describe tags: %v", err)
		}

		// todo cb error out if we already have an internal or public facing elb
		for _, description := range output.TagDescriptions {
			for _, tag := range description.Tags {
				if *tag.Key == ElbTag && *tag.Value == labelValue {
					log.Infof("Found frontend elb %s", *description.LoadBalancerName)
					lb := allLbs[*description.LoadBalancerName]
					clusterFrontEnds[lb.Scheme] = lb
				}
			}
		}
	}
	return clusterFrontEnds, nil
}

// Stop removes this instance from all the front end ELBs
func (e *elb) Stop() error {
	var failed = false
	for _, elb := range e.elbs {
		log.Infof("Deregistering instance %s with elb %s", e.instanceID, elb.Name)
		_, err := e.awsElb.DeregisterInstancesFromLoadBalancer(&aws_elb.DeregisterInstancesFromLoadBalancerInput{
			Instances:        []*aws_elb.Instance{{InstanceId: aws.String(e.instanceID)}},
			LoadBalancerName: aws.String(elb.Name),
		})

		if err != nil {
			log.Warnf("unable to deregister instance %s with elb %s: %v", e.instanceID, elb.Name, err)
			failed = true
		}
	}
	if failed {
		return errors.New("at least one ELB failed to detach")
	}

	time.Sleep(e.drainDelay)

	return nil
}

func (e *elb) Health() error {
	if !e.readyForHealthCheck.Get() || e.expectedNumber == e.registeredFrontends.Get() {
		return nil
	}

	return fmt.Errorf("expected ELBs: %d actual: %d", e.expectedNumber, e.registeredFrontends.Get())
}

func (e *elb) Update(controller.IngressUpdate) error {
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
