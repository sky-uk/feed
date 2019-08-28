package alb

import (
	"errors"
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
	region = "eu-west-1"
)

type mockALB struct {
	mock.Mock
}

func (m *mockALB) DescribeTargetGroups(input *awselb.DescribeTargetGroupsInput) (*awselb.DescribeTargetGroupsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselb.DescribeTargetGroupsOutput), args.Error(1)
}

func (m *mockALB) RegisterTargets(input *awselb.RegisterTargetsInput) (*awselb.RegisterTargetsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselb.RegisterTargetsOutput), args.Error(1)
}

func (m *mockALB) DeregisterTargets(input *awselb.DeregisterTargetsInput) (*awselb.DeregisterTargetsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*awselb.DeregisterTargetsOutput), args.Error(1)
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

func (m *mockALB) mockDescribeTargetGroups(names []string, arns []string, reqMarker *string, nextMarker *string, err error) {
	var awsNames []*string
	var targetGroups []*awselb.TargetGroup
	for i := range names {
		awsNames = append(awsNames, aws.String(names[i]))
		arn := arns[i]
		if arn != "" {
			targetGroups = append(targetGroups, &awselb.TargetGroup{
				TargetGroupArn: aws.String(arn),
			})
		}
	}

	m.On("DescribeTargetGroups",
		&awselb.DescribeTargetGroupsInput{
			Names:  awsNames,
			Marker: reqMarker,
		}).Return(&awselb.DescribeTargetGroupsOutput{NextMarker: nextMarker, TargetGroups: targetGroups}, err).Once()
}

func (m *mockALB) mockRegisterTargets(targetGroupARN, instanceID string, err error) {
	m.On("RegisterTargets", &awselb.RegisterTargetsInput{
		TargetGroupArn: aws.String(targetGroupARN),
		Targets:        []*awselb.TargetDescription{{Id: aws.String(instanceID)}},
	}).Return(&awselb.RegisterTargetsOutput{}, err)
}

func (m *mockALB) mockDeregisterTargets(targetGroupARN, instanceID string, err error) {
	m.On("DeregisterTargets", &awselb.DeregisterTargetsInput{
		TargetGroupArn: aws.String(targetGroupARN),
		Targets:        []*awselb.TargetDescription{{Id: aws.String(instanceID)}},
	}).Return(&awselb.DeregisterTargetsOutput{}, err)
}

func (m *mockMetadata) mockInstanceMetadata(instanceID string) {
	m.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{InstanceID: instanceID}, nil)
}

func setup(targetGroupNames ...string) (controller.Updater, *mockALB, *mockMetadata) {
	a, _ := New(region, targetGroupNames, time.Nanosecond)
	mockALB := &mockALB{}
	mockMetadata := &mockMetadata{}
	a.(*alb).awsALB = mockALB
	a.(*alb).metadata = mockMetadata
	return a, mockALB, mockMetadata
}

func TestCanNotCreateUpdaterWithoutLabelValue(t *testing.T) {
	//when
	_, err := New(region, []string{}, time.Nanosecond)

	//then
	assert.Error(t, err)
}

func TestRegisterInstance(t *testing.T) {
	//given
	a, mockALB, mockMetadata := setup("internal", "external")
	instanceID := "cow"
	mockMetadata.mockInstanceMetadata(instanceID)
	mockALB.mockDescribeTargetGroups([]string{"internal", "external"}, []string{"internal-arn", "external-arn"},
		nil, nil, nil)
	mockALB.mockRegisterTargets("internal-arn", instanceID, nil)
	mockALB.mockRegisterTargets("external-arn", instanceID, nil)

	//when
	err := a.Start()
	updateErr := a.Update(controller.IngressEntries{})

	//then
	mockALB.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, err)
	assert.NoError(t, updateErr)
	assert.NoError(t, a.Health())
}

func TestReportsErrorIfDidntRegisterAllTargetGroups(t *testing.T) {
	//given
	a, mockALB, mockMetadata := setup("internal", "external")
	instanceID := "cow"
	mockMetadata.mockInstanceMetadata(instanceID)
	mockALB.mockDescribeTargetGroups([]string{"internal", "external"}, []string{"internal-arn", "external-arn"},
		nil, nil, nil)
	mockALB.mockRegisterTargets("internal-arn", instanceID, nil)
	mockALB.mockRegisterTargets("external-arn", instanceID, errors.New("ka boom"))

	//when
	err := a.Start()
	updateErr := a.Update(controller.IngressEntries{})

	//then
	assert.NoError(t, err)
	assert.Error(t, updateErr)
}

func TestErrorGettingMetadata(t *testing.T) {
	e, _, mockMetadata := setup("group")
	mockMetadata.On("GetInstanceIdentityDocument").
		Return(ec2metadata.EC2InstanceIdentityDocument{}, errors.New("no metadata for you"))

	err := e.Update(controller.IngressEntries{})

	assert.Error(t, err)
}

func TestErrorDescribingTargetGroups(t *testing.T) {
	//given
	a, mockALB, mockMetadata := setup("group")
	instanceID := "cow"
	mockMetadata.mockInstanceMetadata(instanceID)
	mockALB.mockDescribeTargetGroups([]string{"group"}, []string{"group-arn"}, nil, nil, errors.New("ba koom"))

	//when
	_ = a.Start()
	updateErr := a.Update(controller.IngressEntries{})

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
	mockALB.mockRegisterTargets("external-arn", instanceID, nil)

	//when
	err := a.Start()
	updateErr := a.Update(controller.IngressEntries{})

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
	mockALB.mockRegisterTargets("internal-arn", instanceID, nil)
	mockALB.mockRegisterTargets("external-arn", instanceID, nil)

	//when
	err := a.Start()
	updateErr := a.Update(controller.IngressEntries{})

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
	mockALB.mockRegisterTargets("internal-arn", instanceID, nil)
	mockALB.mockRegisterTargets("external-arn", instanceID, nil)
	mockALB.mockDeregisterTargets("internal-arn", instanceID, nil)
	mockALB.mockDeregisterTargets("external-arn", instanceID, nil)

	//when
	_ = a.Start()
	_ = a.Update(controller.IngressEntries{})
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
	mockALB.mockRegisterTargets("internal-arn", instanceID, nil)
	mockALB.mockRegisterTargets("external-arn", instanceID, nil)
	mockALB.mockDeregisterTargets("internal-arn", instanceID, errors.New("ba boom"))
	mockALB.mockDeregisterTargets("external-arn", instanceID, nil)

	//when
	_ = a.Start()
	_ = a.Update(controller.IngressEntries{})
	stopErr := a.Stop()

	//then
	mockALB.AssertExpectations(t)
	mockMetadata.AssertExpectations(t)
	assert.NoError(t, stopErr)
}

func TestStopWaitsForDeregisterDelay(t *testing.T) {
	//given
	a, mockALB, mockMetadata := setup("internal", "external")
	instanceID := "cow"
	mockMetadata.mockInstanceMetadata(instanceID)
	mockALB.mockDescribeTargetGroups([]string{"internal", "external"}, []string{"internal-arn", "external-arn"},
		nil, nil, nil)
	mockALB.mockRegisterTargets("internal-arn", instanceID, nil)
	mockALB.mockRegisterTargets("external-arn", instanceID, nil)
	mockALB.mockDeregisterTargets("internal-arn", instanceID, nil)
	mockALB.mockDeregisterTargets("external-arn", instanceID, nil)
	a.(*alb).targetGroupDeregistrationDelay = time.Millisecond * 50

	//when
	_ = a.Start()
	_ = a.Update(controller.IngressEntries{})
	beforeStop := time.Now()
	_ = a.Stop()
	stopDuration := time.Now().Sub(beforeStop)

	//then
	assert.True(t, stopDuration.Nanoseconds() > time.Millisecond.Nanoseconds()*50,
		"Drain time should have caused stop to take at least 50ms.")
}

func TestHealthReportsHealthyBeforeFirstUpdate(t *testing.T) {
	// given
	a, _, _ := setup("internal", "external")

	// when
	err := a.Start()

	// then
	assert.NoError(t, err)
	assert.Nil(t, a.Health())
}

func TestHealthReportsUnhealthyAfterUnsuccessfulFirstUpdate(t *testing.T) {
	//given
	a, mockALB, mockMetadata := setup("internal", "external")
	instanceID := "cow"
	mockMetadata.mockInstanceMetadata(instanceID)
	mockALB.mockDescribeTargetGroups([]string{"internal", "external"}, []string{"", "external-arn"},
		nil, nil, nil)
	mockALB.mockRegisterTargets("external-arn", instanceID, nil)

	//when
	err := a.Start()
	updateErr := a.Update(controller.IngressEntries{})

	//then
	assert.NoError(t, err)
	assert.Error(t, updateErr)
	assert.Error(t, a.Health())
}
