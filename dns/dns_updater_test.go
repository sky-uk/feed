package dns

import (
	"testing"

	"errors"

	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	aws_alb "github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/util/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func init() {
	metrics.SetConstLabels(make(prometheus.Labels))
	initMetrics()
}

const (
	hostedZoneID                 = "1234"
	domain                       = "james.com."
	awsRegion                    = "awsRegion"
	lbHostedZoneID               = "lb-hosted-zone-id"
	internalALBName              = "internal-alb"
	externalALBName              = "external-alb"
	internalALBDnsName           = "internal-alb-dns-name"
	externalALBDnsName           = "external-alb-dns-name"
	unassocALBDnsName            = "unassoc-alb-dns-name"
	internalALBDnsNameWithPeriod = internalALBDnsName + "."
	externalALBDnsNameWithPeriod = externalALBDnsName + "."
	unassocALBDnsNameWithPeriod  = unassocALBDnsName + "."
	internalScheme               = "internal"
	externalScheme               = "external"
)

var albNames = []string{internalALBName, externalALBName}
var lbDetails = []lbDetail{
	{scheme: internalScheme, dnsName: internalALBDnsName},
	{scheme: externalScheme, dnsName: externalALBDnsName},
}

type lbDetail struct {
	scheme  string
	dnsName string
}

type mockALB struct {
	mock.Mock
}

func (m *mockALB) DescribeLoadBalancers(input *aws_alb.DescribeLoadBalancersInput) (*aws_alb.DescribeLoadBalancersOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*aws_alb.DescribeLoadBalancersOutput), args.Error(1)
}

func (m *mockALB) mockDescribeLoadBalancers(names []string, lbDetails []lbDetail, err error) {
	var lbs []*aws_alb.LoadBalancer

	for _, lb := range lbDetails {
		lbs = append(lbs, &aws_alb.LoadBalancer{
			Scheme:                aws.String(lb.scheme),
			DNSName:               aws.String(lb.dnsName),
			CanonicalHostedZoneId: aws.String(lbHostedZoneID),
		})
	}

	out := &aws_alb.DescribeLoadBalancersOutput{
		LoadBalancers: lbs,
	}

	m.On("DescribeLoadBalancers", &aws_alb.DescribeLoadBalancersInput{
		Names: aws.StringSlice(names),
	}).Return(out, err)
}

type mockR53Client struct {
	mock.Mock
}

func (m *mockR53Client) GetHostedZoneDomain() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *mockR53Client) UpdateRecordSets(changes []*route53.Change) error {
	args := m.Called(changes)
	return args.Error(0)
}

func (m *mockR53Client) GetARecords() ([]*route53.ResourceRecordSet, error) {
	args := m.Called()
	if args.Error(1) != nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]*route53.ResourceRecordSet), args.Error(1)
}

func (m *mockR53Client) mockGetARecords(rs []*route53.ResourceRecordSet, err error) {
	m.On("GetARecords").Return(rs, err)
}

func (m *mockR53Client) mockGetHostedZoneDomain() {
	m.On("GetHostedZoneDomain").Return(domain, nil)
}

func setup(onlyDelAssoc bool) (*updater, *mockR53Client, *mockALB) {
	dnsUpdater := New(hostedZoneID, awsRegion, "", albNames, 1, onlyDelAssoc).(*updater)
	mockR53 := &mockR53Client{}
	dnsUpdater.r53 = mockR53
	mockALB := &mockALB{}
	dnsUpdater.alb = mockALB
	return dnsUpdater, mockR53, mockALB
}

func TestFailsToQueryFrontends(t *testing.T) {
	dnsUpdater, mockR53, mockALB := setup(false)
	mockALB.mockDescribeLoadBalancers(albNames, nil, errors.New("doh"))
	mockR53.mockGetHostedZoneDomain()

	err := dnsUpdater.Start()

	assert.Error(t, err)
}

func TestGetsDomainNameFails(t *testing.T) {
	dnsUpdater, mockR53, mockALB := setup(false)
	mockALB.mockDescribeLoadBalancers(albNames, lbDetails, nil)
	mockR53.On("GetHostedZoneDomain").Return("", errors.New("No domain for you"))

	err := dnsUpdater.Start()

	assert.Error(t, err)
}

func TestUpdateRecordSetFail(t *testing.T) {
	// given
	dnsUpdater, mockR53, mockALB := setup(false)
	mockR53.mockGetHostedZoneDomain()
	mockR53.mockGetARecords(nil, nil)
	mockALB.mockDescribeLoadBalancers(albNames, lbDetails, nil)

	ingressUpdate := controller.IngressUpdate{
		Entries: []controller.IngressEntry{{Host: "verification.james.com", ELbScheme: internalScheme}},
	}
	mockR53.On("UpdateRecordSets", mock.Anything).Return(errors.New("no updates for you"))

	// when
	assert.NoError(t, dnsUpdater.Start())
	err := dnsUpdater.Update(ingressUpdate)

	//then
	assert.Error(t, err)
}

func TestRecordSetUpdates(t *testing.T) {
	var tests = []struct {
		name            string
		onlyDelAssoc    bool
		update          controller.IngressUpdate
		records         []*route53.ResourceRecordSet
		expectedChanges []*route53.Change
	}{
		{
			"Empty update has no change",
			false,
			controller.IngressUpdate{},
			[]*route53.ResourceRecordSet{},
			[]*route53.Change{},
		},
		{
			"Add new record",
			false,
			controller.IngressUpdate{Entries: []controller.IngressEntry{{
				Name:        "test-entry",
				Host:        "cats.james.com",
				Path:        "/",
				ELbScheme:   internalScheme,
				ServicePort: 80,
			}}},
			nil,
			[]*route53.Change{{
				Action: aws.String("UPSERT"),
				ResourceRecordSet: &route53.ResourceRecordSet{
					Name: aws.String("cats.james.com."),
					Type: aws.String("A"),
					AliasTarget: &route53.AliasTarget{
						DNSName:              aws.String(internalALBDnsNameWithPeriod),
						HostedZoneId:         aws.String(lbHostedZoneID),
						EvaluateTargetHealth: aws.Bool(false),
					},
				},
			}},
		},
		{
			"Updating existing record to a new elb schema",
			false,
			controller.IngressUpdate{Entries: []controller.IngressEntry{{
				Name:        "test-entry",
				Host:        "foo.james.com",
				Path:        "/",
				ELbScheme:   externalScheme,
				ServicePort: 80,
			}}},
			[]*route53.ResourceRecordSet{{
				Name: aws.String("foo.james.com."),
				AliasTarget: &route53.AliasTarget{
					DNSName:              aws.String(internalALBDnsNameWithPeriod),
					HostedZoneId:         aws.String(lbHostedZoneID),
					EvaluateTargetHealth: aws.Bool(false),
				},
			}},
			[]*route53.Change{{
				Action: aws.String("UPSERT"),
				ResourceRecordSet: &route53.ResourceRecordSet{
					Name: aws.String("foo.james.com."),
					Type: aws.String("A"),
					AliasTarget: &route53.AliasTarget{
						DNSName:              aws.String(externalALBDnsNameWithPeriod),
						HostedZoneId:         aws.String(lbHostedZoneID),
						EvaluateTargetHealth: aws.Bool(false),
					},
				},
			}},
		},
		{
			"Deleting existing record",
			false,
			controller.IngressUpdate{},
			[]*route53.ResourceRecordSet{
				{
					Name: aws.String("foo.com."),
					AliasTarget: &route53.AliasTarget{
						DNSName:              aws.String(internalALBDnsNameWithPeriod),
						HostedZoneId:         aws.String(lbHostedZoneID),
						EvaluateTargetHealth: aws.Bool(false),
					},
				},
				{
					Name: aws.String("bar.com."),
					AliasTarget: &route53.AliasTarget{
						DNSName:              aws.String(unassocALBDnsNameWithPeriod),
						HostedZoneId:         aws.String(lbHostedZoneID),
						EvaluateTargetHealth: aws.Bool(false),
					},
				},
			},
			[]*route53.Change{
				{
					Action: aws.String("DELETE"),
					ResourceRecordSet: &route53.ResourceRecordSet{
						Name: aws.String("foo.com."),
						Type: aws.String("A"),
						AliasTarget: &route53.AliasTarget{
							DNSName:              aws.String(internalALBDnsNameWithPeriod),
							HostedZoneId:         aws.String(lbHostedZoneID),
							EvaluateTargetHealth: aws.Bool(false),
						},
					},
				},
				{
					Action: aws.String("DELETE"),
					ResourceRecordSet: &route53.ResourceRecordSet{
						Name: aws.String("bar.com."),
						Type: aws.String("A"),
						AliasTarget: &route53.AliasTarget{
							DNSName:              aws.String(unassocALBDnsNameWithPeriod),
							HostedZoneId:         aws.String(lbHostedZoneID),
							EvaluateTargetHealth: aws.Bool(false),
						},
					},
				},
			},
		},
		{
			"Does not deleted unassociated existing record",
			true,
			controller.IngressUpdate{},
			[]*route53.ResourceRecordSet{
				{
					Name: aws.String("foo.com."),
					AliasTarget: &route53.AliasTarget{
						DNSName:              aws.String(internalALBDnsNameWithPeriod),
						HostedZoneId:         aws.String(lbHostedZoneID),
						EvaluateTargetHealth: aws.Bool(false),
					},
				},
				{
					Name: aws.String("bar.com."),
					AliasTarget: &route53.AliasTarget{
						DNSName:              aws.String(unassocALBDnsNameWithPeriod),
						HostedZoneId:         aws.String(lbHostedZoneID),
						EvaluateTargetHealth: aws.Bool(false),
					},
				},
			},
			[]*route53.Change{{
				Action: aws.String("DELETE"),
				ResourceRecordSet: &route53.ResourceRecordSet{
					Name: aws.String("foo.com."),
					Type: aws.String("A"),
					AliasTarget: &route53.AliasTarget{
						DNSName:              aws.String(internalALBDnsNameWithPeriod),
						HostedZoneId:         aws.String(lbHostedZoneID),
						EvaluateTargetHealth: aws.Bool(false),
					},
				},
			}},
		},
		{
			"Adding and deleting records",
			false,
			controller.IngressUpdate{Entries: []controller.IngressEntry{{
				Name:        "test-entry",
				Host:        "foo.james.com",
				Path:        "/",
				ELbScheme:   internalScheme,
				ServicePort: 80,
			}}},
			[]*route53.ResourceRecordSet{{
				Name: aws.String("bar.james.com."),
				AliasTarget: &route53.AliasTarget{
					DNSName:              aws.String(internalALBDnsNameWithPeriod),
					HostedZoneId:         aws.String(lbHostedZoneID),
					EvaluateTargetHealth: aws.Bool(false),
				},
			}},
			[]*route53.Change{{
				Action: aws.String("UPSERT"),
				ResourceRecordSet: &route53.ResourceRecordSet{
					Name: aws.String("foo.james.com."),
					Type: aws.String("A"),
					AliasTarget: &route53.AliasTarget{
						DNSName:              aws.String(internalALBDnsNameWithPeriod),
						HostedZoneId:         aws.String(lbHostedZoneID),
						EvaluateTargetHealth: aws.Bool(false),
					},
				}},
				{
					Action: aws.String("DELETE"),
					ResourceRecordSet: &route53.ResourceRecordSet{
						Name: aws.String("bar.james.com."),
						Type: aws.String("A"),
						AliasTarget: &route53.AliasTarget{
							DNSName:              aws.String(internalALBDnsNameWithPeriod),
							HostedZoneId:         aws.String(lbHostedZoneID),
							EvaluateTargetHealth: aws.Bool(false),
						},
					},
				},
			},
		},
		{
			"Non-matching schemes and domains are ignored",
			false,
			controller.IngressUpdate{Entries: []controller.IngressEntry{
				// host doesn't match james.com.
				{
					Name:        "test-entry",
					Host:        "foo.com",
					Path:        "/",
					ELbScheme:   internalScheme,
					ServicePort: 80,
				},
				// scheme doesn't match internal
				{
					Name:        "test-entry",
					Host:        "foo.james.com",
					Path:        "/",
					ELbScheme:   "invalidscheme",
					ServicePort: 80,
				},
			}},
			nil,
			[]*route53.Change{},
		},
		{
			"Duplicate hosts are not duplicated in changeset",
			false,
			controller.IngressUpdate{Entries: []controller.IngressEntry{
				{
					Name:        "test-entry",
					Host:        "foo.james.com",
					Path:        "/",
					ELbScheme:   internalScheme,
					ServicePort: 80,
				},
				{
					Name:        "test-entry-blah",
					Host:        "foo.james.com",
					Path:        "/blah/",
					ELbScheme:   internalScheme,
					ServicePort: 80,
				},
				{
					Name:        "test-entry-lala",
					Host:        "foo.james.com",
					Path:        "/lala/",
					ELbScheme:   internalScheme,
					ServicePort: 80,
				},
			}},
			nil,
			[]*route53.Change{{
				Action: aws.String("UPSERT"),
				ResourceRecordSet: &route53.ResourceRecordSet{
					Name: aws.String("foo.james.com."),
					Type: aws.String("A"),
					AliasTarget: &route53.AliasTarget{
						DNSName:              aws.String(internalALBDnsNameWithPeriod),
						HostedZoneId:         aws.String(lbHostedZoneID),
						EvaluateTargetHealth: aws.Bool(false),
					},
				},
			}},
		},
		{
			"Should choose first conflicting scheme",
			false,
			controller.IngressUpdate{Entries: []controller.IngressEntry{
				{
					Name:        "test-entry",
					Host:        "bar.james.com",
					Path:        "/",
					ELbScheme:   externalScheme,
					ServicePort: 80,
				},
				{
					Name:        "test-entry",
					Host:        "bar.james.com",
					Path:        "/",
					ELbScheme:   internalScheme,
					ServicePort: 80,
				},
			}},
			nil,
			[]*route53.Change{{
				Action: aws.String("UPSERT"),
				ResourceRecordSet: &route53.ResourceRecordSet{
					Name: aws.String("bar.james.com."),
					Type: aws.String("A"),
					AliasTarget: &route53.AliasTarget{
						DNSName:              aws.String(externalALBDnsNameWithPeriod),
						HostedZoneId:         aws.String(lbHostedZoneID),
						EvaluateTargetHealth: aws.Bool(false),
					},
				},
			}},
		},
		{
			"Does not update records when current and new entry are the same",
			false,
			controller.IngressUpdate{Entries: []controller.IngressEntry{{
				Name:        "test-entry",
				Host:        "foo.james.com",
				Path:        "/",
				ELbScheme:   externalScheme,
				ServicePort: 80,
			}}},
			[]*route53.ResourceRecordSet{{
				Name: aws.String("foo.james.com."),
				AliasTarget: &route53.AliasTarget{
					DNSName:              aws.String(externalALBDnsNameWithPeriod),
					HostedZoneId:         aws.String(lbHostedZoneID),
					EvaluateTargetHealth: aws.Bool(false),
				},
			}},
			[]*route53.Change{},
		},
	}

	for _, test := range tests {
		fmt.Printf("=== test: %s\n", test.name)

		dnsUpdater, mockR53, mockALB := setup(test.onlyDelAssoc)
		mockALB.mockDescribeLoadBalancers(albNames, lbDetails, nil)
		mockR53.mockGetHostedZoneDomain()
		mockR53.mockGetARecords(test.records, nil)
		mockR53.On("UpdateRecordSets", test.expectedChanges).Return(nil)

		assert.NoError(t, dnsUpdater.Start())
		assert.NoError(t, dnsUpdater.Update(test.update))

		mockR53.AssertExpectations(t)

		if t.Failed() {
			t.FailNow()
		}
	}
}
