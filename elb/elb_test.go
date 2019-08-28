package elb

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	awselb "github.com/aws/aws-sdk-go/service/elbv2"
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

func (m *fakeElb) DeregisterTargets(input *awselb.DeregisterTargetsInput) (*awselb.DeregisterTargetsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselb.DeregisterTargetsOutput), args.Error(1)
}

func (m *fakeElb) RegisterTargets(input *awselb.RegisterTargetsInput) (*awselb.RegisterTargetsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselb.RegisterTargetsOutput), args.Error(1)
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
	arn    string
	scheme string
}

func mockLoadBalancers(m *fakeElb, lbs ...lb) {
	var loadBalancers []*awselb.LoadBalancer
	for _, lb := range lbs {
		loadBalancers = append(loadBalancers, &awselb.LoadBalancer{
			LoadBalancerArn:       aws.String(lb.arn),
			CanonicalHostedZoneId: aws.String(canonicalHostedZoneNameID),
			Scheme:                aws.String(lb.scheme),
			DNSName:               aws.String(elbDNSName),
		})

	}
	m.On("DescribeLoadBalancers", mock.AnythingOfType("*elbv2.DescribeLoadBalancersInput")).Return(&awselb.DescribeLoadBalancersOutput{
		LoadBalancers: loadBalancers,
	}, nil)
}

type lbTags struct {
	tags []*awselb.Tag
	arn  string
}

func mockClusterTags(m *fakeElb, lbs ...lbTags) {
	var tagDescriptions []*awselb.TagDescription

	for _, lb := range lbs {
		tagDescriptions = append(tagDescriptions, &awselb.TagDescription{
			ResourceArn: aws.String(lb.arn),
			Tags:        lb.tags,
		})
	}

	m.On("DescribeTags", mock.AnythingOfType("*elbv2.DescribeTagsInput")).Return(&awselb.DescribeTagsOutput{
		TagDescriptions: tagDescriptions,
	}, nil)
}

func mockRegisterTargets(mockElb *fakeElb, elbName, instanceID string) {
	mockElb.On("RegisterTargets", &awselb.RegisterTargetsInput{
		TargetGroupArn: aws.String(elbName),
		Targets:        []*awselb.TargetDescription{{Id: aws.String(instanceID)}},
	}).Return(&awselb.RegisterTargetsOutput{}, nil)
}

func mockInstanceMetadata(mockMd *fakeMetadata, instanceID string) {
	mockMd.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{InstanceID: instanceID}, nil)
}

func setup() (controller.Updater, *fakeElb, *fakeMetadata) {
	e, _ := New(region, clusterName, ingressName, 1, 0)
	mockElb := &fakeElb{}
	mockMetadata := &fakeMetadata{}
	e.(*elb).awsElb = mockElb
	e.(*elb).metadata = mockMetadata
	return e, mockElb, mockMetadata
}

func TestCanNotCreateUpdaterWithoutFrontEndTagValue(t *testing.T) {
	//when
	_, err := New(region, "", ingressName, 1, 0)

	//then
	assert.Error(t, err)
}

func TestCanNotCreateUpdaterWithoutIngressNameTagValue(t *testing.T) {
	//when
	_, err := New(region, clusterName, "", 1, 0)

	//then
	assert.Error(t, err)
}

func TestAttachWithSingleMatchingLoadBalancer(t *testing.T) {
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
		lbTags{arn: clusterFrontEnd, tags: defaultTags},
		lbTags{arn: clusterFrontEndDifferentCluster, tags: []*awselb.Tag{
			{Key: aws.String(frontendTag), Value: aws.String("different cluster")},
			{Key: aws.String(ingressName), Value: aws.String("different cluster")},
		}},
		lbTags{arn: "other elb", tags: []*awselb.Tag{{Key: aws.String("Bannana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterTargets(mockElb, clusterFrontEnd, instanceID)
	err := e.Start()

	//when
	e.Update(controller.IngressEntries{})

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
		lb{arn: clusterFrontEnd, scheme: elbInternalScheme},
		lb{arn: clusterFrontEndDifferentCluster, scheme: elbInternalScheme},
		lb{arn: "other", scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{arn: clusterFrontEnd, tags: defaultTags},
		lbTags{arn: clusterFrontEndDifferentCluster, tags: []*awselb.Tag{
			{Key: aws.String(frontendTag), Value: aws.String("different cluster")},
			{Key: aws.String(ingressNameTag), Value: aws.String("different cluster")},
		}},
		lbTags{arn: "other elb", tags: []*awselb.Tag{{Key: aws.String("Bannana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterTargets(mockElb, clusterFrontEnd, instanceID)

	//when
	e.Start()
	err := e.Update(controller.IngressEntries{})

	//then
	assert.EqualError(t, err, "expected ELBs: 2 actual: 1")
}

func TestNameAndDNSNameAndHostedZoneIDLoadBalancerDetailsAreExtracted(t *testing.T) {
	//given
	mockElb := &fakeElb{}
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancers(mockElb, lb{arn: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{arn: clusterFrontEnd, tags: defaultTags},
	)

	//when
	frontends, _ := FindFrontEndElbsWithIngressClassName(mockElb, clusterName, ingressName)

	//then
	assert.Equal(t, "cluster-frontend", frontends[elbInternalScheme].Arn)
	assert.Equal(t, elbDNSName, frontends[elbInternalScheme].DNSName)
	assert.Equal(t, canonicalHostedZoneNameID, frontends[elbInternalScheme].HostedZoneID)
	assert.Equal(t, elbInternalScheme, frontends[elbInternalScheme].Scheme)
}

func TestFindElbWithoutIngressName(t *testing.T) {
	//given
	mockElb := &fakeElb{}
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancers(mockElb, lb{arn: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{arn: clusterFrontEnd, tags: []*awselb.Tag{
			{Key: aws.String(frontendTag), Value: aws.String(clusterName)},
		}},
	)

	//when
	frontends, _ := FindFrontEndElbs(mockElb, clusterName)

	//then
	assert.Equal(t, "cluster-frontend", frontends[elbInternalScheme].Arn)
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
		lb{arn: privateFrontend, scheme: elbInternalScheme},
		lb{arn: publicFrontend, scheme: elbInternetFacingScheme})
	mockClusterTags(mockElb,
		lbTags{arn: privateFrontend, tags: defaultTags},
		lbTags{arn: publicFrontend, tags: defaultTags},
	)
	mockRegisterTargets(mockElb, privateFrontend, instanceID)
	mockRegisterTargets(mockElb, publicFrontend, instanceID)

	//when
	err := e.Start()
	e.Update(controller.IngressEntries{})

	//then
	mockElb.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, err)
}

func TestErrorGettingMetadata(t *testing.T) {
	e, _, mockMetadata := setup()
	mockMetadata.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{}, fmt.Errorf("No metadata for you"))

	err := e.Update(controller.IngressEntries{})

	assert.EqualError(t, err, "unable to query ec2 metadata service for InstanceId: No metadata for you")
}

func TestErrorDescribingLoadBalancers(t *testing.T) {
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockElb.On("DescribeLoadBalancers", mock.AnythingOfType("*elbv2.DescribeLoadBalancersInput")).Return(&awselb.DescribeLoadBalancersOutput{}, errors.New("oh dear oh dear"))

	e.Start()
	err := e.Update(controller.IngressEntries{})

	assert.EqualError(t, err, "unable to describe load balancers: oh dear oh dear")
}

func TestErrorDescribingTags(t *testing.T) {
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancers(mockElb, lb{arn: "one"})
	mockElb.On("DescribeTags", mock.AnythingOfType("*elbv2.DescribeTagsInput")).Return(&awselb.DescribeTagsOutput{}, errors.New("oh dear oh dear"))

	e.Start()
	err := e.Update(controller.IngressEntries{})

	assert.EqualError(t, err, "unable to describe tags: oh dear oh dear")
}

func TestNoMatchingElbs(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerArn := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancers(mockElb, lb{arn: loadBalancerArn, scheme: elbInternalScheme})
	// No cluster tags
	mockClusterTags(mockElb, lbTags{arn: loadBalancerArn, tags: []*awselb.Tag{}})

	// when
	e.Start()
	err := e.Update(controller.IngressEntries{})

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestAttachingWithoutIngressNameTagElbs(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerArn := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancers(mockElb, lb{arn: loadBalancerArn, scheme: elbInternalScheme})
	// No cluster tags
	mockClusterTags(mockElb, lbTags{arn: loadBalancerArn, tags: []*awselb.Tag{
		{Key: aws.String(frontendTag), Value: aws.String(clusterName)},
	}})

	// when
	e.Start()
	err := e.Update(controller.IngressEntries{})

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestAttachingWithoutFrontendTagElbs(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerArn := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancers(mockElb, lb{arn: loadBalancerArn, scheme: elbInternalScheme})
	// No cluster tags
	mockClusterTags(mockElb, lbTags{arn: loadBalancerArn, tags: []*awselb.Tag{
		{Key: aws.String(ingressNameTag), Value: aws.String(ingressName)},
	}})

	// when
	e.Start()
	err := e.Update(controller.IngressEntries{})

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestGetLoadBalancerPages(t *testing.T) {
	// given
	e, mockElb, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerArn := "lb1"
	mockElb.On("DescribeLoadBalancers", &awselb.DescribeLoadBalancersInput{}).Return(&awselb.DescribeLoadBalancersOutput{NextMarker: aws.String("Use me")}, nil)
	mockElb.On("DescribeLoadBalancers", &awselb.DescribeLoadBalancersInput{Marker: aws.String("Use me")}).Return(&awselb.DescribeLoadBalancersOutput{
		LoadBalancers: []*awselb.LoadBalancer{{
			LoadBalancerArn:       aws.String(loadBalancerArn),
			DNSName:               aws.String(elbDNSName),
			CanonicalHostedZoneId: aws.String(canonicalHostedZoneNameID),
		}},
	}, nil)
	mockInstanceMetadata(mockMetadata, instanceID)
	mockClusterTags(mockElb, lbTags{arn: loadBalancerArn, tags: defaultTags})
	mockRegisterTargets(mockElb, loadBalancerArn, instanceID)

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
	loadBalancerArn1 := "lb1"
	loadBalancerArn2 := "lb2"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancers(mockElb,
		lb{arn: loadBalancerArn1, scheme: elbInternalScheme},
		lb{arn: loadBalancerArn2, scheme: elbInternetFacingScheme})
	mockClusterTags(mockElb,
		lbTags{arn: loadBalancerArn1, tags: defaultTags},
		lbTags{arn: loadBalancerArn2, tags: defaultTags})
	mockRegisterTargets(mockElb, loadBalancerArn1, instanceID)
	mockRegisterTargets(mockElb, loadBalancerArn2, instanceID)

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
		lb{arn: clusterFrontEnd, scheme: elbInternalScheme},
		lb{arn: clusterFrontEnd2, scheme: elbInternetFacingScheme},
		lb{arn: "other", scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{arn: clusterFrontEnd, tags: defaultTags},
		lbTags{arn: clusterFrontEnd2, tags: defaultTags},
		lbTags{arn: "other elb", tags: []*awselb.Tag{{Key: aws.String("Bannana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterTargets(mockElb, clusterFrontEnd, instanceID)
	mockRegisterTargets(mockElb, clusterFrontEnd2, instanceID)

	mockElb.On("DeregisterTargets", &awselb.DeregisterTargetsInput{
		Targets:        []*awselb.TargetDescription{{Id: aws.String(instanceID)}},
		TargetGroupArn: aws.String(clusterFrontEnd),
	}).Return(&awselb.DeregisterTargetsOutput{}, nil)
	mockElb.On("DeregisterTargets", &awselb.DeregisterTargetsInput{
		Targets:        []*awselb.TargetDescription{{Id: aws.String(instanceID)}},
		TargetGroupArn: aws.String(clusterFrontEnd2),
	}).Return(&awselb.DeregisterTargetsOutput{}, nil)

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
	mockLoadBalancers(mockElb, lb{arn: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{arn: clusterFrontEnd, tags: defaultTags},
	)
	mockElb.On("RegisterTargets", mock.Anything).Return(&awselb.RegisterTargetsOutput{}, errors.New("no register for you"))

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
		lb{arn: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{arn: clusterFrontEnd, tags: defaultTags},
	)
	mockRegisterTargets(mockElb, clusterFrontEnd, instanceID)
	mockElb.On("DeregisterTargets", mock.Anything).Return(&awselb.DeregisterTargetsOutput{}, errors.New("no deregister for you"))

	// when
	e.Start()
	e.Update(controller.IngressEntries{})
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
		lb{arn: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{
			arn:  clusterFrontEnd,
			tags: defaultTags})
	mockElb.On("RegisterTargets", mock.Anything).Return(
		&awselb.RegisterTargetsOutput{}, errors.New("no register for you"))

	// when
	e.Start()
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
		lb{arn: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTags(mockElb,
		lbTags{arn: clusterFrontEnd, tags: defaultTags})
	mockRegisterTargets(mockElb, clusterFrontEnd, instanceID)

	// when
	err := e.Start()
	updateErr := e.Update(controller.IngressEntries{})

	// then
	assert.NoError(t, err)
	assert.Error(t, updateErr)
	assert.Error(t, e.Health())
}
