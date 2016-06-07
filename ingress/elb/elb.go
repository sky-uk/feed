package elb

import (
	"fmt"

	"errors"

	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	aws_elb "github.com/aws/aws-sdk-go/service/elb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sky-uk/feed/ingress"
)

const (
	elbTag = "sky.uk/KubernetesClusterFrontend"
)

var attachedFrontendGauge = prometheus.NewGauge(prometheus.GaugeOpts{
	Namespace: "feed",
	Subsystem: "ingress",
	Name:      "frontends_attached",
	Help:      "The total number of frontends attached",
})

// New  creates a new ELB frontend
func New(region string, clusterName string, expectedFrontends int) ingress.Frontend {
	log.Infof("ELB Front end region: %s cluster: %s expected frontends: %d", region, clusterName, expectedFrontends)
	metadata := ec2metadata.New(session.New())
	return &elb{
		metadata:          metadata,
		awsElb:            aws_elb.New(session.New(&aws.Config{Region: &region})),
		clusterName:       clusterName,
		region:            region,
		expectedFrontends: expectedFrontends,
		maxTagQuery:       20,
	}
}

type elb struct {
	awsElb              ELB
	metadata            EC2Metadata
	clusterName         string
	region              string
	expectedFrontends   int
	maxTagQuery         int
	instanceID          string
	elbs                []string
	registeredFrontends int
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

func (e *elb) Attach() error {

	if e.expectedFrontends == 0 {
		return nil
	}

	id, err := e.metadata.GetInstanceIdentityDocument()
	if err != nil {
		return fmt.Errorf("unable to query ec2 metadata service for InstanceId: %v", err)
	}

	instance := id.InstanceID
	log.Infof("Attaching to ELBs from instance %s", instance)
	clusterFrontEnds, err := e.findFrontEndElbs()

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
		log.Infof("Registering instance %s with elb %", instance, frontend)
		_, err = e.awsElb.RegisterInstancesWithLoadBalancer(&aws_elb.RegisterInstancesWithLoadBalancerInput{
			Instances: []*aws_elb.Instance{
				&aws_elb.Instance{
					InstanceId: aws.String(instance),
				}},
			LoadBalancerName: aws.String(frontend),
		})

		if err != nil {
			return fmt.Errorf("unable to register instance %s with elb %s: %v", instance, frontend, err)
		}
		registered++

	}

	prometheus.Register(attachedFrontendGauge)
	attachedFrontendGauge.Set(float64(registered))
	e.registeredFrontends = registered
	return nil
}

func (e *elb) findFrontEndElbs() ([]string, error) {
	// Find the load balancers that are tagged with this cluster name
	request := &aws_elb.DescribeLoadBalancersInput{}
	var lbNames []*string
	for {
		resp, err := e.awsElb.DescribeLoadBalancers(request)

		if err != nil {
			return nil, fmt.Errorf("unable to describe load balancers: %v", err)
		}

		for _, entry := range resp.LoadBalancerDescriptions {
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

	log.Infof("Found %d loadbalancers. Checking for %s tag set to %s", len(lbNames), elbTag, e.clusterName)
	var clusterFrontEnds []string
	totalLbs := len(lbNames)
	for i := 0; i < len(lbNames); i += e.maxTagQuery {
		to := min(i+e.maxTagQuery, totalLbs)
		log.Debugf("Querying tags from %d to %d", i, to)
		names := lbNames[i:to]
		output, err := e.awsElb.DescribeTags(&aws_elb.DescribeTagsInput{
			LoadBalancerNames: names,
		})

		if err != nil {
			return nil, fmt.Errorf("unable to describe tags: %v", err)
		}

		for _, description := range output.TagDescriptions {
			for _, tag := range description.Tags {
				if *tag.Key == elbTag && *tag.Value == e.clusterName {
					log.Infof("Found frontend elb %s", *description.LoadBalancerName)
					clusterFrontEnds = append(clusterFrontEnds, *description.LoadBalancerName)
				}
			}
		}
	}
	return clusterFrontEnds, nil
}

// Detach removes this instance from all the front end ELBs
func (e *elb) Detach() error {
	var failed = false
	for _, elb := range e.elbs {
		log.Infof("Deregistering instance %s with elb %s", e.instanceID, elb)
		_, err := e.awsElb.DeregisterInstancesFromLoadBalancer(&aws_elb.DeregisterInstancesFromLoadBalancerInput{
			Instances:        []*aws_elb.Instance{&aws_elb.Instance{InstanceId: aws.String(e.instanceID)}},
			LoadBalancerName: aws.String(elb),
		})

		if err != nil {
			log.Warnf("unable to deregister instance %s with elb %s: %v", e.instanceID, elb, err)
			failed = true
		}
	}
	if failed {
		return errors.New("at least one ELB failed to detach")
	}
	return nil
}

func (e *elb) Health() error {
	if e.registeredFrontends != e.expectedFrontends {
		return fmt.Errorf("expected frontends %d registered frontends %d", e.expectedFrontends, e.registeredFrontends)
	}
	return nil
}

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}
