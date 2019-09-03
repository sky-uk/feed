package elb

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	awselb "github.com/aws/aws-sdk-go/service/elb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/util/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func init() {
	metrics.SetConstLabels(make(prometheus.Labels))
}

const (
	clusterName               = "cluster_name"
	ingressName               = "ingress_name"
	region                    = "eu-west-1"
	frontendTag               = "sky.uk/KubernetesClusterFrontend"
	ingressNameTag            = "sky.uk/KubernetesClusterIngressClass"
	canonicalHostedZoneNameID = "test-id"
	elbDNSName                = "elb-dnsname"
	elbInternalScheme         = "internal"
	elbInternetFacingScheme   = "internet-facing"
)

var defaultTags = []*awselb.Tag{
	{Key: aws.String(frontendTag), Value: aws.String(clusterName)},
	{Key: aws.String(ingressNameTag), Value: aws.String(ingressName)},
}

type fakeElb struct {
	mock.Mock
}

func (m *fakeElb) DescribeLoadBalancers(input *awselb.DescribeLoadBalancersInput) (*awselb.DescribeLoadBalancersOutput, error) {
	args := m.Called(input)

	return args.Get(0).(*awselb.DescribeLoadBalancersOutput), args.Error(1)
}

func (m *fakeElb) DescribeTags(input *awselb.DescribeTagsInput) (*awselb.DescribeTagsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselb.DescribeTagsOutput), args.Error(1)
}

func (m *fakeElb) DeregisterInstancesFromLoadBalancer(input *awselb.DeregisterInstancesFromLoadBalancerInput) (*awselb.DeregisterInstancesFromLoadBalancerOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselb.DeregisterInstancesFromLoadBalancerOutput), args.Error(1)
}

func (m *fakeElb) RegisterInstancesWithLoadBalancer(input *awselb.RegisterInstancesWithLoadBalancerInput) (*awselb.RegisterInstancesWithLoadBalancerOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselb.RegisterInstancesWithLoadBalancerOutput), args.Error(1)
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

type lb struct {
	name   string
	scheme string
}

func mockLoadBalancers(m *fakeElb, lbs ...lb) {
	var descriptions []*awselb.LoadBalancerDescription
	for _, lb := range lbs {
		descriptions = append(descriptions, &awselb.LoadBalancerDescription{
			LoadBalancerName:          aws.String(lb.name),
			CanonicalHostedZoneNameID: aws.String(canonicalHostedZoneNameID),
			Scheme:                    aws.String(lb.scheme),
			DNSName:                   aws.String(elbDNSName),
		})

	}
	m.On("DescribeLoadBalancers", mock.AnythingOfType("*elb.DescribeLoadBalancersInput")).Return(&awselb.DescribeLoadBalancersOutput{
		LoadBalancerDescriptions: descriptions,
	}, nil)
}

type lbTags struct {
	tags []*awselb.Tag
	name string
}

func mockClusterTags(m *fakeElb, lbs ...lbTags) {
	var tagDescriptions []*awselb.TagDescription

	for _, lb := range lbs {
		tagDescriptions = append(tagDescriptions, &awselb.TagDescription{
			LoadBalancerName: aws.String(lb.name),
			Tags:             lb.tags,
		})
	}

	m.On("DescribeTags", mock.AnythingOfType("*elb.DescribeTagsInput")).Return(&awselb.DescribeTagsOutput{
		TagDescriptions: tagDescriptions,
	}, nil)
}

func mockRegisterInstances(mockElb *fakeElb, elbName, instanceID string) {
	mockElb.On("RegisterInstancesWithLoadBalancer", &awselb.RegisterInstancesWithLoadBalancerInput{
		LoadBalancerName: aws.String(elbName),
		Instances:        []*awselb.Instance{{InstanceId: aws.String(instanceID)}},
	}).Return(&awselb.RegisterInstancesWithLoadBalancerOutput{
		Instances: []*awselb.Instance{{InstanceId: aws.String(instanceID)}},
	}, nil)
}

func mockInstanceMetadata(mockMd *fakeMetadata, instanceID string) {
	mockMd.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{InstanceID: instanceID}, nil)
}

func setup() (controller.Updater, *fakeElb, *fakeMetadata) {
	e, _ := New(Classic, region, clusterName, ingressName, 1, 0)
	mockElb := &fakeElb{}
	mockMetadata := &fakeMetadata{}
	e.(*elb).awsElbV1 = mockElb
	e.(*elb).metadata = mockMetadata
	return e, mockElb, mockMetadata
}

func TestCanNotCreateUpdaterWithoutFrontEndTagValue(t *testing.T) {
	//when
	_, err := New(Classic, region, "", ingressName, 1, 0)

	//then
	assert.Error(t, err)
}

func TestCanNotCreateUpdaterWithoutIngressNameTagValue(t *testing.T) {
	//when
	_, err := New(Classic, region, clusterName, "", 1, 0)

	//then
	assert.Error(t, err)
}

func TestAttachWithSingleMatchingLoadBalancers(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndDifferentCluster := "cluster-frontend-different-cluster"
	mockLoadBalancers(mockElb,
		lb{clusterFrontEnd, elbInternalScheme},
		lb{clusterFrontEndDifferentCluster, elbInternalScheme},
		lb{"other", elbInternalScheme})

	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: defaultTags},
		lbTags{name: clusterFrontEndDifferentCluster, tags: []*awselb.Tag{
			{Key: aws.String(frontendTag), Value: aws.String("different cluster")},
			{Key: aws.String(ingressName), Value: aws.String("different cluster")},
		}},
		lbTags{name: "other elb", tags: []*awselb.Tag{{Key: aws.String("Banana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterInstances(mockElb, clusterFrontEnd, instanceID)
	err := e.Start()

	//when
	_ = e.Update(controller.IngressEntries{})

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
	mockLoadBalancers(mockElb,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme},
		lb{name: clusterFrontEndDifferentCluster, scheme: elbInternalScheme},
		lb{name: "other", scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: defaultTags},
		lbTags{name: clusterFrontEndDifferentCluster, tags: []*awselb.Tag{
			{Key: aws.String(frontendTag), Value: aws.String("different cluster")},
			{Key: aws.String(ingressNameTag), Value: aws.String("different cluster")},
		}},
		lbTags{name: "other elb", tags: []*awselb.Tag{{Key: aws.String("Banana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterInstances(mockElb, clusterFrontEnd, instanceID)

	//when
	_ = e.Start()
	err := e.Update(controller.IngressEntries{})

	//then
	assert.EqualError(t, err, "expected ELBs: 2 actual: 1")
}

func TestNameAndDNSNameAndHostedZoneIDLoadBalancerDetailsAreExtracted(t *testing.T) {
	//given
	mockElb := &fakeElb{}
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancers(mockElb, lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: defaultTags},
	)

	//when
	frontends, _ := FindFrontEndElbsWithIngressClassName(Classic, mockElb, nil, clusterName, ingressName)

	//then
	assert.Equal(t, "cluster-frontend", frontends[elbInternalScheme].Name)
	assert.Equal(t, elbDNSName, frontends[elbInternalScheme].DNSName)
	assert.Equal(t, canonicalHostedZoneNameID, frontends[elbInternalScheme].HostedZoneID)
	assert.Equal(t, elbInternalScheme, frontends[elbInternalScheme].Scheme)
}

func TestFindElbWithoutIngressName(t *testing.T) {
	//given
	mockElb := &fakeElb{}
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancers(mockElb, lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: []*awselb.Tag{
			{Key: aws.String(frontendTag), Value: aws.String(clusterName)},
		}},
	)

	//when
	frontends, _ := FindFrontEndElbsV1(mockElb, clusterName)

	//then
	assert.Equal(t, "cluster-frontend", frontends[elbInternalScheme].Name)
	assert.Equal(t, elbDNSName, frontends[elbInternalScheme].DNSName)
	assert.Equal(t, canonicalHostedZoneNameID, frontends[elbInternalScheme].HostedZoneID)
	assert.Equal(t, elbInternalScheme, frontends[elbInternalScheme].Scheme)
}

func TestAttachWithInternalAndInternetFacing(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	e.(*elb).expectedNumber = 2
	instanceID := "cow"
	privateFrontend := "cluster-frontend"
	publicFrontend := "cluster-frontend2"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancers(mockElb,
		lb{name: privateFrontend, scheme: elbInternalScheme},
		lb{name: publicFrontend, scheme: elbInternetFacingScheme})
	mockClusterTags(mockElb,
		lbTags{name: privateFrontend, tags: defaultTags},
		lbTags{name: publicFrontend, tags: defaultTags},
	)
	mockRegisterInstances(mockElb, privateFrontend, instanceID)
	mockRegisterInstances(mockElb, publicFrontend, instanceID)

	//when
	err := e.Start()
	_ = e.Update(controller.IngressEntries{})

	//then
	mockElb.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, err)
}

func TestErrorGettingMetadata(t *testing.T) {
	e, _, mockMetadata := setup()
	mockMetadata.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{}, fmt.Errorf("no metadata for you"))

	err := e.Update(controller.IngressEntries{})

	assert.EqualError(t, err, "unable to query ec2 metadata service for InstanceId: no metadata for you")
}

func TestErrorDescribingInstances(t *testing.T) {
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockElb.On("DescribeLoadBalancers", mock.AnythingOfType("*elb.DescribeLoadBalancersInput")).Return(&awselb.DescribeLoadBalancersOutput{}, errors.New("oh dear oh dear"))

	_ = e.Start()
	err := e.Update(controller.IngressEntries{})

	assert.EqualError(t, err, "unable to describe load balancers: oh dear oh dear")
}

func TestErrorDescribingTags(t *testing.T) {
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancers(mockElb, lb{name: "one"})
	mockElb.On("DescribeTags", mock.AnythingOfType("*elb.DescribeTagsInput")).Return(&awselb.DescribeTagsOutput{}, errors.New("oh dear oh dear"))

	_ = e.Start()
	err := e.Update(controller.IngressEntries{})

	assert.EqualError(t, err, "unable to describe tags: oh dear oh dear")
}

func TestNoMatchingElbs(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerName := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancers(mockElb, lb{name: loadBalancerName, scheme: elbInternalScheme})
	// No cluster tags
	mockClusterTags(mockElb, lbTags{name: loadBalancerName, tags: []*awselb.Tag{}})

	// when
	_ = e.Start()
	err := e.Update(controller.IngressEntries{})

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestAttachingWithoutIngressNameTagElbs(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerName := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancers(mockElb, lb{name: loadBalancerName, scheme: elbInternalScheme})
	// No cluster tags
	mockClusterTags(mockElb, lbTags{name: loadBalancerName, tags: []*awselb.Tag{
		{Key: aws.String(frontendTag), Value: aws.String(clusterName)},
	}})

	// when
	_ = e.Start()
	err := e.Update(controller.IngressEntries{})

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestAttachingWithoutFrontendTagElbs(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerName := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancers(mockElb, lb{name: loadBalancerName, scheme: elbInternalScheme})
	// No cluster tags
	mockClusterTags(mockElb, lbTags{name: loadBalancerName, tags: []*awselb.Tag{
		{Key: aws.String(ingressNameTag), Value: aws.String(ingressName)},
	}})

	// when
	_ = e.Start()
	err := e.Update(controller.IngressEntries{})

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestGetLoadBalancerPages(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerName := "lb1"
	mockElb.On("DescribeLoadBalancers", &awselb.DescribeLoadBalancersInput{}).Return(&awselb.DescribeLoadBalancersOutput{NextMarker: aws.String("Use me")}, nil)
	mockElb.On("DescribeLoadBalancers", &awselb.DescribeLoadBalancersInput{Marker: aws.String("Use me")}).Return(&awselb.DescribeLoadBalancersOutput{
		LoadBalancerDescriptions: []*awselb.LoadBalancerDescription{{
			LoadBalancerName:          aws.String(loadBalancerName),
			DNSName:                   aws.String(elbDNSName),
			CanonicalHostedZoneNameID: aws.String(canonicalHostedZoneNameID),
		}},
	}, nil)
	mockInstanceMetadata(mockMetadata, instanceID)
	mockClusterTags(mockElb, lbTags{name: loadBalancerName, tags: defaultTags})
	mockRegisterInstances(mockElb, loadBalancerName, instanceID)

	// when
	err := e.Update(controller.IngressEntries{})

	// then
	assert.NoError(t, err)
	mockElb.AssertExpectations(t)
}

func TestTagCallsPage(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	e.(*elb).expectedNumber = 2
	instanceID := "cow"
	loadBalancerName1 := "lb1"
	loadBalancerName2 := "lb2"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancers(mockElb,
		lb{name: loadBalancerName1, scheme: elbInternalScheme},
		lb{name: loadBalancerName2, scheme: elbInternetFacingScheme})
	mockClusterTags(mockElb,
		lbTags{name: loadBalancerName1, tags: defaultTags},
		lbTags{name: loadBalancerName2, tags: defaultTags})
	mockRegisterInstances(mockElb, loadBalancerName1, instanceID)
	mockRegisterInstances(mockElb, loadBalancerName2, instanceID)

	// when
	err := e.Update(controller.IngressEntries{})

	// then
	assert.NoError(t, err)
	mockElb.AssertExpectations(t)
}

func TestDeregistersWithAttachedELBs(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	e.(*elb).expectedNumber = 2
	e.(*elb).drainDelay = time.Millisecond * 100

	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEnd2 := "cluster-frontend2"
	mockLoadBalancers(mockElb,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme},
		lb{name: clusterFrontEnd2, scheme: elbInternetFacingScheme},
		lb{name: "other", scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: defaultTags},
		lbTags{name: clusterFrontEnd2, tags: defaultTags},
		lbTags{name: "other elb", tags: []*awselb.Tag{{Key: aws.String("Banana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterInstances(mockElb, clusterFrontEnd, instanceID)
	mockRegisterInstances(mockElb, clusterFrontEnd2, instanceID)

	mockElb.On("DeregisterInstancesFromLoadBalancer", &awselb.DeregisterInstancesFromLoadBalancerInput{
		Instances:        []*awselb.Instance{{InstanceId: aws.String(instanceID)}},
		LoadBalancerName: aws.String(clusterFrontEnd),
	}).Return(&awselb.DeregisterInstancesFromLoadBalancerOutput{
		Instances: []*awselb.Instance{{InstanceId: aws.String(instanceID)}},
	}, nil)
	mockElb.On("DeregisterInstancesFromLoadBalancer", &awselb.DeregisterInstancesFromLoadBalancerInput{
		Instances:        []*awselb.Instance{{InstanceId: aws.String(instanceID)}},
		LoadBalancerName: aws.String(clusterFrontEnd2),
	}).Return(&awselb.DeregisterInstancesFromLoadBalancerOutput{
		Instances: []*awselb.Instance{{InstanceId: aws.String(instanceID)}},
	}, nil)

	//when
	assert.NoError(t, e.Start())
	assert.NoError(t, e.Update(controller.IngressEntries{}))
	beforeStop := time.Now()
	assert.NoError(t, e.Stop())
	stopDuration := time.Now().Sub(beforeStop)

	//then
	mockElb.AssertExpectations(t)
	assert.True(t, stopDuration.Nanoseconds() > time.Millisecond.Nanoseconds()*50,
		"Drain time should have caused stop to take at least 50ms.")
}

func TestRegisterInstanceError(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancers(mockElb, lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: defaultTags},
	)
	mockElb.On("RegisterInstancesWithLoadBalancer", mock.Anything).Return(&awselb.RegisterInstancesWithLoadBalancerOutput{}, errors.New("no register for you"))

	// when
	err := e.Update(controller.IngressEntries{})

	// then
	assert.EqualError(t, err, "unable to register instance cow with elb cluster-frontend: no register for you")
}

func TestDeRegisterInstanceError(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancers(mockElb,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: defaultTags},
	)
	mockRegisterInstances(mockElb, clusterFrontEnd, instanceID)
	mockElb.On("DeregisterInstancesFromLoadBalancer", mock.Anything).Return(&awselb.DeregisterInstancesFromLoadBalancerOutput{}, errors.New("no deregister for you"))

	// when
	_ = e.Start()
	_ = e.Update(controller.IngressEntries{})
	err := e.Stop()

	// then
	assert.EqualError(t, err, "at least one ELB failed to detach")
}

func TestRetriesUpdateIfFirstAttemptFails(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancers(mockElb,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{
			name: clusterFrontEnd,
			tags: defaultTags})
	mockElb.On("RegisterInstancesWithLoadBalancer", mock.Anything).Return(
		&awselb.RegisterInstancesWithLoadBalancerOutput{}, errors.New("no register for you"))

	// when
	_ = e.Start()
	firstErr := e.Update(controller.IngressEntries{})
	secondErr := e.Update(controller.IngressEntries{})

	// then
	assert.Error(t, firstErr)
	assert.Error(t, secondErr)
}

func TestHealthReportsHealthyBeforeFirstUpdate(t *testing.T) {
	// given
	e, _, _ := setup()

	// when
	err := e.Start()

	// then
	assert.NoError(t, err)
	assert.Nil(t, e.Health())
}

func TestHealthReportsUnhealthyAfterUnsuccessfulFirstUpdate(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	e.(*elb).expectedNumber = 2

	// and
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancers(mockElb,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: defaultTags})
	mockRegisterInstances(mockElb, clusterFrontEnd, instanceID)

	// when
	err := e.Start()
	updateErr := e.Update(controller.IngressEntries{})

	// then
	assert.NoError(t, err)
	assert.Error(t, updateErr)
	assert.Error(t, e.Health())
}
