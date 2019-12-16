package nlb

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/service/elbv2"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
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
	elbDNSName                = "nlb-dnsname"
	elbInternalScheme         = "internal"
	elbInternetFacingScheme   = "internet-facing"
)

var defaultTags = []*elbv2.Tag{
	{Key: aws.String(frontendTag), Value: aws.String(clusterName)},
	{Key: aws.String(ingressClassTag), Value: aws.String(ingressClass)},
}

type fakeElb struct {
	mock.Mock
}

func (m *fakeElb) DescribeLoadBalancers(input *elbv2.DescribeLoadBalancersInput) (*elbv2.DescribeLoadBalancersOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*elbv2.DescribeLoadBalancersOutput), args.Error(1)
}

func (m *fakeElb) DescribeTargetGroups(input *elbv2.DescribeTargetGroupsInput) (*elbv2.DescribeTargetGroupsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*elbv2.DescribeTargetGroupsOutput), args.Error(1)
}

func (m *fakeElb) DescribeTags(input *elbv2.DescribeTagsInput) (*elbv2.DescribeTagsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*elbv2.DescribeTagsOutput), args.Error(1)
}

func (m *fakeElb) RegisterTargets(input *elbv2.RegisterTargetsInput) (*elbv2.RegisterTargetsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*elbv2.RegisterTargetsOutput), args.Error(1)
}

func (m *fakeElb) DeregisterTargets(input *elbv2.DeregisterTargetsInput) (*elbv2.DeregisterTargetsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*elbv2.DeregisterTargetsOutput), args.Error(1)
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
	var loadBalancers []*elbv2.LoadBalancer
	for _, lb := range lbs {
		loadBalancers = append(loadBalancers, &elbv2.LoadBalancer{
			LoadBalancerArn:       aws.String(lb.name),
			CanonicalHostedZoneId: aws.String(canonicalHostedZoneNameID),
			Scheme:                aws.String(lb.scheme),
			DNSName:               aws.String(elbDNSName),
		})

	}
	m.On("DescribeLoadBalancers", mock.AnythingOfType("*elbv2.DescribeLoadBalancersInput")).
		Return(&elbv2.DescribeLoadBalancersOutput{
			LoadBalancers: loadBalancers,
		}, nil)
}

type tg struct {
	arn        string
	targetType string
}

func mockDescribeTargetGroups(m *fakeElb, tgs ...tg) {
	var targetGroups []*elbv2.TargetGroup
	for _, tg := range tgs {
		targetGroups = append(targetGroups, &elbv2.TargetGroup{
			TargetGroupArn: aws.String(tg.arn),
			TargetType:     aws.String(tg.targetType),
		})
	}
	m.On("DescribeTargetGroups", mock.AnythingOfType("*elbv2.DescribeTargetGroupsInput")).Return(&elbv2.DescribeTargetGroupsOutput{
		TargetGroups: targetGroups,
	}, nil)
}

type lbTags struct {
	tags []*elbv2.Tag
	name string
}

func mockClusterTags(m *fakeElb, lbs ...lbTags) {
	var tagDescriptions []*elbv2.TagDescription

	for _, lb := range lbs {
		tagDescriptions = append(tagDescriptions, &elbv2.TagDescription{
			ResourceArn: aws.String(lb.name),
			Tags:        lb.tags,
		})
	}

	m.On("DescribeTags", mock.AnythingOfType("*elbv2.DescribeTagsInput")).Return(&elbv2.DescribeTagsOutput{
		TagDescriptions: tagDescriptions,
	}, nil)
}

func mockRegisterTargets(mockElb *fakeElb, targetGroupArn, instanceID string, privateIP string, targetType string) {
	targetDescription, _ := generateTargetDescriptionFromTargetType(targetType, instanceID, privateIP)
	mockElb.On("RegisterTargets", &elbv2.RegisterTargetsInput{
		TargetGroupArn: aws.String(targetGroupArn),
		Targets:        targetDescription,
	}).Return(&elbv2.RegisterTargetsOutput{}, nil)
}

func mockInstanceMetadata(mockMd *fakeMetadata, instanceID, privateIP string) {
	mockMd.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{InstanceID: instanceID, PrivateIP: privateIP}, nil)
}

func setup() (controller.Updater, *fakeElb, *fakeMetadata) {
	mockMetadata := &fakeMetadata{}

	mockElb := &fakeElb{}
	elbUpdater, _ := New(region, clusterName, ingressClass, 1, 0)
	elbUpdater.(*nlb).awsElb = mockElb
	elbUpdater.(*nlb).metadata = mockMetadata

	return elbUpdater, mockElb, mockMetadata
}

func TestMetricsRegisteredCorrectly(t *testing.T) {
	//when
	_, _ = New(region, clusterName, ingressClass, 1, 0)

	//then
	assert.Equal(t, "feed_ingress_frontends_attached", metricName(attachedFrontendGauge))
}

func metricName(c prometheus.Collector) string {
	descriptionCh := make(chan *prometheus.Desc, 1)
	c.Describe(descriptionCh)
	desc := <-descriptionCh
	descReflect := reflect.ValueOf(*desc)
	fqNameField := descReflect.FieldByName("fqName")
	return fqNameField.String()
}

func TestCannotCreateUpdaterWithoutFrontEndTagValue(t *testing.T) {
	//when
	_, err := New(region, "", ingressClass, 1, 0)

	//then
	assert.Error(t, err)
}

func TestCannotCreateUpdaterWithoutIngressClassTagValue(t *testing.T) {
	//when
	_, err := New(region, clusterName, "", 1, 0)

	//then
	assert.Error(t, err)
}

func TestAttachWithSingleMatchingLoadBalancer(t *testing.T) {
	// given
	elbUpdater, mockElb, mockMetadata := setup()
	instanceID := "cow"
	privateIP := "192.168.0.1"
	targetType := elbv2.TargetTypeEnumIp
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndTargetGroup := "cluster-frontend-tg"
	clusterFrontEndDifferentCluster := "cluster-frontend-different-cluster"
	lbs := []lb{
		{clusterFrontEnd, elbInternalScheme},
		{clusterFrontEndDifferentCluster, elbInternalScheme},
		{"other", elbInternalScheme},
	}
	mockLoadBalancers(mockElb, lbs...)
	mockDescribeTargetGroups(mockElb, tg{arn: clusterFrontEndTargetGroup, targetType: targetType})

	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: defaultTags},
		lbTags{name: clusterFrontEndDifferentCluster, tags: []*elbv2.Tag{
			{Key: aws.String(frontendTag), Value: aws.String("different cluster")},
			{Key: aws.String(ingressClass), Value: aws.String("different cluster")},
		}},
		lbTags{name: "other nlb", tags: []*elbv2.Tag{{Key: aws.String("Banana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterTargets(mockElb, clusterFrontEndTargetGroup, instanceID, privateIP, targetType)
	err := elbUpdater.Start()

	//when
	_ = elbUpdater.Update(controller.IngressEntries{})

	//then
	assert.NoError(t, elbUpdater.Health())
	mockElb.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)

	assert.NoError(t, err)
}

func TestReportsErrorIfExpectedNotMatched(t *testing.T) {
	// given
	elbUpdater, mockElb, mockMetadata := setup()
	elbUpdater.(*nlb).expectedNumber = 2
	instanceID := "cow"
	privateIP := "192.168.0.1"
	targetType := elbv2.TargetTypeEnumIp
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndTargetGroup := "cluster-frontend-tg"
	clusterFrontEndDifferentCluster := "cluster-frontend-different-cluster"
	mockLoadBalancers(mockElb,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme},
		lb{name: clusterFrontEndDifferentCluster, scheme: elbInternalScheme},
		lb{name: "other", scheme: elbInternalScheme})
	mockDescribeTargetGroups(mockElb, tg{arn: clusterFrontEndTargetGroup, targetType: targetType})
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: defaultTags},
		lbTags{name: clusterFrontEndDifferentCluster, tags: []*elbv2.Tag{
			{Key: aws.String(frontendTag), Value: aws.String("different cluster")},
			{Key: aws.String(ingressClassTag), Value: aws.String("different cluster")},
		}},
		lbTags{name: "other nlb", tags: []*elbv2.Tag{{Key: aws.String("Banana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterTargets(mockElb, clusterFrontEndTargetGroup, instanceID, privateIP, targetType)

	//when
	_ = elbUpdater.Start()
	err := elbUpdater.Update(controller.IngressEntries{})

	//then
	assert.EqualError(t, err, "expected NLBs: 2 actual: 1")
}

func TestNameAndDNSNameAndHostedZoneIDLoadBalancerDetailsAreExtracted(t *testing.T) {
	//given
	mockElb := &fakeElb{}
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndTargetGroup := "cluster-frontend-tg"
	mockLoadBalancers(mockElb, lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockDescribeTargetGroups(mockElb, tg{clusterFrontEndTargetGroup, elbv2.TargetTypeEnumIp})
	mockClusterTags(mockElb,
		lbTags{name: clusterFrontEnd, tags: defaultTags},
	)

	//when
	frontends, _ := FindFrontEndLoadBalancersWithIngressClassName(mockElb, clusterName, ingressClass)

	//then
	assert.Equal(t, "cluster-frontend", frontends[elbInternalScheme].Name)
	assert.Equal(t, elbDNSName, frontends[elbInternalScheme].DNSName)
	assert.Equal(t, canonicalHostedZoneNameID, frontends[elbInternalScheme].HostedZoneID)
	assert.Equal(t, elbInternalScheme, frontends[elbInternalScheme].Scheme)
}

func TestAttachWithInternalAndInternetFacing(t *testing.T) {
	// given
	elbUpdater, mockElb, mockMetadata := setup()
	elbUpdater.(*nlb).expectedNumber = 2
	instanceID := "cow"
	privateIP := "192.168.0.1"
	targetType := elbv2.TargetTypeEnumIp
	privateFrontend := "cluster-frontend"
	privateFrontendTargetGroup := "cluster-frontend-tg"
	publicFrontend := "cluster-frontend2"
	publicFrontendTargetGroup := "cluster-frontend2-tg"
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	mockLoadBalancers(mockElb,
		lb{name: privateFrontend, scheme: elbInternalScheme},
		lb{name: publicFrontend, scheme: elbInternetFacingScheme})
	mockDescribeTargetGroups(mockElb,
		tg{privateFrontendTargetGroup, targetType},
		tg{publicFrontendTargetGroup, targetType})
	mockClusterTags(mockElb,
		lbTags{name: privateFrontend, tags: defaultTags},
		lbTags{name: publicFrontend, tags: defaultTags},
	)
	mockRegisterTargets(mockElb, privateFrontendTargetGroup, instanceID, privateIP, targetType)
	mockRegisterTargets(mockElb, publicFrontendTargetGroup, instanceID, privateIP, targetType)

	//when
	err := elbUpdater.Start()
	_ = elbUpdater.Update(controller.IngressEntries{})

	//then
	mockElb.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, err)
}

func TestAttachTargetGroupsMultipleTypes(t *testing.T) {
	// given
	elbUpdater, mockElb, mockMetadata := setup()
	elbUpdater.(*nlb).expectedNumber = 2
	instanceID := "cow"
	privateIP := "192.168.0.1"
	privateFrontend := "cluster-frontend"
	privateFrontendTargetGroup := "cluster-frontend-tg"
	publicFrontend := "cluster-frontend2"
	publicFrontendTargetGroup := "cluster-frontend2-tg"
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	mockLoadBalancers(mockElb,
		lb{name: privateFrontend, scheme: elbInternalScheme},
		lb{name: publicFrontend, scheme: elbInternetFacingScheme})
	mockDescribeTargetGroups(mockElb,
		tg{privateFrontendTargetGroup, elbv2.TargetTypeEnumIp},
		tg{publicFrontendTargetGroup, elbv2.TargetTypeEnumInstance})
	mockClusterTags(mockElb,
		lbTags{name: privateFrontend, tags: defaultTags},
		lbTags{name: publicFrontend, tags: defaultTags},
	)
	mockRegisterTargets(mockElb, privateFrontendTargetGroup, instanceID, privateIP, elbv2.TargetTypeEnumIp)
	mockRegisterTargets(mockElb, publicFrontendTargetGroup, instanceID, privateIP, elbv2.TargetTypeEnumInstance)

	//when
	err := elbUpdater.Start()
	_ = elbUpdater.Update(controller.IngressEntries{})

	//then
	mockElb.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, err)
}

func TestErrorGettingMetadata(t *testing.T) {
	elbUpdater, _, mockMetadata := setup()
	mockMetadata.
		On("GetInstanceIdentityDocument").
		Return(ec2metadata.EC2InstanceIdentityDocument{}, fmt.Errorf("no metadata for you"))

	err := elbUpdater.Update(controller.IngressEntries{})

	assert.EqualError(t, err, "unable to query ec2 metadata service for InstanceId: no metadata for you")
}

func TestErrorDescribingInstances(t *testing.T) {
	elbUpdater, mockElb, mockMetadata := setup()
	instanceID := "cow"
	privateIP := "192.168.0.1"
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	mockElb.
		On("DescribeLoadBalancers", mock.AnythingOfType("*elbv2.DescribeLoadBalancersInput")).
		Return(&elbv2.DescribeLoadBalancersOutput{}, errors.New("oh dear oh dear"))

	_ = elbUpdater.Start()
	err := elbUpdater.Update(controller.IngressEntries{})

	assert.EqualError(t, err, "unable to describe load balancers: oh dear oh dear")
}

func TestErrorDescribingTags(t *testing.T) {
	elbUpdater, mockElb, mockMetadata := setup()
	instanceID := "cow"
	privateIP := "192.168.0.1"
	targetType := elbv2.TargetTypeEnumIp
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	mockLoadBalancers(mockElb, lb{name: "one"})
	mockDescribeTargetGroups(mockElb, tg{"some-target-group-arn", targetType})
	mockElb.
		On("DescribeTags", mock.AnythingOfType("*elbv2.DescribeTagsInput")).
		Return(&elbv2.DescribeTagsOutput{}, errors.New("oh dear oh dear"))

	_ = elbUpdater.Start()
	err := elbUpdater.Update(controller.IngressEntries{})

	assert.EqualError(t, err, "unable to describe tags: oh dear oh dear")
}

func TestNoMatchingElbs(t *testing.T) {
	// given
	elbUpdater, mockElb, mockMetadata := setup()
	instanceID := "cow"
	privateIP := "192.168.0.1"
	targetType := elbv2.TargetTypeEnumIp
	loadBalancerArn := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	mockLoadBalancers(mockElb, lb{name: loadBalancerArn, scheme: elbInternalScheme})
	mockDescribeTargetGroups(mockElb, tg{"some-target-group-arn", targetType})
	// No cluster tags
	mockClusterTags(mockElb, lbTags{name: loadBalancerArn, tags: []*elbv2.Tag{}})

	// when
	_ = elbUpdater.Start()
	err := elbUpdater.Update(controller.IngressEntries{})

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestAttachingWithoutIngressClassTagElbs(t *testing.T) {
	// given
	elbUpdater, mockElb, mockMetadata := setup()
	instanceID := "cow"
	privateIP := "192.168.0.1"
	targetType := elbv2.TargetTypeEnumIp
	loadBalancerArn := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	mockLoadBalancers(mockElb, lb{name: loadBalancerArn, scheme: elbInternalScheme})
	mockDescribeTargetGroups(mockElb, tg{"some-target-group-arn", targetType})
	// No cluster tags
	mockClusterTags(mockElb, lbTags{name: loadBalancerArn, tags: []*elbv2.Tag{
		{Key: aws.String(frontendTag), Value: aws.String(clusterName)},
	}})

	// when
	_ = elbUpdater.Start()
	err := elbUpdater.Update(controller.IngressEntries{})

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestAttachingWithoutFrontendTagElbs(t *testing.T) {
	// given
	elbUpdater, mockElb, mockMetadata := setup()
	instanceID := "cow"
	privateIP := "192.168.0.1"
	targetType := elbv2.TargetTypeEnumIp
	loadBalancerArn := "i am not the loadbalancer you are looking for"
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	mockLoadBalancers(mockElb, lb{name: loadBalancerArn, scheme: elbInternalScheme})
	mockDescribeTargetGroups(mockElb, tg{"some-target-group-arn", targetType})
	// No cluster tags
	mockClusterTags(mockElb, lbTags{name: loadBalancerArn, tags: []*elbv2.Tag{
		{Key: aws.String(ingressClassTag), Value: aws.String(ingressClass)},
	}})

	// when
	_ = elbUpdater.Start()
	err := elbUpdater.Update(controller.IngressEntries{})

	// then
	assert.Error(t, err, "expected ELBs: 1 actual: 0")
}

func TestGetLoadBalancerPages(t *testing.T) {
	// given
	elbUpdater, mockElb, mockMetadata := setup()
	instanceID := "cow"
	privateIP := "192.168.0.1"
	targetType := elbv2.TargetTypeEnumIp
	loadBalancerArn := "lb1"
	loadBalancerTargetGroupArn := "lb1-tg"
	mockElb.
		On("DescribeLoadBalancers", &elbv2.DescribeLoadBalancersInput{}).
		Return(&elbv2.DescribeLoadBalancersOutput{NextMarker: aws.String("Use me")}, nil)
	mockElb.
		On("DescribeLoadBalancers", &elbv2.DescribeLoadBalancersInput{Marker: aws.String("Use me")}).
		Return(&elbv2.DescribeLoadBalancersOutput{
			LoadBalancers: []*elbv2.LoadBalancer{{
				LoadBalancerArn:       aws.String(loadBalancerArn),
				DNSName:               aws.String(elbDNSName),
				CanonicalHostedZoneId: aws.String(canonicalHostedZoneNameID),
			}},
		}, nil)
	mockDescribeTargetGroups(mockElb, tg{loadBalancerTargetGroupArn, targetType})
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	mockClusterTags(mockElb, lbTags{name: loadBalancerArn, tags: defaultTags})
	mockRegisterTargets(mockElb, loadBalancerTargetGroupArn, instanceID, privateIP, targetType)

	// when
	err := elbUpdater.Update(controller.IngressEntries{})

	// then
	assert.NoError(t, err)
	mockElb.AssertExpectations(t)
}

func TestTagCallsPageV2(t *testing.T) {
	// given
	elbUpdaterV2, mockElbV2, mockMetadata := setup()
	elbUpdaterV2.(*nlb).expectedNumber = 2
	instanceID := "cow"
	privateIP := "192.168.0.1"
	targetType := elbv2.TargetTypeEnumIp
	loadBalancerArn := "lb1"
	loadBalancerTargetGroupArn := "lb1-tg"
	loadBalancer2Arn := "lb2"
	loadBalancer2TargetGroupArn := "lb2-tg"
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	mockLoadBalancers(mockElbV2,
		lb{name: loadBalancerArn, scheme: elbInternalScheme},
		lb{name: loadBalancer2Arn, scheme: elbInternetFacingScheme})
	mockDescribeTargetGroups(mockElbV2,
		tg{loadBalancerTargetGroupArn, targetType},
		tg{loadBalancer2TargetGroupArn, targetType})
	mockClusterTags(mockElbV2,
		lbTags{name: loadBalancerArn, tags: defaultTags},
		lbTags{name: loadBalancer2Arn, tags: defaultTags})
	mockRegisterTargets(mockElbV2, loadBalancerTargetGroupArn, instanceID, privateIP, targetType)
	mockRegisterTargets(mockElbV2, loadBalancer2TargetGroupArn, instanceID, privateIP, targetType)

	// when
	err := elbUpdaterV2.Update(controller.IngressEntries{})

	// then
	assert.NoError(t, err)
	mockElbV2.AssertExpectations(t)
}

func TestDeregistersWithAttachedELBsV2(t *testing.T) {
	// given
	elbUpdaterV2, mockElbV2, mockMetadata := setup()
	elbUpdaterV2.(*nlb).expectedNumber = 2
	elbUpdaterV2.(*nlb).drainDelay = time.Millisecond * 100
	instanceID := "cow"
	privateIP := "192.168.0.1"
	targetType := elbv2.TargetTypeEnumIp
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndTargetGroupArn := "cluster-frontend-tg"
	clusterFrontEnd2 := "cluster-frontend2"
	clusterFrontEnd2TargetGroupArn := "cluster-frontend2-tg"
	mockLoadBalancers(mockElbV2,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme},
		lb{name: clusterFrontEnd2, scheme: elbInternetFacingScheme},
		lb{name: "other", scheme: elbInternalScheme})
	mockDescribeTargetGroups(mockElbV2,
		tg{clusterFrontEndTargetGroupArn, targetType},
		tg{clusterFrontEnd2TargetGroupArn, targetType})
	mockClusterTags(mockElbV2,
		lbTags{name: clusterFrontEnd, tags: defaultTags},
		lbTags{name: clusterFrontEnd2, tags: defaultTags},
		lbTags{name: "other nlb", tags: []*elbv2.Tag{{Key: aws.String("Banana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterTargets(mockElbV2, clusterFrontEndTargetGroupArn, instanceID, privateIP, targetType)
	mockRegisterTargets(mockElbV2, clusterFrontEnd2TargetGroupArn, instanceID, privateIP, targetType)

	mockElbV2.On("DeregisterTargets", &elbv2.DeregisterTargetsInput{
		Targets:        []*elbv2.TargetDescription{{Id: aws.String(privateIP)}},
		TargetGroupArn: aws.String(clusterFrontEndTargetGroupArn),
	}).Return(&elbv2.DeregisterTargetsOutput{}, nil)
	mockElbV2.On("DeregisterTargets", &elbv2.DeregisterTargetsInput{
		Targets:        []*elbv2.TargetDescription{{Id: aws.String(privateIP)}},
		TargetGroupArn: aws.String(clusterFrontEnd2TargetGroupArn),
	}).Return(&elbv2.DeregisterTargetsOutput{}, nil)

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

func TestDeregistersWithAttachedTargetGroupsMultipleTypes(t *testing.T) {
	// given
	elbUpdaterV2, mockElbV2, mockMetadata := setup()
	elbUpdaterV2.(*nlb).expectedNumber = 2
	elbUpdaterV2.(*nlb).drainDelay = time.Millisecond * 100
	instanceID := "cow"
	privateIP := "192.168.0.1"
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndTargetGroupArn := "cluster-frontend-tg"
	clusterFrontEnd2 := "cluster-frontend2"
	clusterFrontEnd2TargetGroupArn := "cluster-frontend2-tg"
	mockLoadBalancers(mockElbV2,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme},
		lb{name: clusterFrontEnd2, scheme: elbInternetFacingScheme},
		lb{name: "other", scheme: elbInternalScheme})
	mockDescribeTargetGroups(mockElbV2,
		tg{clusterFrontEndTargetGroupArn, elbv2.TargetTypeEnumIp},
		tg{clusterFrontEnd2TargetGroupArn, elbv2.TargetTypeEnumInstance})
	mockClusterTags(mockElbV2,
		lbTags{name: clusterFrontEnd, tags: defaultTags},
		lbTags{name: clusterFrontEnd2, tags: defaultTags},
		lbTags{name: "other nlb", tags: []*elbv2.Tag{{Key: aws.String("Banana"), Value: aws.String("Tasty")}}},
	)
	mockRegisterTargets(mockElbV2, clusterFrontEndTargetGroupArn, instanceID, privateIP, elbv2.TargetTypeEnumIp)
	mockRegisterTargets(mockElbV2, clusterFrontEnd2TargetGroupArn, instanceID, privateIP, elbv2.TargetTypeEnumInstance)

	mockElbV2.On("DeregisterTargets", &elbv2.DeregisterTargetsInput{
		Targets:        []*elbv2.TargetDescription{{Id: aws.String(privateIP)}},
		TargetGroupArn: aws.String(clusterFrontEndTargetGroupArn),
	}).Return(&elbv2.DeregisterTargetsOutput{}, nil)
	mockElbV2.On("DeregisterTargets", &elbv2.DeregisterTargetsInput{
		Targets:        []*elbv2.TargetDescription{{Id: aws.String(instanceID)}},
		TargetGroupArn: aws.String(clusterFrontEnd2TargetGroupArn),
	}).Return(&elbv2.DeregisterTargetsOutput{}, nil)

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

func TestRegisterInstanceErrorV2(t *testing.T) {
	// given
	elbUpdaterV2, mockElbV2, mockMetadata := setup()
	instanceID := "cow"
	privateIP := "192.168.0.1"
	targetType := elbv2.TargetTypeEnumIp
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndTargetGroup := "cluster-frontend-tg"
	mockLoadBalancers(mockElbV2, lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockDescribeTargetGroups(mockElbV2, tg{arn: clusterFrontEndTargetGroup, targetType: targetType})
	mockClusterTags(mockElbV2,
		lbTags{name: clusterFrontEnd, tags: defaultTags},
	)
	mockElbV2.On("RegisterTargets", mock.Anything).
		Return(&elbv2.RegisterTargetsOutput{}, errors.New("no register for you"))

	// when
	err := elbUpdaterV2.Update(controller.IngressEntries{})

	// then
	assert.EqualError(t, err, fmt.Sprintf("unable to register instance %s (%s) with nlb %s: could not register target group(s) with instance %s (%s): [%s]",
		instanceID, privateIP, clusterFrontEnd, instanceID, privateIP, clusterFrontEndTargetGroup))
}

func TestRegisterInstanceInvalidTypeError(t *testing.T) {
	// given
	elbUpdaterV2, mockElbV2, mockMetadata := setup()
	instanceID := "cow"
	privateIP := "192.168.0.1"
	targetType := elbv2.TargetTypeEnumLambda
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndTargetGroup := "cluster-frontend-tg"
	mockLoadBalancers(mockElbV2, lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockDescribeTargetGroups(mockElbV2, tg{arn: clusterFrontEndTargetGroup, targetType: targetType})
	mockClusterTags(mockElbV2,
		lbTags{name: clusterFrontEnd, tags: defaultTags},
	)
	mockElbV2.On("RegisterTargets", mock.Anything).
		Return(&elbv2.RegisterTargetsOutput{}, errors.New("lambda targets not supported"))

	// when
	err := elbUpdaterV2.Update(controller.IngressEntries{})

	// then
	assert.EqualError(t, err, fmt.Sprintf("unable to register instance %s (%s) with nlb %s: could not register target group(s) with instance %s (%s): [%s]",
		instanceID, privateIP, clusterFrontEnd, instanceID, privateIP, clusterFrontEndTargetGroup))
}

func TestDeRegisterInstanceErrorV2(t *testing.T) {
	// given
	elbUpdaterV2, mockElbV2, mockMetadata := setup()
	instanceID := "cow"
	privateIP := "192.168.0.1"
	targetType := elbv2.TargetTypeEnumIp
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	clusterFrontEnd := "cluster-frontend"
	clusterFrontEndTargetGroup := "cluster-frontend-tg"
	mockLoadBalancers(mockElbV2,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockDescribeTargetGroups(mockElbV2, tg{arn: clusterFrontEndTargetGroup, targetType: targetType})
	mockClusterTags(mockElbV2,
		lbTags{name: clusterFrontEnd, tags: defaultTags},
	)
	mockRegisterTargets(mockElbV2, clusterFrontEndTargetGroup, instanceID, privateIP, targetType)
	mockElbV2.On("DeregisterTargets", mock.Anything).
		Return(&elbv2.DeregisterTargetsOutput{}, errors.New("no deregister for you"))

	// when
	_ = elbUpdaterV2.Start()
	_ = elbUpdaterV2.Update(controller.IngressEntries{})
	err := elbUpdaterV2.Stop()

	// then
	assert.EqualError(t, err, "at least one NLB failed to detach")
}

func TestRetriesUpdateIfFirstAttemptFailsV2(t *testing.T) {
	// given
	elbUpdaterV2, mockElbV2, mockMetadata := setup()
	instanceID := "cow"
	privateIP := "192.168.0.1"
	targetType := elbv2.TargetTypeEnumIp
	mockInstanceMetadata(mockMetadata, instanceID, privateIP)
	clusterFrontEnd := "cluster-frontend"
	mockLoadBalancers(mockElbV2,
		lb{name: clusterFrontEnd, scheme: elbInternalScheme})
	mockDescribeTargetGroups(mockElbV2, tg{"some-target-group-arn", targetType})
	mockClusterTags(mockElbV2,
		lbTags{
			name: clusterFrontEnd,
			tags: defaultTags})
	mockElbV2.On("RegisterTargets", mock.Anything).Return(
		&elbv2.RegisterTargetsOutput{}, errors.New("no register for you"))

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
	elbUpdaterV2, _, _ := setup()

	// when
	err := elbUpdaterV2.Start()

	// then
	assert.NoError(t, err)
	assert.Nil(t, elbUpdaterV2.Health())
}
