package alb

import (
	//"errors"
	//"fmt"
	"testing"

	"errors"

	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	aws_alb "github.com/aws/aws-sdk-go/service/elbv2"
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
	region = "eu-west-1"
)

type mockALB struct {
	mock.Mock
}

func (m *mockALB) DescribeTargetGroups(input *aws_alb.DescribeTargetGroupsInput) (*aws_alb.DescribeTargetGroupsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*aws_alb.DescribeTargetGroupsOutput), args.Error(1)
}

func (m *mockALB) RegisterTargets(input *aws_alb.RegisterTargetsInput) (*aws_alb.RegisterTargetsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*aws_alb.RegisterTargetsOutput), args.Error(1)
}

func (m *mockALB) DeregisterTargets(input *aws_alb.DeregisterTargetsInput) (*aws_alb.DeregisterTargetsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*aws_alb.DeregisterTargetsOutput), args.Error(1)
}

type mockMetadata struct {
	mock.Mock
}

func (m *mockMetadata) Available() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *mockMetadata) Region() (string, error) {
	args := m.Called()
	return args.String(0), nil
}

func (m *mockMetadata) GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error) {
	args := m.Called()
	return args.Get(0).(ec2metadata.EC2InstanceIdentityDocument), args.Error(1)
}

type targetGroup struct {
	name string
}

func (m *mockALB) mockDescribeTargetGroups(names []string, arns []string, reqMarker *string, nextMarker *string, err error) {
	var awsNames []*string
	var targetGroups []*aws_alb.TargetGroup
	for i := range names {
		awsNames = append(awsNames, aws.String(names[i]))
		arn := arns[i]
		if arn != "" {
			targetGroups = append(targetGroups, &aws_alb.TargetGroup{
				TargetGroupArn: aws.String(arn),
			})
		}
	}

	m.On("DescribeTargetGroups",
		&aws_alb.DescribeTargetGroupsInput{
			Names:  awsNames,
			Marker: reqMarker,
		}).Return(&aws_alb.DescribeTargetGroupsOutput{NextMarker: nextMarker, TargetGroups: targetGroups}, err).Once()
}

func (m *mockALB) mockRegisterInstances(targetGroupARN, instanceID string, err error) {
	m.On("RegisterTargets", &aws_alb.RegisterTargetsInput{
		TargetGroupArn: aws.String(targetGroupARN),
		Targets:        []*aws_alb.TargetDescription{{Id: aws.String(instanceID)}},
	}).Return(&aws_alb.RegisterTargetsOutput{}, err)
}

func (m *mockALB) mockDeregisterInstances(targetGroupARN, instanceID string, err error) {
	m.On("DeregisterTargets", &aws_alb.DeregisterTargetsInput{
		TargetGroupArn: aws.String(targetGroupARN),
		Targets:        []*aws_alb.TargetDescription{{Id: aws.String(instanceID)}},
	}).Return(&aws_alb.DeregisterTargetsOutput{}, err)
}

func (m *mockMetadata) mockInstanceMetadata(instanceID string) {
	m.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{InstanceID: instanceID}, nil)
}

func setup(targetGroupNames ...string) (controller.Updater, *mockALB, *mockMetadata) {
	a := New(region, targetGroupNames, time.Nanosecond)
	mockALB := &mockALB{}
	mockMetadata := &mockMetadata{}
	a.(*alb).awsALB = mockALB
	a.(*alb).metadata = mockMetadata
	return a, mockALB, mockMetadata
}

func TestNoopIfNoExpectedFrontEnds(t *testing.T) {
	//given
	a, mockElb, mockMetadata := setup()
	a.(*alb).targetGroupNames = nil

	//when
	a.Start()
	a.Update(controller.IngressUpdate{})
	a.Stop()

	//then
	mockElb.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
}

func TestRegisterInstance(t *testing.T) {
	//given
	a, mockALB, mockMetadata := setup("internal", "external")
	instanceID := "cow"
	mockMetadata.mockInstanceMetadata(instanceID)
	mockALB.mockDescribeTargetGroups([]string{"internal", "external"}, []string{"internal-arn", "external-arn"},
		nil, nil, nil)
	mockALB.mockRegisterInstances("internal-arn", instanceID, nil)
	mockALB.mockRegisterInstances("external-arn", instanceID, nil)

	//when
	err := a.Start()
	updateErr := a.Update(controller.IngressUpdate{})

	//then
	mockALB.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, err)
	assert.NoError(t, updateErr)
}

func TestReportsErrorIfDidntRegisterAllTargetGroups(t *testing.T) {
	//given
	a, mockALB, mockMetadata := setup("internal", "external")
	instanceID := "cow"
	mockMetadata.mockInstanceMetadata(instanceID)
	mockALB.mockDescribeTargetGroups([]string{"internal", "external"}, []string{"internal-arn", "external-arn"},
		nil, nil, nil)
	mockALB.mockRegisterInstances("internal-arn", instanceID, nil)
	mockALB.mockRegisterInstances("external-arn", instanceID, errors.New("ka boom"))

	//when
	err := a.Start()
	updateErr := a.Update(controller.IngressUpdate{})

	//then
	assert.NoError(t, err)
	assert.Error(t, updateErr)
}

func TestErrorGettingMetadata(t *testing.T) {
	e, _, mockMetadata := setup("group")
	mockMetadata.On("GetInstanceIdentityDocument").
		Return(ec2metadata.EC2InstanceIdentityDocument{}, errors.New("no metadata for you"))

	err := e.Update(controller.IngressUpdate{})

	assert.Error(t, err)
}

func TestErrorDescribingTargetGroups(t *testing.T) {
	//given
	a, mockALB, mockMetadata := setup("group")
	instanceID := "cow"
	mockMetadata.mockInstanceMetadata(instanceID)
	mockALB.mockDescribeTargetGroups([]string{"group"}, []string{"group-arn"}, nil, nil, errors.New("ba koom"))

	//when
	a.Start()
	updateErr := a.Update(controller.IngressUpdate{})

	//then
	assert.Error(t, updateErr)
}

func TestMissingTargetGroups(t *testing.T) {
	//given
	a, mockALB, mockMetadata := setup("internal", "external")
	instanceID := "cow"
	mockMetadata.mockInstanceMetadata(instanceID)
	mockALB.mockDescribeTargetGroups([]string{"internal", "external"}, []string{"", "external-arn"},
		nil, nil, nil)
	mockALB.mockRegisterInstances("external-arn", instanceID, nil)

	//when
	err := a.Start()
	updateErr := a.Update(controller.IngressUpdate{})

	//then
	assert.NoError(t, err)
	assert.Error(t, updateErr)
}

func TestDescribeTargetGroupPages(t *testing.T) {
	//given
	a, mockALB, mockMetadata := setup("internal", "external")
	instanceID := "cow"
	mockMetadata.mockInstanceMetadata(instanceID)
	mockALB.mockDescribeTargetGroups([]string{"internal", "external"}, []string{"internal-arn", ""},
		nil, aws.String("do-next"), nil)
	mockALB.mockDescribeTargetGroups([]string{"internal", "external"}, []string{"", "external-arn"},
		aws.String("do-next"), nil, nil)
	mockALB.mockRegisterInstances("internal-arn", instanceID, nil)
	mockALB.mockRegisterInstances("external-arn", instanceID, nil)

	//when
	err := a.Start()
	updateErr := a.Update(controller.IngressUpdate{})

	//then
	mockALB.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, err)
	assert.NoError(t, updateErr)
}

func TestDeregistersOnStop(t *testing.T) {
	//given
	a, mockALB, mockMetadata := setup("internal", "external")
	instanceID := "cow"
	mockMetadata.mockInstanceMetadata(instanceID)
	mockALB.mockDescribeTargetGroups([]string{"internal", "external"}, []string{"internal-arn", "external-arn"},
		nil, nil, nil)
	mockALB.mockRegisterInstances("internal-arn", instanceID, nil)
	mockALB.mockRegisterInstances("external-arn", instanceID, nil)
	mockALB.mockDeregisterInstances("internal-arn", instanceID, nil)
	mockALB.mockDeregisterInstances("external-arn", instanceID, nil)

	//when
	a.Start()
	a.Update(controller.IngressUpdate{})
	stopErr := a.Stop()

	//then
	mockALB.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, stopErr)
}

func TestDeregisterErrorIsHandledInStop(t *testing.T) {
	//given
	a, mockALB, mockMetadata := setup("internal", "external")
	instanceID := "cow"
	mockMetadata.mockInstanceMetadata(instanceID)
	mockALB.mockDescribeTargetGroups([]string{"internal", "external"}, []string{"internal-arn", "external-arn"},
		nil, nil, nil)
	mockALB.mockRegisterInstances("internal-arn", instanceID, nil)
	mockALB.mockRegisterInstances("external-arn", instanceID, nil)
	mockALB.mockDeregisterInstances("internal-arn", instanceID, errors.New("ba boom"))
	mockALB.mockDeregisterInstances("external-arn", instanceID, nil)

	//when
	a.Start()
	a.Update(controller.IngressUpdate{})
	stopErr := a.Stop()

	//then
	mockALB.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, stopErr)
}

func TestStopWaitsForDeregisterDelay(t *testing.T) {
	//given
	a, _, _ := setup()
	a.(*alb).targetGroupNames = nil
	a.(*alb).targetGroupDeregistrationDelay = time.Millisecond * 50

	//when
	a.Start()
	beforeStop := time.Now()
	a.Stop()
	stopDuration := time.Now().Sub(beforeStop)

	//then
	assert.True(t, stopDuration.Nanoseconds() > time.Millisecond.Nanoseconds()*50)
}
