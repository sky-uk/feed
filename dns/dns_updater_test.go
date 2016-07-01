package dns

import (
	"testing"

	"errors"

	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/elb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	r53Zone    = "1234"
	domain     = "james.com."
	elbName    = "elbName"
	elbDNSName = "elbDnsName"
	elbScheme  = "internal"
	awsRegion  = "awsRegion"
)

var defaultFrontends = map[string]elb.LoadBalancerDetails{"internal": elb.LoadBalancerDetails{
	Name:         elbName,
	DNSName:      elbDNSName,
	HostedZoneID: r53Zone,
	Scheme:       elbScheme,
}}

type fakeR53Client struct {
	mock.Mock
}

func (m *fakeR53Client) GetHostedZoneDomain() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *fakeR53Client) UpdateRecordSets(changes []*route53.Change) error {
	args := m.Called(changes)
	return args.Error(0)
}

func (m *fakeR53Client) GetARecords() ([]*route53.ResourceRecordSet, error) {
	args := m.Called()
	if args.Error(1) != nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]*route53.ResourceRecordSet), args.Error(1)
}

func createDNSUpdater() (*updater, *fakeR53Client) {
	dnsUpdater := New(r53Zone, awsRegion, elbName).(*updater)
	dnsUpdater.findElbs = func(elb.ELB, string) (map[string]elb.LoadBalancerDetails, error) {
		return defaultFrontends, nil
	}
	fakeR53Client := new(fakeR53Client)
	dnsUpdater.r53Sdk = fakeR53Client
	fakeR53Client.On("GetHostedZoneDomain").Return(domain, nil)
	fakeR53Client.On("GetARecords").Return([]*route53.ResourceRecordSet{}, nil)
	//fakeR53Client.On("UpdateRecordSets", mock.Anything).Return(nil)
	return dnsUpdater, fakeR53Client
}

func TestQueryFrontendsOnStartup(t *testing.T) {
	dnsUpdater, _ := createDNSUpdater()

	err := dnsUpdater.Start()

	assert.NoError(t, err)
	assert.Equal(t, defaultFrontends, dnsUpdater.frontends)
}

func TestQueryFrontendsFails(t *testing.T) {
	dnsUpdater, _ := createDNSUpdater()
	dnsUpdater.findElbs = func(elb.ELB, string) (map[string]elb.LoadBalancerDetails, error) {
		return nil, errors.New("No elbs for you")
	}

	err := dnsUpdater.Start()

	assert.EqualError(t, err, "unable to find front end load balancers: No elbs for you")
}

func TestGetsDomainName(t *testing.T) {
	dnsUpdater, fakeR53Client := createDNSUpdater()

	fakeR53Client.On("GetHostedZoneDomain").Return(domain, nil)

	err := dnsUpdater.Start()

	assert.NoError(t, err)
	assert.Equal(t, domain, dnsUpdater.domain)
}

func TestGetsDomainNameFails(t *testing.T) {
	fakeR53Client := new(fakeR53Client)
	dnsUpdater := New(domain, awsRegion, elbName).(*updater)
	dnsUpdater.findElbs = func(elb.ELB, string) (map[string]elb.LoadBalancerDetails, error) {
		return nil, nil
	}
	dnsUpdater.r53Sdk = fakeR53Client
	fakeR53Client.On("GetHostedZoneDomain").Return("", errors.New("No domain for you"))

	err := dnsUpdater.Start()

	assert.EqualError(t, err, "unable to get domain for hosted zone: No domain for you")
}

func TestRemovesHostsWithInvalidHost(t *testing.T) {
	// given
	dnsUpdater, fakeR53 := createDNSUpdater()
	validEntry := controller.IngressEntry{
		Host:      fmt.Sprintf("verification.james.com"),
		ELbScheme: "internal",
	}
	invalidEntry := controller.IngressEntry{
		Host:      "notjames.com",
		ELbScheme: "internal",
	}
	ingressUpdate := controller.IngressUpdate{
		Entries: []controller.IngressEntry{validEntry, invalidEntry},
	}
	expectedRecordSetsInput := []*route53.Change{
		{
			Action: aws.String("UPSERT"),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("verification.james.com"),
				Type: aws.String("A"),
				AliasTarget: &route53.AliasTarget{
					DNSName:              aws.String(elbDNSName),
					HostedZoneId:         aws.String(r53Zone),
					EvaluateTargetHealth: aws.Bool(true),
				},
			},
		},
	}
	fakeR53.On("UpdateRecordSets", expectedRecordSetsInput).Return(nil)

	// when
	dnsUpdater.Start()
	err := dnsUpdater.Update(ingressUpdate)

	// then
	assert.NoError(t, err)
	fakeR53.AssertCalled(t, "UpdateRecordSets", expectedRecordSetsInput)
}

func TestUpdateRecordSetFail(t *testing.T) {
	// given
	dnsUpdater, fakeR53 := createDNSUpdater()
	validEntry := controller.IngressEntry{Host: fmt.Sprintf("verification.james.com"), ELbScheme: "internal"}
	ingressUpdate := controller.IngressUpdate{
		Entries: []controller.IngressEntry{validEntry},
	}
	fakeR53.On("UpdateRecordSets", mock.Anything).Return(
		errors.New("No updates for you!"),
	)

	// when
	dnsUpdater.Start()
	err := dnsUpdater.Update(ingressUpdate)

	//then
	assert.EqualError(t, err, "unable to update record sets: No updates for you!")
}

// calculateChanges tests with no external dependencies
func TestEmptyIngressUpdateResultsInNoChange(t *testing.T) {
	// given
	frontEnds := map[string]elb.LoadBalancerDetails{
		"elb-name": elb.LoadBalancerDetails{
			Name:         "elb-name",
			DNSName:      "elb-dnsname",
			HostedZoneID: "elb-hosted-zone-id",
		},
	}

	aRecords := []*route53.ResourceRecordSet{}

	update := controller.IngressUpdate{
		Entries: []controller.IngressEntry{},
	}

	// when
	actualChanges, _ := calculateChanges(frontEnds, aRecords, update, domain)

	// then
	expectedRecordSetsInput := []*route53.Change{}
	assert.Equal(t, expectedRecordSetsInput, actualChanges)
}

func TestUpdateAddsMissingRecordSet(t *testing.T) {
	// given
	frontEnds := map[string]elb.LoadBalancerDetails{
		"internal": elb.LoadBalancerDetails{
			Name:         "elb-name",
			DNSName:      "elb-dnsname",
			HostedZoneID: "elb-hosted-zone-id",
			Scheme:       "internal",
		},
	}

	aRecords := []*route53.ResourceRecordSet{}

	update := controller.IngressUpdate{
		Entries: []controller.IngressEntry{
			controller.IngressEntry{
				Name:        "test-entry",
				Host:        "cats.james.com",
				Path:        "/",
				ELbScheme:   "internal",
				ServicePort: 80,
			},
		},
	}

	// when
	actualChanges, _ := calculateChanges(frontEnds, aRecords, update, domain)

	// then
	expectedRecordSetsInput := []*route53.Change{
		{
			Action: aws.String("UPSERT"),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("cats.james.com"),
				Type: aws.String("A"),
				AliasTarget: &route53.AliasTarget{
					DNSName:              aws.String("elb-dnsname"),
					HostedZoneId:         aws.String("elb-hosted-zone-id"),
					EvaluateTargetHealth: aws.Bool(true),
				},
			},
		},
	}

	assert.Equal(t, expectedRecordSetsInput, actualChanges)
}

func TestUpdatingExistingRecordSet(t *testing.T) {
	// given
	frontEnds := map[string]elb.LoadBalancerDetails{
		"internal": elb.LoadBalancerDetails{
			Name:         "elb-name",
			DNSName:      "elb-dnsname",
			HostedZoneID: "elb-hosted-zone-id",
			Scheme:       "internal",
		},
		"internet-facing": elb.LoadBalancerDetails{
			Name:         "elb-name-2",
			DNSName:      "elb-dnsname-2",
			HostedZoneID: "elb-hosted-zone-id-2",
			Scheme:       "internet-facing",
		},
	}

	aRecords := []*route53.ResourceRecordSet{
		{
			Name: aws.String("foo.james.com."),
			AliasTarget: &route53.AliasTarget{
				DNSName:              aws.String("elb-dnsname"),
				HostedZoneId:         aws.String("elb-hosted-zone-id"),
				EvaluateTargetHealth: aws.Bool(true),
			},
		},
	}

	update := controller.IngressUpdate{
		Entries: []controller.IngressEntry{
			controller.IngressEntry{
				Name:        "test-entry",
				Host:        "foo.james.com",
				Path:        "/",
				ELbScheme:   "internet-facing",
				ServicePort: 80,
			},
		},
	}

	// when
	actualChanges, _ := calculateChanges(frontEnds, aRecords, update, domain)

	// then
	expectedRecordSetsInput := []*route53.Change{
		{
			Action: aws.String("UPSERT"),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("foo.james.com"),
				Type: aws.String("A"),
				AliasTarget: &route53.AliasTarget{
					DNSName:              aws.String("elb-dnsname-2"),
					HostedZoneId:         aws.String("elb-hosted-zone-id-2"),
					EvaluateTargetHealth: aws.Bool(true),
				},
			},
		},
	}

	assert.Equal(t, expectedRecordSetsInput, actualChanges)
}

func TestDeletingExistingRecordSet(t *testing.T) {
	// given
	frontEnds := map[string]elb.LoadBalancerDetails{
		"internal": elb.LoadBalancerDetails{
			Name:         "elb-name",
			DNSName:      "elb-dnsname",
			HostedZoneID: "elb-hosted-zone-id",
			Scheme:       "internal",
		},
	}

	aRecords := []*route53.ResourceRecordSet{
		{
			Name: aws.String("foo.com"),
			AliasTarget: &route53.AliasTarget{
				DNSName:              aws.String("elb-dnsname"),
				HostedZoneId:         aws.String("elb-hosted-zone-id"),
				EvaluateTargetHealth: aws.Bool(true),
			},
		},
	}

	update := controller.IngressUpdate{
		Entries: []controller.IngressEntry{},
	}

	// when
	actualChanges, _ := calculateChanges(frontEnds, aRecords, update, domain)

	// then
	expectedRecordSetsInput := []*route53.Change{
		{
			Action: aws.String("DELETE"),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("foo.com"),
				Type: aws.String("A"),
				AliasTarget: &route53.AliasTarget{
					DNSName:              aws.String("elb-dnsname"),
					HostedZoneId:         aws.String("elb-hosted-zone-id"),
					EvaluateTargetHealth: aws.Bool(true),
				},
			},
		},
	}

	assert.Equal(t, expectedRecordSetsInput, actualChanges)
}

func TestDeletingAndAddingADifferentRecordSet(t *testing.T) {
	// given
	frontEnds := map[string]elb.LoadBalancerDetails{
		"internal": elb.LoadBalancerDetails{
			Name:         "elb-name",
			DNSName:      "elb-dnsname",
			HostedZoneID: "elb-hosted-zone-id",
			Scheme:       "internal",
		},
	}

	aRecords := []*route53.ResourceRecordSet{
		{
			Name: aws.String("bar.james.com"),
			AliasTarget: &route53.AliasTarget{
				DNSName:              aws.String("elb-dnsname"),
				HostedZoneId:         aws.String("elb-hosted-zone-id"),
				EvaluateTargetHealth: aws.Bool(true),
			},
		},
	}

	update := controller.IngressUpdate{
		Entries: []controller.IngressEntry{
			controller.IngressEntry{
				Name:        "test-entry",
				Host:        "foo.james.com",
				Path:        "/",
				ELbScheme:   "internal",
				ServicePort: 80,
			},
		},
	}

	// when
	actualChanges, err := calculateChanges(frontEnds, aRecords, update, domain)

	// then
	assert.NoError(t, err)
	expectedRecordSetsInput := []*route53.Change{
		{
			Action: aws.String("UPSERT"),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("foo.james.com"),
				Type: aws.String("A"),
				AliasTarget: &route53.AliasTarget{
					DNSName:              aws.String("elb-dnsname"),
					HostedZoneId:         aws.String("elb-hosted-zone-id"),
					EvaluateTargetHealth: aws.Bool(true),
				},
			},
		},
		{
			Action: aws.String("DELETE"),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("bar.james.com"),
				Type: aws.String("A"),
				AliasTarget: &route53.AliasTarget{
					DNSName:              aws.String("elb-dnsname"),
					HostedZoneId:         aws.String("elb-hosted-zone-id"),
					EvaluateTargetHealth: aws.Bool(true),
				},
			},
		},
	}

	assert.Equal(t, expectedRecordSetsInput, actualChanges)
}

func TestErrorResponseWhenElbNotFound(t *testing.T) {
	// given
	frontEnds := map[string]elb.LoadBalancerDetails{}

	aRecords := []*route53.ResourceRecordSet{}

	update := controller.IngressUpdate{
		Entries: []controller.IngressEntry{
			controller.IngressEntry{
				Name:        "test-entry",
				Host:        "foo.james.com",
				Path:        "/",
				ELbScheme:   "internal",
				ServicePort: 80,
			},
		},
	}

	// when
	_, err := calculateChanges(frontEnds, aRecords, update, domain)

	// then
	assert.Error(t, err, "Expecting an error when load balancer could not be found.")
}

func TestIngressWithNoFrontEndsAreIgnored(t *testing.T) {
	// given
	frontEnds := map[string]elb.LoadBalancerDetails{}

	aRecords := []*route53.ResourceRecordSet{}

	update := controller.IngressUpdate{
		Entries: []controller.IngressEntry{
			controller.IngressEntry{
				Name:        "test-entry",
				Host:        "foo.com",
				Path:        "/",
				ServicePort: 80,
			},
		},
	}

	// when
	actualChanges, _ := calculateChanges(frontEnds, aRecords, update, domain)

	// then
	assert.Empty(t, actualChanges)
}
