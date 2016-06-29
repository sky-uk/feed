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

func (m *fake53) GetHostedZone(input *route53.GetHostedZoneInput) (*route53.GetHostedZoneOutput, error) {
	args := m.Called(input)
	err := args.Error(1)
	if err != nil {
		return nil, err
	}
	return args.Get(0).(*route53.GetHostedZoneOutput), err
}

func (m *fake53) ChangeResourceRecordSets(input *route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error) {
	panic("not implemented")
}

func (m *fake53) ListResourceRecordSets(input *route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error) {
	panic("not implemented")
}

func TestGetHostedZoneDomain(t *testing.T) {
	id := "james-zone"
	zoneDomain := "james.com"
	dns := New("fake", id).(*dns)
	fake53 := new(fake53)
	dns.r53 = fake53
	fake53.On("GetHostedZone", &route53.GetHostedZoneInput{Id: aws.String(id)}).Return(&route53.GetHostedZoneOutput{
		HostedZone: &route53.HostedZone{
			Name: aws.String(zoneDomain),
		},
	}, nil)

	hz, err := dns.GetHostedZoneDomain()

	assert.NoError(t, err)
	assert.Equal(t, zoneDomain, hz)
}

func TestGetHostedZoneDomainError(t *testing.T) {
	dns := New("fake", "fake").(*dns)
	fake53 := new(fake53)
	dns.r53 = fake53
	fake53.On("GetHostedZone", mock.Anything).Return(nil, errors.New("james says no"))

	_, err := dns.GetHostedZoneDomain()

	assert.EqualError(t, err, "unable to get Hosted Zone Info: james says no")
}
