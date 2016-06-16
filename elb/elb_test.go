package elb

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	aws_elb "github.com/aws/aws-sdk-go/service/elb"
	"github.com/sky-uk/feed/controller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	clusterName               = "cluster_name"
	region                    = "eu-west-1"
	frontendTag               = "sky.uk/KubernetesClusterFrontend"
	canonicalHostedZoneNameID = "test-id"
)

type fakeElb struct {
	mock.Mock
}

func (m *fakeElb) DescribeLoadBalancers(input *aws_elb.DescribeLoadBalancersInput) (*aws_elb.DescribeLoadBalancersOutput, error) {
	args := m.Called(input)

	return args.Get(0).(*aws_elb.DescribeLoadBalancersOutput), args.Error(1)
}

func (m *fakeElb) DescribeTags(input *aws_elb.DescribeTagsInput) (*aws_elb.DescribeTagsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*aws_elb.DescribeTagsOutput), args.Error(1)
}

func (m *fakeElb) DeregisterInstancesFromLoadBalancer(input *aws_elb.DeregisterInstancesFromLoadBalancerInput) (*aws_elb.DeregisterInstancesFromLoadBalancerOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*aws_elb.DeregisterInstancesFromLoadBalancerOutput), args.Error(1)
}

func (m *fakeElb) RegisterInstancesWithLoadBalancer(input *aws_elb.RegisterInstancesWithLoadBalancerInput) (*aws_elb.RegisterInstancesWithLoadBalancerOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*aws_elb.RegisterInstancesWithLoadBalancerOutput), args.Error(1)
}

type fakeMetadata struct {
	mock.Mock
}

func (m *fakeMetadata) Available() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *fakeMetadata) Region() (string, error) {
	args := m.Called()
	return args.String(0), nil
}

func (m *fakeMetadata) GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error) {
	args := m.Called()
	return args.Get(0).(ec2metadata.EC2InstanceIdentityDocument), args.Error(1)
}

func mockLoadBalancers(m *fakeElb, lbs ...string) {
	var descriptions []*aws_elb.LoadBalancerDescription
	for _, lb := range lbs {
		descriptions = append(descriptions, &aws_elb.LoadBalancerDescription{
			LoadBalancerName:          aws.String(lb),
			CanonicalHostedZoneNameID: aws.String(canonicalHostedZoneNameID),
		})

	}
	m.On("DescribeLoadBalancers", mock.AnythingOfType("*elb.DescribeLoadBalancersInput")).Return(&aws_elb.DescribeLoadBalancersOutput{
		LoadBalancerDescriptions: descriptions,
	}, nil)
}

type lbTags struct {
	tags []*aws_elb.Tag
	name string
}

func mockClusterTags(m *fakeElb, lbs ...lbTags) {
	var tagDescriptions []*aws_elb.TagDescription

	for _, lb := range lbs {
		tagDescriptions = append(tagDescriptions, &aws_elb.TagDescription{
			LoadBalancerName: aws.String(lb.name),
			Tags:             lb.tags,
		})
	}

	m.On("DescribeTags", mock.AnythingOfType("*elb.DescribeTagsInput")).Return(&aws_elb.DescribeTagsOutput{
		TagDescriptions: tagDescriptions,
	}, nil)
}

func mockRegisterInstances(mockElb *fakeElb, elbName, instanceID string) {
	mockElb.On("RegisterInstancesWithLoadBalancer", &aws_elb.RegisterInstancesWithLoadBalancerInput{
		LoadBalancerName: aws.String(elbName),
		Instances:        []*aws_elb.Instance{&aws_elb.Instance{InstanceId: aws.String(instanceID)}},
	}).Return(&aws_elb.RegisterInstancesWithLoadBalancerOutput{
		Instances: []*aws_elb.Instance{&aws_elb.Instance{InstanceId: aws.String(instanceID)}},
	}, nil)
}

func mockInstanceMetadata(mockMd *fakeMetadata, instanceID string) {
	mockMd.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{InstanceID: instanceID}, nil)
}

func setup() (controller.Updater, *fakeElb, *fakeMetadata) {
	e := New(region, clusterName, 1)
	mockElb := &fakeElb{}
	mockMetadata := &fakeMetadata{}
	e.(*elb).awsElb = mockElb
	e.(*elb).metadata = mockMetadata
	return e, mockElb, mockMetadata
}

func TestNoopIfNoExpectedFrontEnds(t *testing.T) {
	//given
	e, mockElb, mockMetadata := setup()
	e.(*elb).labelValue = ""

	//when
	e.Start()
	e.Stop()

	//then
	mock.AssertExpectationsForObjects(t, mockElb.Mock, mockMetadata.Mock)
}

func TestAttachWithSingleMatchingLoadBalancers(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndDifferentCluster := "cluster-frontend-different-cluster"
	mockLoadBalancers(mockElb, clusterFrontEnd, clusterFrontEndDifferentCluster, "other")
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String(frontendTag), Value: aws.String(clusterName)}}},
		lbTags{name: clusterFrontEndDifferentCluster, tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String(frontendTag), Value: aws.String("different cluster")}}},
		lbTags{name: "other elb", tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String("Bannana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterInstances(mockElb, clusterFrontEnd, instanceID)

	//when
	err := e.Start()

	//then
	assert.NoError(t, e.Health())
	mockElb.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, err)
}

func TestReportsErrorIfExpectedNotMatched(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	e.(*elb).expectedNumber = 2
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndDifferentCluster := "cluster-frontend-different-cluster"
	mockLoadBalancers(mockElb, clusterFrontEnd, clusterFrontEndDifferentCluster, "other")
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String(frontendTag), Value: aws.String(clusterName)}}},
		lbTags{name: clusterFrontEndDifferentCluster, tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String(frontendTag), Value: aws.String("different cluster")}}},
		lbTags{name: "other elb", tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String("Bannana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterInstances(mockElb, clusterFrontEnd, instanceID)

	//when
	err := e.Start()

	//then
	assert.EqualError(t, err, "expected ELBs: 2 actual: 1")
}

func TestNameAndHostedZoneIDLoadBalancerDetailsAreExtracted(t *testing.T) {
	//given
	mockElb := &fakeElb{}
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndDifferentCluster := "cluster-frontend-different-cluster"
	mockLoadBalancers(mockElb, clusterFrontEnd, clusterFrontEndDifferentCluster, "other")
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String(frontendTag), Value: aws.String(clusterName)}}},
		lbTags{name: clusterFrontEndDifferentCluster, tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String(frontendTag), Value: aws.String("different cluster")}}},
		lbTags{name: "other elb", tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String("Bannana"), Value: aws.String("Tasty")}}},
	)

	//when
	frontEnds, _ := FindFrontEndElbs(mockElb, clusterName)

	//then
	assert.Equal(t, frontEnds["cluster-frontend"].Name, "cluster-frontend")
	assert.Equal(t, frontEnds["cluster-frontend"].HostedZoneID, canonicalHostedZoneNameID)
}

func TestAttachWithMultipleMatchingLoadBalancers(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	e.(*elb).expectedNumber = 2
	instanceID := "cow"
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEnd2 := "cluster-frontend2"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancers(mockElb, clusterFrontEnd, clusterFrontEnd2)
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String(frontendTag), Value: aws.String(clusterName)}}},
		lbTags{name: clusterFrontEnd2, tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String(frontendTag), Value: aws.String(clusterName)}}},
	)
	mockRegisterInstances(mockElb, clusterFrontEnd, instanceID)
	mockRegisterInstances(mockElb, clusterFrontEnd2, instanceID)

	//when
	err := e.Start()

	//then
	mockElb.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, err)
}

func TestErrorGettingMetadata(t *testing.T) {
	e, _, mockMetadata := setup()
	mockMetadata.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{}, fmt.Errorf("No metadata for you"))

	err := e.Start()

	assert.EqualError(t, err, "unable to query ec2 metadata service for InstanceId: No metadata for you")
}

func TestErrorDescribingInstances(t *testing.T) {
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockElb.On("DescribeLoadBalancers", mock.AnythingOfType("*elb.DescribeLoadBalancersInput")).Return(&aws_elb.DescribeLoadBalancersOutput{}, errors.New("oh dear oh dear"))

	err := e.Start()

	assert.EqualError(t, err, "unable to describe load balancers: oh dear oh dear")
}

func TestErrorDescribingTags(t *testing.T) {
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancers(mockElb, "one")
	mockElb.On("DescribeTags", mock.AnythingOfType("*elb.DescribeTagsInput")).Return(&aws_elb.DescribeTagsOutput{}, errors.New("oh dear oh dear"))

	err := e.Start()

	assert.EqualError(t, err, "unable to describe tags: oh dear oh dear")
}

func TestNoMatchingElbs(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerName := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancers(mockElb, loadBalancerName)
	// No cluster tags
	mockClusterTags(mockElb, lbTags{name: loadBalancerName, tags: []*aws_elb.Tag{}})

	// when
	err := e.Start()

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestGetLoadBalancerPages(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerName := "lb1"
	mockElb.On("DescribeLoadBalancers", &aws_elb.DescribeLoadBalancersInput{}).Return(&aws_elb.DescribeLoadBalancersOutput{NextMarker: aws.String("Use me")}, nil)
	mockElb.On("DescribeLoadBalancers", &aws_elb.DescribeLoadBalancersInput{Marker: aws.String("Use me")}).Return(&aws_elb.DescribeLoadBalancersOutput{
		LoadBalancerDescriptions: []*aws_elb.LoadBalancerDescription{&aws_elb.LoadBalancerDescription{
			LoadBalancerName:          aws.String(loadBalancerName),
			CanonicalHostedZoneNameID: aws.String(canonicalHostedZoneNameID),
		}},
	}, nil)
	mockInstanceMetadata(mockMetadata, instanceID)
	mockClusterTags(mockElb, lbTags{name: loadBalancerName, tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String(frontendTag), Value: aws.String(clusterName)}}})
	mockRegisterInstances(mockElb, loadBalancerName, instanceID)

	// when
	err := e.Start()

	// then
	assert.NoError(t, err)
	mock.AssertExpectationsForObjects(t, mockElb.Mock)
}

func TestTagCallsPage(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	e.(*elb).expectedNumber = 2
	instanceID := "cow"
	loadBalancerName1 := "lb1"
	loadBalancerName2 := "lb2"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancers(mockElb, loadBalancerName1, loadBalancerName2)
	mockClusterTags(mockElb,
		lbTags{name: loadBalancerName1, tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String(frontendTag), Value: aws.String(clusterName)}}},
		lbTags{name: loadBalancerName2, tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String(frontendTag), Value: aws.String(clusterName)}}})
	mockRegisterInstances(mockElb, loadBalancerName1, instanceID)
	mockRegisterInstances(mockElb, loadBalancerName2, instanceID)

	// when
	err := e.Start()

	// then
	assert.NoError(t, err)
	mock.AssertExpectationsForObjects(t, mockElb.Mock)
}

func TestDeregistersWithAttachedELBs(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEnd2 := "cluster-frontend2"
	mockLoadBalancers(mockElb, clusterFrontEnd, clusterFrontEnd2, "other")
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String(frontendTag), Value: aws.String(clusterName)}}},
		lbTags{name: clusterFrontEnd2, tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String(frontendTag), Value: aws.String(clusterName)}}},
		lbTags{name: "other elb", tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String("Bannana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterInstances(mockElb, clusterFrontEnd, instanceID)
	mockRegisterInstances(mockElb, clusterFrontEnd2, instanceID)

	mockElb.On("DeregisterInstancesFromLoadBalancer", &aws_elb.DeregisterInstancesFromLoadBalancerInput{
		Instances:        []*aws_elb.Instance{&aws_elb.Instance{InstanceId: aws.String(instanceID)}},
		LoadBalancerName: aws.String(clusterFrontEnd),
	}).Return(&aws_elb.DeregisterInstancesFromLoadBalancerOutput{
		Instances: []*aws_elb.Instance{&aws_elb.Instance{InstanceId: aws.String(instanceID)}},
	}, nil)
	mockElb.On("DeregisterInstancesFromLoadBalancer", &aws_elb.DeregisterInstancesFromLoadBalancerInput{
		Instances:        []*aws_elb.Instance{&aws_elb.Instance{InstanceId: aws.String(instanceID)}},
		LoadBalancerName: aws.String(clusterFrontEnd2),
	}).Return(&aws_elb.DeregisterInstancesFromLoadBalancerOutput{
		Instances: []*aws_elb.Instance{&aws_elb.Instance{InstanceId: aws.String(instanceID)}},
	}, nil)

	//when
	e.Start()
	err := e.Stop()

	//then
	assert.NoError(t, err)
	mock.AssertExpectationsForObjects(t, mockElb.Mock)
}

func TestRegisterInstanceError(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancers(mockElb, clusterFrontEnd)
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String(frontendTag), Value: aws.String(clusterName)}}},
	)
	mockElb.On("RegisterInstancesWithLoadBalancer", mock.Anything).Return(&aws_elb.RegisterInstancesWithLoadBalancerOutput{}, errors.New("no register for you"))

	// when
	err := e.Start()

	// then
	assert.EqualError(t, err, "unable to register instance cow with elb cluster-frontend: no register for you")
}

func TestDeRegisterInstanceError(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancers(mockElb, clusterFrontEnd)
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: []*aws_elb.Tag{&aws_elb.Tag{Key: aws.String(frontendTag), Value: aws.String(clusterName)}}},
	)
	mockRegisterInstances(mockElb, clusterFrontEnd, instanceID)
	mockElb.On("DeregisterInstancesFromLoadBalancer", mock.Anything).Return(&aws_elb.DeregisterInstancesFromLoadBalancerOutput{}, errors.New("no deregister for you"))

	// when
	e.Start()
	err := e.Stop()

	// then
	assert.EqualError(t, err, "at least one ELB failed to detach")
}
