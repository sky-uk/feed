package r53

import (
	"testing"

	"errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type fake53 struct {
	mock.Mock
}

const hostedZone = "james-zone"

func (m *fake53) GetHostedZone(input *route53.GetHostedZoneInput) (*route53.GetHostedZoneOutput, error) {
	args := m.Called(input)
	err := args.Error(1)
	if err != nil {
		return nil, err
	}
	return args.Get(0).(*route53.GetHostedZoneOutput), err
}

func (m *fake53) ChangeResourceRecordSets(input *route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error) {
	args := m.Called(input)
	err := args.Error(1)
	if err != nil {
		return nil, err
	}
	return args.Get(0).(*route53.ChangeResourceRecordSetsOutput), err
}

func (m *fake53) ListResourceRecordSets(input *route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error) {
	args := m.Called(input)
	err := args.Error(1)
	if err != nil {
		return nil, err
	}
	return args.Get(0).(*route53.ListResourceRecordSetsOutput), err
}

func TestGetHostedZoneDomain(t *testing.T) {
	zoneDomain := "james.com"
	client, fake53 := createClient()
	fake53.On("GetHostedZone", &route53.GetHostedZoneInput{Id: aws.String(hostedZone)}).Return(&route53.GetHostedZoneOutput{
		HostedZone: &route53.HostedZone{
			Name: aws.String(zoneDomain),
		},
	}, nil)

	hz, err := client.GetHostedZoneDomain()

	assert.NoError(t, err)
	assert.Equal(t, zoneDomain, hz)
}

func TestGetHostedZoneDomainError(t *testing.T) {
	client, fake53 := createClient()
	fake53.On("GetHostedZone", mock.Anything).Return(nil, errors.New("james says no"))

	_, err := client.GetHostedZoneDomain()

	assert.EqualError(t, err, "unable to get Hosted Zone Info: james says no")
}

func TestGetRecords(t *testing.T) {
	// given
	client, fake53 := createClient()
	expectedRecords := []*route53.ResourceRecordSet{
		&route53.ResourceRecordSet{
			Name: aws.String("james.com"),
			Type: aws.String("A"),
		},
		&route53.ResourceRecordSet{
			Name: aws.String("james2.com"),
			Type: aws.String("CNAME"),
		},
	}
	fake53.On("ListResourceRecordSets", &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(hostedZone),
	}).Return(&route53.ListResourceRecordSetsOutput{ResourceRecordSets: expectedRecords}, nil)

	// when
	records, err := client.GetRecords()

	// then
	assert.NoError(t, err)
	assert.Equal(t, expectedRecords, records)
}

func TestGetRecordsFiltersOutNonARecordsAndCNAMERecords(t *testing.T) {
	// given
	client, fake53 := createClient()
	aRecord := &route53.ResourceRecordSet{
		Name: aws.String("james.com"),
		Type: aws.String("A"),
	}
	cRecord := &route53.ResourceRecordSet{
		Name: aws.String("james2.com"),
		Type: aws.String("CNAME"),
	}
	txtRecord := &route53.ResourceRecordSet{
		Name: aws.String("blah-mx"),
		Type: aws.String("TXT"),
	}
	allRecords := []*route53.ResourceRecordSet{aRecord, cRecord, txtRecord}
	fake53.On("ListResourceRecordSets", &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(hostedZone),
	}).Return(&route53.ListResourceRecordSetsOutput{ResourceRecordSets: allRecords}, nil)

	// when
	records, err := client.GetRecords()

	// then
	acRecords := []*route53.ResourceRecordSet{aRecord, cRecord}
	assert.NoError(t, err)
	assert.Equal(t, acRecords, records)
}

func TestGetARecordPages(t *testing.T) {
	// given
	client, fake53 := createClient()
	firstRecord := &route53.ResourceRecordSet{
		Name: aws.String("james.com"),
		Type: aws.String("A"),
	}
	secondRecord := &route53.ResourceRecordSet{
		Name: aws.String("yo.com"),
		Type: aws.String("A"),
	}
	fake53.On("ListResourceRecordSets", &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(hostedZone),
	}).Return(&route53.ListResourceRecordSetsOutput{
		ResourceRecordSets: []*route53.ResourceRecordSet{firstRecord},
		IsTruncated:        aws.Bool(true),
		NextRecordName:     aws.String("yo.com"),
		NextRecordType:     aws.String("A"),
	}, nil)
	fake53.On("ListResourceRecordSets", &route53.ListResourceRecordSetsInput{
		HostedZoneId:    aws.String(hostedZone),
		StartRecordName: aws.String("yo.com"),
		StartRecordType: aws.String("A"),
	}).Return(&route53.ListResourceRecordSetsOutput{
		ResourceRecordSets: []*route53.ResourceRecordSet{secondRecord},
		IsTruncated:        aws.Bool(false),
	}, nil)

	// when
	records, err := client.GetRecords()

	// then
	allRecords := []*route53.ResourceRecordSet{firstRecord, secondRecord}
	assert.NoError(t, err)
	assert.Equal(t, allRecords, records)
}

func TestUpdateRecordSetsFull(t *testing.T) {
	// given
	client, fake53 := createClient()
	client.maxRecordChanges = 1
	fake53.On("ChangeResourceRecordSets", mock.Anything).Return(&route53.ChangeResourceRecordSetsOutput{}, nil)
	firstChange := &route53.Change{Action: aws.String("UPDATE")}
	secondChange := &route53.Change{Action: aws.String("DELETE")}

	// when
	err := client.UpdateRecordSets([]*route53.Change{firstChange, secondChange})

	// then
	assert.NoError(t, err)
	fake53.AssertCalled(t, "ChangeResourceRecordSets", &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(hostedZone),
		ChangeBatch:  &route53.ChangeBatch{Changes: []*route53.Change{firstChange}},
	})
	fake53.AssertCalled(t, "ChangeResourceRecordSets", &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(hostedZone),
		ChangeBatch:  &route53.ChangeBatch{Changes: []*route53.Change{secondChange}},
	})
}

func TestUpdateRecordSetsPartial(t *testing.T) {
	// given
	client, fake53 := createClient()
	client.maxRecordChanges = 2
	fake53.On("ChangeResourceRecordSets", mock.Anything).Return(&route53.ChangeResourceRecordSetsOutput{}, nil)
	firstChange := &route53.Change{Action: aws.String("UPDATE")}
	secondChange := &route53.Change{Action: aws.String("DELETE")}
	thirdChange := &route53.Change{Action: aws.String("EAT")}

	// when
	err := client.UpdateRecordSets([]*route53.Change{firstChange, secondChange, thirdChange})

	// then
	assert.NoError(t, err)
	fake53.AssertCalled(t, "ChangeResourceRecordSets", &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(hostedZone),
		ChangeBatch:  &route53.ChangeBatch{Changes: []*route53.Change{firstChange, secondChange}},
	})
	fake53.AssertCalled(t, "ChangeResourceRecordSets", &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(hostedZone),
		ChangeBatch:  &route53.ChangeBatch{Changes: []*route53.Change{thirdChange}},
	})
}

func createClient() (*client, *fake53) {
	client := New("fake", hostedZone, 1).(*client)
	fake53 := new(fake53)
	client.r53 = fake53
	return client, fake53
}
