package elb

import (
	"errors"
	"fmt"
	"testing"
	"time"

	awselbv2 "github.com/aws/aws-sdk-go/service/elbv2"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	awselbv1 "github.com/aws/aws-sdk-go/service/elb"
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
	ingressClass              = "ingress_name"
	region                    = "eu-west-1"
	frontendTag               = "sky.uk/KubernetesClusterFrontend"
	ingressClassTag           = "sky.uk/KubernetesClusterIngressClass"
	canonicalHostedZoneNameID = "test-id"
	elbDNSName                = "elb-dnsname"
	elbInternalScheme         = "internal"
	elbInternetFacingScheme   = "internet-facing"
)

var defaultTagsV1 = []*awselbv1.Tag{
	{Key: aws.String(frontendTag), Value: aws.String(clusterName)},
	{Key: aws.String(ingressClassTag), Value: aws.String(ingressClass)},
}

var defaultTagsV2 = []*awselbv2.Tag{
	{Key: aws.String(frontendTag), Value: aws.String(clusterName)},
	{Key: aws.String(ingressClassTag), Value: aws.String(ingressClass)},
}

type fakeElbV1 struct {
	mock.Mock
}

func (m *fakeElbV1) DescribeLoadBalancers(input *awselbv1.DescribeLoadBalancersInput) (*awselbv1.DescribeLoadBalancersOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselbv1.DescribeLoadBalancersOutput), args.Error(1)
}

func (m *fakeElbV1) DescribeTags(input *awselbv1.DescribeTagsInput) (*awselbv1.DescribeTagsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselbv1.DescribeTagsOutput), args.Error(1)
}

func (m *fakeElbV1) DeregisterInstancesFromLoadBalancer(input *awselbv1.DeregisterInstancesFromLoadBalancerInput) (*awselbv1.DeregisterInstancesFromLoadBalancerOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselbv1.DeregisterInstancesFromLoadBalancerOutput), args.Error(1)
}

func (m *fakeElbV1) RegisterInstancesWithLoadBalancer(input *awselbv1.RegisterInstancesWithLoadBalancerInput) (*awselbv1.RegisterInstancesWithLoadBalancerOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselbv1.RegisterInstancesWithLoadBalancerOutput), args.Error(1)
}

type fakeElbV2 struct {
	mock.Mock
}

func (m *fakeElbV2) DescribeLoadBalancers(input *awselbv2.DescribeLoadBalancersInput) (*awselbv2.DescribeLoadBalancersOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselbv2.DescribeLoadBalancersOutput), args.Error(1)
}

func (m *fakeElbV2) DescribeTargetGroups(input *awselbv2.DescribeTargetGroupsInput) (*awselbv2.DescribeTargetGroupsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselbv2.DescribeTargetGroupsOutput), args.Error(1)
}

func (m *fakeElbV2) DescribeTags(input *awselbv2.DescribeTagsInput) (*awselbv2.DescribeTagsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselbv2.DescribeTagsOutput), args.Error(1)
}

func (m *fakeElbV2) RegisterTargets(input *awselbv2.RegisterTargetsInput) (*awselbv2.RegisterTargetsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselbv2.RegisterTargetsOutput), args.Error(1)
}

func (m *fakeElbV2) DeregisterTargets(input *awselbv2.DeregisterTargetsInput) (*awselbv2.DeregisterTargetsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselbv2.DeregisterTargetsOutput), args.Error(1)
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

func mockLoadBalancersV1(m *fakeElbV1, lbs ...lb) {
	var descriptions []*awselbv1.LoadBalancerDescription
	for _, lb := range lbs {
		descriptions = append(descriptions, &awselbv1.LoadBalancerDescription{
			LoadBalancerName:          aws.String(lb.name),
			CanonicalHostedZoneNameID: aws.String(canonicalHostedZoneNameID),
			Scheme:                    aws.String(lb.scheme),
			DNSName:                   aws.String(elbDNSName),
		})

	}
	m.On("DescribeLoadBalancers", mock.AnythingOfType("*elb.DescribeLoadBalancersInput")).
		Return(&awselbv1.DescribeLoadBalancersOutput{
			LoadBalancerDescriptions: descriptions,
		}, nil)
}

func mockLoadBalancersV2(m *fakeElbV2, lbs ...lb) {
	var loadBalancers []*awselbv2.LoadBalancer
	for _, lb := range lbs {
		loadBalancers = append(loadBalancers, &awselbv2.LoadBalancer{
			LoadBalancerArn:       aws.String(lb.name),
			CanonicalHostedZoneId: aws.String(canonicalHostedZoneNameID),
			Scheme:                aws.String(lb.scheme),
			DNSName:               aws.String(elbDNSName),
		})

	}
	m.On("DescribeLoadBalancers", mock.AnythingOfType("*elbv2.DescribeLoadBalancersInput")).
		Return(&awselbv2.DescribeLoadBalancersOutput{
			LoadBalancers: loadBalancers,
		}, nil)
}

type tg struct {
	arn string
}

func mockDescribeTargetGroupsV2(m *fakeElbV2, tgs ...tg) {
	var targetGroups []*awselbv2.TargetGroup
	for _, tg := range tgs {
		targetGroups = append(targetGroups, &awselbv2.TargetGroup{
			TargetGroupArn: aws.String(tg.arn),
		})

	}
	m.On("DescribeTargetGroups", mock.AnythingOfType("*elbv2.DescribeTargetGroupsInput")).Return(&awselbv2.DescribeTargetGroupsOutput{
		TargetGroups: targetGroups,
	}, nil)
}

type lbTagsV1 struct {
	tags []*awselbv1.Tag
	name string
}

func mockClusterTagsV1(m *fakeElbV1, lbs ...lbTagsV1) {
	var tagDescriptions []*awselbv1.TagDescription

	for _, lb := range lbs {
		tagDescriptions = append(tagDescriptions, &awselbv1.TagDescription{
			LoadBalancerName: aws.String(lb.name),
			Tags:             lb.tags,
		})
	}

	m.On("DescribeTags", mock.AnythingOfType("*elb.DescribeTagsInput")).Return(&awselbv1.DescribeTagsOutput{
		TagDescriptions: tagDescriptions,
	}, nil)
}

type lbTagsV2 struct {
	tags []*awselbv2.Tag
	name string
}

func mockClusterTagsV2(m *fakeElbV2, lbs ...lbTagsV2) {
	var tagDescriptions []*awselbv2.TagDescription

	for _, lb := range lbs {
		tagDescriptions = append(tagDescriptions, &awselbv2.TagDescription{
			ResourceArn: aws.String(lb.name),
			Tags:        lb.tags,
		})
	}

	m.On("DescribeTags", mock.AnythingOfType("*elbv2.DescribeTagsInput")).Return(&awselbv2.DescribeTagsOutput{
		TagDescriptions: tagDescriptions,
	}, nil)
}

func mockRegisterInstancesV1(mockElb *fakeElbV1, elbName, instanceID string) {
	mockElb.On("RegisterInstancesWithLoadBalancer", &awselbv1.RegisterInstancesWithLoadBalancerInput{
		LoadBalancerName: aws.String(elbName),
		Instances:        []*awselbv1.Instance{{InstanceId: aws.String(instanceID)}},
	}).Return(&awselbv1.RegisterInstancesWithLoadBalancerOutput{
		Instances: []*awselbv1.Instance{{InstanceId: aws.String(instanceID)}},
	}, nil)
}

func mockRegisterTargetsV2(mockElb *fakeElbV2, targetGroupArn, instanceID string) {
	mockElb.On("RegisterTargets", &awselbv2.RegisterTargetsInput{
		TargetGroupArn: aws.String(targetGroupArn),
		Targets:        []*awselbv2.TargetDescription{{Id: aws.String(instanceID)}},
	}).Return(&awselbv2.RegisterTargetsOutput{}, nil)
}

func mockInstanceMetadata(mockMd *fakeMetadata, instanceID string) {
	mockMd.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{InstanceID: instanceID}, nil)
}

func setup() (controller.Updater, controller.Updater, *fakeElbV1, *fakeElbV2, *fakeMetadata) {
	mockMetadata := &fakeMetadata{}

	mockElbV1 := &fakeElbV1{}
	elbUpdaterV1, _ := New(Classic, region, clusterName, ingressClass, 1, 0)
	elbUpdaterV1.(*elb).awsElbV1 = mockElbV1
	elbUpdaterV1.(*elb).metadata = mockMetadata

	mockElbV2 := &fakeElbV2{}
	elbUpdaterV2, _ := New(Standard, region, clusterName, ingressClass, 1, 0)
	elbUpdaterV2.(*elb).awsElbV2 = mockElbV2
	elbUpdaterV2.(*elb).metadata = mockMetadata

	return elbUpdaterV1, elbUpdaterV2, mockElbV1, mockElbV2, mockMetadata
}

func TestCannotCreateUpdaterWithoutFrontEndTagValue(t *testing.T) {
	//when
	_, err := New(Classic, region, "", ingressClass, 1, 0)

	//then
	assert.Error(t, err)
}

func TestCannotCreateUpdaterWithoutIngressClassTagValue(t *testing.T) {
	//when
	_, err := New(Classic, region, clusterName, "", 1, 0)

	//then
	assert.Error(t, err)
}

func TestAttachWithSingleMatchingLoadBalancerV1(t *testing.T) {
	// given
	elbUpdaterV1, _, mockElbV1, _, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndDifferentCluster := "cluster-frontend-different-cluster"
	mockLoadBalancersV1(mockElbV1,
		lb{clusterFrontEnd, elbInternalScheme},
		lb{clusterFrontEndDifferentCluster, elbInternalScheme},
		lb{"other", elbInternalScheme})

	mockClusterTagsV1(mockElbV1,
		lbTagsV1{name: clusterFrontEnd, tags: defaultTagsV1},
		lbTagsV1{name: clusterFrontEndDifferentCluster, tags: []*awselbv1.Tag{
			{Key: aws.String(frontendTag), Value: aws.String("different cluster")},
			{Key: aws.String(ingressClass), Value: aws.String("different cluster")},
		}},
		lbTagsV1{name: "other elb", tags: []*awselbv1.Tag{{Key: aws.String("Banana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterInstancesV1(mockElbV1, clusterFrontEnd, instanceID)
	err := elbUpdaterV1.Start()

	//when
	_ = elbUpdaterV1.Update(controller.IngressEntries{})

	//then
	assert.NoError(t, elbUpdaterV1.Health())
	mockElbV1.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, err)
}

func TestAttachWithSingleMatchingLoadBalancerV2(t *testing.T) {
	// given
	_, elbUpdaterV2, _, mockElbV2, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndTargetGroup := "cluster-frontend-tg"
	clusterFrontEndDifferentCluster := "cluster-frontend-different-cluster"
	lbs := []lb{
		{clusterFrontEnd, elbInternalScheme},
		{clusterFrontEndDifferentCluster, elbInternalScheme},
		{"other", elbInternalScheme},
	}
	mockLoadBalancersV2(mockElbV2, lbs...)
	mockDescribeTargetGroupsV2(mockElbV2, tg{clusterFrontEndTargetGroup})

	mockClusterTagsV2(mockElbV2,
		lbTagsV2{name: clusterFrontEnd, tags: defaultTagsV2},
		lbTagsV2{name: clusterFrontEndDifferentCluster, tags: []*awselbv2.Tag{
			{Key: aws.String(frontendTag), Value: aws.String("different cluster")},
			{Key: aws.String(ingressClass), Value: aws.String("different cluster")},
		}},
		lbTagsV2{name: "other elb", tags: []*awselbv2.Tag{{Key: aws.String("Banana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterTargetsV2(mockElbV2, clusterFrontEndTargetGroup, instanceID)
	err := elbUpdaterV2.Start()

	//when
	_ = elbUpdaterV2.Update(controller.IngressEntries{})

	//then
	assert.NoError(t, elbUpdaterV2.Health())
	mockElbV2.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, err)
}

func TestReportsErrorIfExpectedNotMatchedV1(t *testing.T) {
	// given
	elbUpdaterV1, _, mockElbV1, _, mockMetadata := setup()
	elbUpdaterV1.(*elb).expectedNumber = 2
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndDifferentCluster := "cluster-frontend-different-cluster"
	mockLoadBalancersV1(mockElbV1,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme},
		lb{name: clusterFrontEndDifferentCluster, scheme: elbInternalScheme},
		lb{name: "other", scheme: elbInternalScheme})
	mockClusterTagsV1(mockElbV1,
		lbTagsV1{name: clusterFrontEnd, tags: defaultTagsV1},
		lbTagsV1{name: clusterFrontEndDifferentCluster, tags: []*awselbv1.Tag{
			{Key: aws.String(frontendTag), Value: aws.String("different cluster")},
			{Key: aws.String(ingressClassTag), Value: aws.String("different cluster")},
		}},
		lbTagsV1{name: "other elb", tags: []*awselbv1.Tag{{Key: aws.String("Banana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterInstancesV1(mockElbV1, clusterFrontEnd, instanceID)

	//when
	_ = elbUpdaterV1.Start()
	err := elbUpdaterV1.Update(controller.IngressEntries{})

	//then
	assert.EqualError(t, err, "expected ELBs: 2 actual: 1")
}

func TestReportsErrorIfExpectedNotMatchedV2(t *testing.T) {
	// given
	_, elbUpdaterV2, _, mockElbV2, mockMetadata := setup()
	elbUpdaterV2.(*elb).expectedNumber = 2
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndTargetGroup := "cluster-frontend-tg"
	clusterFrontEndDifferentCluster := "cluster-frontend-different-cluster"
	mockLoadBalancersV2(mockElbV2,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme},
		lb{name: clusterFrontEndDifferentCluster, scheme: elbInternalScheme},
		lb{name: "other", scheme: elbInternalScheme})
	mockDescribeTargetGroupsV2(mockElbV2, tg{clusterFrontEndTargetGroup})
	mockClusterTagsV2(mockElbV2,
		lbTagsV2{name: clusterFrontEnd, tags: defaultTagsV2},
		lbTagsV2{name: clusterFrontEndDifferentCluster, tags: []*awselbv2.Tag{
			{Key: aws.String(frontendTag), Value: aws.String("different cluster")},
			{Key: aws.String(ingressClassTag), Value: aws.String("different cluster")},
		}},
		lbTagsV2{name: "other elb", tags: []*awselbv2.Tag{{Key: aws.String("Banana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterTargetsV2(mockElbV2, clusterFrontEndTargetGroup, instanceID)

	//when
	_ = elbUpdaterV2.Start()
	err := elbUpdaterV2.Update(controller.IngressEntries{})

	//then
	assert.EqualError(t, err, "expected ELBs: 2 actual: 1")
}

func TestNameAndDNSNameAndHostedZoneIDLoadBalancerDetailsAreExtractedV1(t *testing.T) {
	//given
	mockElb := &fakeElbV1{}
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancersV1(mockElb, lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTagsV1(mockElb,
		lbTagsV1{name: clusterFrontEnd, tags: defaultTagsV1},
	)

	//when
	frontends, _ := FindFrontEndElbsWithIngressClassName(Classic, mockElb, nil, clusterName, ingressClass)

	//then
	assert.Equal(t, "cluster-frontend", frontends[elbInternalScheme].Name)
	assert.Equal(t, elbDNSName, frontends[elbInternalScheme].DNSName)
	assert.Equal(t, canonicalHostedZoneNameID, frontends[elbInternalScheme].HostedZoneID)
	assert.Equal(t, elbInternalScheme, frontends[elbInternalScheme].Scheme)
}

func TestNameAndDNSNameAndHostedZoneIDLoadBalancerDetailsAreExtractedV2(t *testing.T) {
	//given
	mockElb := &fakeElbV2{}
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndTargetGroup := "cluster-frontend-tg"
	mockLoadBalancersV2(mockElb, lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockDescribeTargetGroupsV2(mockElb, tg{clusterFrontEndTargetGroup})
	mockClusterTagsV2(mockElb,
		lbTagsV2{name: clusterFrontEnd, tags: defaultTagsV2},
	)

	//when
	frontends, _ := FindFrontEndElbsWithIngressClassName(Standard, nil, mockElb, clusterName, ingressClass)

	//then
	assert.Equal(t, "cluster-frontend", frontends[elbInternalScheme].Name)
	assert.Equal(t, elbDNSName, frontends[elbInternalScheme].DNSName)
	assert.Equal(t, canonicalHostedZoneNameID, frontends[elbInternalScheme].HostedZoneID)
	assert.Equal(t, elbInternalScheme, frontends[elbInternalScheme].Scheme)
}

func TestFindElbWithoutIngressClassV1(t *testing.T) {
	//given
	mockElb := &fakeElbV1{}
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancersV1(mockElb, lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTagsV1(mockElb,
		lbTagsV1{name: clusterFrontEnd, tags: []*awselbv1.Tag{
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

func TestAttachWithInternalAndInternetFacingV1(t *testing.T) {
	// given
	elbUpdaterV1, _, mockElbV1, _, mockMetadata := setup()
	elbUpdaterV1.(*elb).expectedNumber = 2
	instanceID := "cow"
	privateFrontend := "cluster-frontend"
	publicFrontend := "cluster-frontend2"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancersV1(mockElbV1,
		lb{name: privateFrontend, scheme: elbInternalScheme},
		lb{name: publicFrontend, scheme: elbInternetFacingScheme})
	mockClusterTagsV1(mockElbV1,
		lbTagsV1{name: privateFrontend, tags: defaultTagsV1},
		lbTagsV1{name: publicFrontend, tags: defaultTagsV1},
	)
	mockRegisterInstancesV1(mockElbV1, privateFrontend, instanceID)
	mockRegisterInstancesV1(mockElbV1, publicFrontend, instanceID)

	//when
	err := elbUpdaterV1.Start()
	_ = elbUpdaterV1.Update(controller.IngressEntries{})

	//then
	mockElbV1.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, err)
}

func TestAttachWithInternalAndInternetFacingV2(t *testing.T) {
	// given
	_, elbUpdaterV2, _, mockElbV2, mockMetadata := setup()
	elbUpdaterV2.(*elb).expectedNumber = 2
	instanceID := "cow"
	privateFrontend := "cluster-frontend"
	privateFrontendTargetGroup := "cluster-frontend-tg"
	publicFrontend := "cluster-frontend2"
	publicFrontendTargetGroup := "cluster-frontend2-tg"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancersV2(mockElbV2,
		lb{name: privateFrontend, scheme: elbInternalScheme},
		lb{name: publicFrontend, scheme: elbInternetFacingScheme})
	mockDescribeTargetGroupsV2(mockElbV2, tg{privateFrontendTargetGroup}, tg{publicFrontendTargetGroup})
	mockClusterTagsV2(mockElbV2,
		lbTagsV2{name: privateFrontend, tags: defaultTagsV2},
		lbTagsV2{name: publicFrontend, tags: defaultTagsV2},
	)
	mockRegisterTargetsV2(mockElbV2, privateFrontendTargetGroup, instanceID)
	mockRegisterTargetsV2(mockElbV2, publicFrontendTargetGroup, instanceID)

	//when
	err := elbUpdaterV2.Start()
	_ = elbUpdaterV2.Update(controller.IngressEntries{})

	//then
	mockElbV2.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, err)
}

func TestErrorGettingMetadata(t *testing.T) {
	elbUpdaterV1, _, _, _, mockMetadata := setup()
	mockMetadata.
		On("GetInstanceIdentityDocument").
		Return(ec2metadata.EC2InstanceIdentityDocument{}, fmt.Errorf("no metadata for you"))

	err := elbUpdaterV1.Update(controller.IngressEntries{})

	assert.EqualError(t, err, "unable to query ec2 metadata service for InstanceId: no metadata for you")
}

func TestErrorDescribingInstancesV1(t *testing.T) {
	elbUpdaterV1, _, mockElbV1, _, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockElbV1.
		On("DescribeLoadBalancers", mock.AnythingOfType("*elb.DescribeLoadBalancersInput")).
		Return(&awselbv1.DescribeLoadBalancersOutput{}, errors.New("oh dear oh dear"))

	_ = elbUpdaterV1.Start()
	err := elbUpdaterV1.Update(controller.IngressEntries{})

	assert.EqualError(t, err, "unable to describe load balancers: oh dear oh dear")
}

func TestErrorDescribingInstancesV2(t *testing.T) {
	_, elbUpdaterV2, _, mockElbV2, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockElbV2.
		On("DescribeLoadBalancers", mock.AnythingOfType("*elbv2.DescribeLoadBalancersInput")).
		Return(&awselbv2.DescribeLoadBalancersOutput{}, errors.New("oh dear oh dear"))

	_ = elbUpdaterV2.Start()
	err := elbUpdaterV2.Update(controller.IngressEntries{})

	assert.EqualError(t, err, "unable to describe load balancers: oh dear oh dear")
}

func TestErrorDescribingTagsV1(t *testing.T) {
	elbUpdaterV1, _, mockElbV1, _, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancersV1(mockElbV1, lb{name: "one"})
	mockElbV1.On("DescribeTags", mock.AnythingOfType("*elb.DescribeTagsInput")).Return(&awselbv1.DescribeTagsOutput{}, errors.New("oh dear oh dear"))

	_ = elbUpdaterV1.Start()
	err := elbUpdaterV1.Update(controller.IngressEntries{})

	assert.EqualError(t, err, "unable to describe tags: oh dear oh dear")
}

func TestErrorDescribingTagsV2(t *testing.T) {
	_, elbUpdaterV2, _, mockElbV2, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancersV2(mockElbV2, lb{name: "one"})
	mockDescribeTargetGroupsV2(mockElbV2, tg{"some-target-group-arn"})
	mockElbV2.
		On("DescribeTags", mock.AnythingOfType("*elbv2.DescribeTagsInput")).
		Return(&awselbv2.DescribeTagsOutput{}, errors.New("oh dear oh dear"))

	_ = elbUpdaterV2.Start()
	err := elbUpdaterV2.Update(controller.IngressEntries{})

	assert.EqualError(t, err, "unable to describe tags: oh dear oh dear")
}

func TestNoMatchingElbsV1(t *testing.T) {
	// given
	elbUpdaterV1, _, mockElbV1, _, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerName := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancersV1(mockElbV1, lb{name: loadBalancerName, scheme: elbInternalScheme})
	// No cluster tags
	mockClusterTagsV1(mockElbV1, lbTagsV1{name: loadBalancerName, tags: []*awselbv1.Tag{}})

	// when
	_ = elbUpdaterV1.Start()
	err := elbUpdaterV1.Update(controller.IngressEntries{})

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestNoMatchingElbsV2(t *testing.T) {
	// given
	_, elbUpdaterV2, _, mockElbV2, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerArn := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancersV2(mockElbV2, lb{name: loadBalancerArn, scheme: elbInternalScheme})
	mockDescribeTargetGroupsV2(mockElbV2, tg{"some-target-group-arn"})
	// No cluster tags
	mockClusterTagsV2(mockElbV2, lbTagsV2{name: loadBalancerArn, tags: []*awselbv2.Tag{}})

	// when
	_ = elbUpdaterV2.Start()
	err := elbUpdaterV2.Update(controller.IngressEntries{})

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestAttachingWithoutIngressClassTagElbsV1(t *testing.T) {
	// given
	elbUpdaterV1, _, mockElbV1, _, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerName := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancersV1(mockElbV1, lb{name: loadBalancerName, scheme: elbInternalScheme})
	// No cluster tags
	mockClusterTagsV1(mockElbV1, lbTagsV1{name: loadBalancerName, tags: []*awselbv1.Tag{
		{Key: aws.String(frontendTag), Value: aws.String(clusterName)},
	}})

	// when
	_ = elbUpdaterV1.Start()
	err := elbUpdaterV1.Update(controller.IngressEntries{})

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestAttachingWithoutIngressClassTagElbsV2(t *testing.T) {
	// given
	_, elbUpdaterV2, _, mockElbV2, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerArn := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancersV2(mockElbV2, lb{name: loadBalancerArn, scheme: elbInternalScheme})
	mockDescribeTargetGroupsV2(mockElbV2, tg{"some-target-group-arn"})
	// No cluster tags
	mockClusterTagsV2(mockElbV2, lbTagsV2{name: loadBalancerArn, tags: []*awselbv2.Tag{
		{Key: aws.String(frontendTag), Value: aws.String(clusterName)},
	}})

	// when
	_ = elbUpdaterV2.Start()
	err := elbUpdaterV2.Update(controller.IngressEntries{})

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestAttachingWithoutFrontendTagElbsV1(t *testing.T) {
	// given
	elbUpdaterV1, _, mockElbV1, _, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerName := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancersV1(mockElbV1, lb{name: loadBalancerName, scheme: elbInternalScheme})
	// No cluster tags
	mockClusterTagsV1(mockElbV1, lbTagsV1{name: loadBalancerName, tags: []*awselbv1.Tag{
		{Key: aws.String(ingressClassTag), Value: aws.String(ingressClass)},
	}})

	// when
	_ = elbUpdaterV1.Start()
	err := elbUpdaterV1.Update(controller.IngressEntries{})

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestAttachingWithoutFrontendTagElbsV2(t *testing.T) {
	// given
	_, elbUpdaterV2, _, mockElbV2, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerArn := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancersV2(mockElbV2, lb{name: loadBalancerArn, scheme: elbInternalScheme})
	mockDescribeTargetGroupsV2(mockElbV2, tg{"some-target-group-arn"})
	// No cluster tags
	mockClusterTagsV2(mockElbV2, lbTagsV2{name: loadBalancerArn, tags: []*awselbv2.Tag{
		{Key: aws.String(ingressClassTag), Value: aws.String(ingressClass)},
	}})

	// when
	_ = elbUpdaterV2.Start()
	err := elbUpdaterV2.Update(controller.IngressEntries{})

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestGetLoadBalancerPagesV1(t *testing.T) {
	// given
	elbUpdaterV1, _, mockElbV1, _, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerName := "lb1"
	mockElbV1.
		On("DescribeLoadBalancers", &awselbv1.DescribeLoadBalancersInput{}).
		Return(&awselbv1.DescribeLoadBalancersOutput{NextMarker: aws.String("Use me")}, nil)
	mockElbV1.
		On("DescribeLoadBalancers", &awselbv1.DescribeLoadBalancersInput{Marker: aws.String("Use me")}).
		Return(&awselbv1.DescribeLoadBalancersOutput{
			LoadBalancerDescriptions: []*awselbv1.LoadBalancerDescription{{
				LoadBalancerName:          aws.String(loadBalancerName),
				DNSName:                   aws.String(elbDNSName),
				CanonicalHostedZoneNameID: aws.String(canonicalHostedZoneNameID),
			}},
		}, nil)
	mockInstanceMetadata(mockMetadata, instanceID)
	mockClusterTagsV1(mockElbV1, lbTagsV1{name: loadBalancerName, tags: defaultTagsV1})
	mockRegisterInstancesV1(mockElbV1, loadBalancerName, instanceID)

	// when
	err := elbUpdaterV1.Update(controller.IngressEntries{})

	// then
	assert.NoError(t, err)
	mockElbV1.AssertExpectations(t)
}

func TestGetLoadBalancerPagesV2(t *testing.T) {
	// given
	_, elbUpdaterV2, _, mockElbV2, mockMetadata := setup()
	instanceID := "cow"
	loadBalancerArn := "lb1"
	loadBalancerTargetGroupArn := "lb1-tg"
	mockElbV2.
		On("DescribeLoadBalancers", &awselbv2.DescribeLoadBalancersInput{}).
		Return(&awselbv2.DescribeLoadBalancersOutput{NextMarker: aws.String("Use me")}, nil)
	mockElbV2.
		On("DescribeLoadBalancers", &awselbv2.DescribeLoadBalancersInput{Marker: aws.String("Use me")}).
		Return(&awselbv2.DescribeLoadBalancersOutput{
			LoadBalancers: []*awselbv2.LoadBalancer{{
				LoadBalancerArn:       aws.String(loadBalancerArn),
				DNSName:               aws.String(elbDNSName),
				CanonicalHostedZoneId: aws.String(canonicalHostedZoneNameID),
			}},
		}, nil)
	mockDescribeTargetGroupsV2(mockElbV2, tg{loadBalancerTargetGroupArn})
	mockInstanceMetadata(mockMetadata, instanceID)
	mockClusterTagsV2(mockElbV2, lbTagsV2{name: loadBalancerArn, tags: defaultTagsV2})
	mockRegisterTargetsV2(mockElbV2, loadBalancerTargetGroupArn, instanceID)

	// when
	err := elbUpdaterV2.Update(controller.IngressEntries{})

	// then
	assert.NoError(t, err)
	mockElbV2.AssertExpectations(t)
}

func TestTagCallsPageV1(t *testing.T) {
	// given
	elbUpdaterV1, _, mockElbV1, _, mockMetadata := setup()
	elbUpdaterV1.(*elb).expectedNumber = 2
	instanceID := "cow"
	loadBalancerNamelbUpdaterV1 := "lb1"
	loadBalancerName2 := "lb2"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancersV1(mockElbV1,
		lb{name: loadBalancerNamelbUpdaterV1, scheme: elbInternalScheme},
		lb{name: loadBalancerName2, scheme: elbInternetFacingScheme})
	mockClusterTagsV1(mockElbV1,
		lbTagsV1{name: loadBalancerNamelbUpdaterV1, tags: defaultTagsV1},
		lbTagsV1{name: loadBalancerName2, tags: defaultTagsV1})
	mockRegisterInstancesV1(mockElbV1, loadBalancerNamelbUpdaterV1, instanceID)
	mockRegisterInstancesV1(mockElbV1, loadBalancerName2, instanceID)

	// when
	err := elbUpdaterV1.Update(controller.IngressEntries{})

	// then
	assert.NoError(t, err)
	mockElbV1.AssertExpectations(t)
}

func TestTagCallsPageV2(t *testing.T) {
	// given
	_, elbUpdaterV2, _, mockElbV2, mockMetadata := setup()
	elbUpdaterV2.(*elb).expectedNumber = 2
	instanceID := "cow"
	loadBalancerArn := "lb1"
	loadBalancerTargetGroupArn := "lb1-tg"
	loadBalancer2Arn := "lb2"
	loadBalancer2TargetGroupArn := "lb2-tg"
	mockInstanceMetadata(mockMetadata, instanceID)
	mockLoadBalancersV2(mockElbV2,
		lb{name: loadBalancerArn, scheme: elbInternalScheme},
		lb{name: loadBalancer2Arn, scheme: elbInternetFacingScheme})
	mockDescribeTargetGroupsV2(mockElbV2, tg{loadBalancerTargetGroupArn}, tg{loadBalancer2TargetGroupArn})
	mockClusterTagsV2(mockElbV2,
		lbTagsV2{name: loadBalancerArn, tags: defaultTagsV2},
		lbTagsV2{name: loadBalancer2Arn, tags: defaultTagsV2})
	mockRegisterTargetsV2(mockElbV2, loadBalancerTargetGroupArn, instanceID)
	mockRegisterTargetsV2(mockElbV2, loadBalancer2TargetGroupArn, instanceID)

	// when
	err := elbUpdaterV2.Update(controller.IngressEntries{})

	// then
	assert.NoError(t, err)
	mockElbV2.AssertExpectations(t)
}

func TestDeregistersWithAttachedELBsV1(t *testing.T) {
	// given
	elbUpdaterV1, _, mockElbV1, _, mockMetadata := setup()
	elbUpdaterV1.(*elb).expectedNumber = 2
	elbUpdaterV1.(*elb).drainDelay = time.Millisecond * 100

	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEnd2 := "cluster-frontend2"
	mockLoadBalancersV1(mockElbV1,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme},
		lb{name: clusterFrontEnd2, scheme: elbInternetFacingScheme},
		lb{name: "other", scheme: elbInternalScheme})
	mockClusterTagsV1(mockElbV1,
		lbTagsV1{name: clusterFrontEnd, tags: defaultTagsV1},
		lbTagsV1{name: clusterFrontEnd2, tags: defaultTagsV1},
		lbTagsV1{name: "other elb", tags: []*awselbv1.Tag{{Key: aws.String("Banana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterInstancesV1(mockElbV1, clusterFrontEnd, instanceID)
	mockRegisterInstancesV1(mockElbV1, clusterFrontEnd2, instanceID)

	mockElbV1.On("DeregisterInstancesFromLoadBalancer", &awselbv1.DeregisterInstancesFromLoadBalancerInput{
		Instances:        []*awselbv1.Instance{{InstanceId: aws.String(instanceID)}},
		LoadBalancerName: aws.String(clusterFrontEnd),
	}).Return(&awselbv1.DeregisterInstancesFromLoadBalancerOutput{
		Instances: []*awselbv1.Instance{{InstanceId: aws.String(instanceID)}},
	}, nil)
	mockElbV1.On("DeregisterInstancesFromLoadBalancer", &awselbv1.DeregisterInstancesFromLoadBalancerInput{
		Instances:        []*awselbv1.Instance{{InstanceId: aws.String(instanceID)}},
		LoadBalancerName: aws.String(clusterFrontEnd2),
	}).Return(&awselbv1.DeregisterInstancesFromLoadBalancerOutput{
		Instances: []*awselbv1.Instance{{InstanceId: aws.String(instanceID)}},
	}, nil)

	//when
	assert.NoError(t, elbUpdaterV1.Start())
	assert.NoError(t, elbUpdaterV1.Update(controller.IngressEntries{}))
	beforeStop := time.Now()
	assert.NoError(t, elbUpdaterV1.Stop())
	stopDuration := time.Now().Sub(beforeStop)

	//then
	mockElbV1.AssertExpectations(t)
	assert.True(t, stopDuration.Nanoseconds() > time.Millisecond.Nanoseconds()*50,
		"Drain time should have caused stop to take at least 50ms.")
}

func TestDeregistersWithAttachedELBsV2(t *testing.T) {
	// given
	_, elbUpdaterV2, _, mockElbV2, mockMetadata := setup()
	elbUpdaterV2.(*elb).expectedNumber = 2
	elbUpdaterV2.(*elb).drainDelay = time.Millisecond * 100

	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndTargetGroupArn := "cluster-frontend-tg"
	clusterFrontEnd2 := "cluster-frontend2"
	clusterFrontEnd2TargetGroupArn := "cluster-frontend2-tg"
	mockLoadBalancersV2(mockElbV2,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme},
		lb{name: clusterFrontEnd2, scheme: elbInternetFacingScheme},
		lb{name: "other", scheme: elbInternalScheme})
	mockDescribeTargetGroupsV2(mockElbV2, tg{clusterFrontEndTargetGroupArn}, tg{clusterFrontEnd2TargetGroupArn})
	mockClusterTagsV2(mockElbV2,
		lbTagsV2{name: clusterFrontEnd, tags: defaultTagsV2},
		lbTagsV2{name: clusterFrontEnd2, tags: defaultTagsV2},
		lbTagsV2{name: "other elb", tags: []*awselbv2.Tag{{Key: aws.String("Banana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterTargetsV2(mockElbV2, clusterFrontEndTargetGroupArn, instanceID)
	mockRegisterTargetsV2(mockElbV2, clusterFrontEnd2TargetGroupArn, instanceID)

	mockElbV2.On("DeregisterTargets", &awselbv2.DeregisterTargetsInput{
		Targets:        []*awselbv2.TargetDescription{{Id: aws.String(instanceID)}},
		TargetGroupArn: aws.String(clusterFrontEnd),
	}).Return(&awselbv2.DeregisterTargetsOutput{}, nil)
	mockElbV2.On("DeregisterTargets", &awselbv2.DeregisterTargetsInput{
		Targets:        []*awselbv2.TargetDescription{{Id: aws.String(instanceID)}},
		TargetGroupArn: aws.String(clusterFrontEnd2),
	}).Return(&awselbv2.DeregisterTargetsOutput{}, nil)

	//when
	assert.NoError(t, elbUpdaterV2.Start())
	assert.NoError(t, elbUpdaterV2.Update(controller.IngressEntries{}))
	beforeStop := time.Now()
	assert.NoError(t, elbUpdaterV2.Stop())
	stopDuration := time.Now().Sub(beforeStop)

	//then
	mockElbV2.AssertExpectations(t)
	assert.True(t, stopDuration.Nanoseconds() > time.Millisecond.Nanoseconds()*50,
		"Drain time should have caused stop to take at least 50ms.")
}

func TestRegisterInstanceErrorV1(t *testing.T) {
	// given
	elbUpdaterV1, _, mockElbV1, _, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancersV1(mockElbV1, lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTagsV1(mockElbV1,
		lbTagsV1{name: clusterFrontEnd, tags: defaultTagsV1},
	)
	mockElbV1.On("RegisterInstancesWithLoadBalancer", mock.Anything).Return(&awselbv1.RegisterInstancesWithLoadBalancerOutput{}, errors.New("no register for you"))

	// when
	err := elbUpdaterV1.Update(controller.IngressEntries{})

	// then
	assert.EqualError(t, err, "unable to register instance cow with elb cluster-frontend: no register for you")
}

func TestRegisterInstanceErrorV2(t *testing.T) {
	// given
	_, elbUpdaterV2, _, mockElbV2, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndTargetGroup := "cluster-frontend-tg"
	mockLoadBalancersV2(mockElbV2, lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockDescribeTargetGroupsV2(mockElbV2, tg{clusterFrontEndTargetGroup})
	mockClusterTagsV2(mockElbV2,
		lbTagsV2{name: clusterFrontEnd, tags: defaultTagsV2},
	)
	mockElbV2.On("RegisterTargets", mock.Anything).
		Return(&awselbv2.RegisterTargetsOutput{}, errors.New("no register for you"))

	// when
	err := elbUpdaterV2.Update(controller.IngressEntries{})

	// then
	assert.EqualError(t, err, fmt.Sprintf("unable to register instance cow with elb cluster-frontend: "+
		"could not register Target Group(s) with Instance cow: [%s]", clusterFrontEndTargetGroup))
}

func TestDeRegisterInstanceErrorV1(t *testing.T) {
	// given
	elbUpdaterV1, _, mockElbV1, _, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancersV1(mockElbV1,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTagsV1(mockElbV1,
		lbTagsV1{name: clusterFrontEnd, tags: defaultTagsV1},
	)
	mockRegisterInstancesV1(mockElbV1, clusterFrontEnd, instanceID)
	mockElbV1.On("DeregisterInstancesFromLoadBalancer", mock.Anything).Return(&awselbv1.DeregisterInstancesFromLoadBalancerOutput{}, errors.New("no deregister for you"))

	// when
	_ = elbUpdaterV1.Start()
	_ = elbUpdaterV1.Update(controller.IngressEntries{})
	err := elbUpdaterV1.Stop()

	// then
	assert.EqualError(t, err, "at least one ELB failed to detach")
}

func TestDeRegisterInstanceErrorV2(t *testing.T) {
	// given
	_, elbUpdaterV2, _, mockElbV2, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndTargetGroup := "cluster-frontend-tg"
	mockLoadBalancersV2(mockElbV2,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockDescribeTargetGroupsV2(mockElbV2, tg{clusterFrontEndTargetGroup})
	mockClusterTagsV2(mockElbV2,
		lbTagsV2{name: clusterFrontEnd, tags: defaultTagsV2},
	)
	mockRegisterTargetsV2(mockElbV2, clusterFrontEndTargetGroup, instanceID)
	mockElbV2.On("DeregisterTargets", mock.Anything).
		Return(&awselbv2.DeregisterTargetsOutput{}, errors.New("no deregister for you"))

	// when
	_ = elbUpdaterV2.Start()
	_ = elbUpdaterV2.Update(controller.IngressEntries{})
	err := elbUpdaterV2.Stop()

	// then
	assert.EqualError(t, err, "at least one ELB failed to detach")
}

func TestRetriesUpdateIfFirstAttemptFailsV1(t *testing.T) {
	// given
	elbUpdaterV1, _, mockElbV1, _, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancersV1(mockElbV1,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTagsV1(mockElbV1,
		lbTagsV1{
			name: clusterFrontEnd,
			tags: defaultTagsV1})
	mockElbV1.On("RegisterInstancesWithLoadBalancer", mock.Anything).Return(
		&awselbv1.RegisterInstancesWithLoadBalancerOutput{}, errors.New("no register for you"))

	// when
	_ = elbUpdaterV1.Start()
	firstErr := elbUpdaterV1.Update(controller.IngressEntries{})
	secondErr := elbUpdaterV1.Update(controller.IngressEntries{})

	// then
	assert.Error(t, firstErr)
	assert.Error(t, secondErr)
}

func TestRetriesUpdateIfFirstAttemptFailsV2(t *testing.T) {
	// given
	_, elbUpdaterV2, _, mockElbV2, mockMetadata := setup()
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancersV2(mockElbV2,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockDescribeTargetGroupsV2(mockElbV2, tg{"some-target-group-arn"})
	mockClusterTagsV2(mockElbV2,
		lbTagsV2{
			name: clusterFrontEnd,
			tags: defaultTagsV2})
	mockElbV2.On("RegisterTargets", mock.Anything).Return(
		&awselbv2.RegisterTargetsOutput{}, errors.New("no register for you"))

	// when
	_ = elbUpdaterV2.Start()
	firstErr := elbUpdaterV2.Update(controller.IngressEntries{})
	secondErr := elbUpdaterV2.Update(controller.IngressEntries{})

	// then
	assert.Error(t, firstErr)
	assert.Error(t, secondErr)
}

func TestHealthReportsHealthyBeforeFirstUpdate(t *testing.T) {
	// given
	elbUpdaterV1, _, _, _, _ := setup()

	// when
	err := elbUpdaterV1.Start()

	// then
	assert.NoError(t, err)
	assert.Nil(t, elbUpdaterV1.Health())
}

func TestHealthReportsUnhealthyAfterUnsuccessfulFirstUpdateV1(t *testing.T) {
	// given
	elbUpdaterV1, _, mockElbV1, _, mockMetadata := setup()
	elbUpdaterV1.(*elb).expectedNumber = 2

	// and
	instanceID := "cow"
	mockInstanceMetadata(mockMetadata, instanceID)
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancersV1(mockElbV1,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockClusterTagsV1(mockElbV1,
		lbTagsV1{name: clusterFrontEnd, tags: defaultTagsV1})
	mockRegisterInstancesV1(mockElbV1, clusterFrontEnd, instanceID)

	// when
	err := elbUpdaterV1.Start()
	updateErr := elbUpdaterV1.Update(controller.IngressEntries{})

	// then
	assert.NoError(t, err)
	assert.Error(t, updateErr)
	assert.Error(t, elbUpdaterV1.Health())
}
