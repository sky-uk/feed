package dns

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	aws_alb "github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/elb"
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
	elbLabelValue                = "elbLabelValue"
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

// Note that unlike most mocks in this test, this is actually mocking a single function from the package not the actual elb.ELB interface
type mockELB struct {
	mock.Mock
}

func (m *mockELB) FindFrontEndElbs(e elb.ELB, labelValue string) (map[string]elb.LoadBalancerDetails, error) {
	args := m.Called(e, labelValue)
	return args.Get(0).(map[string]elb.LoadBalancerDetails), args.Error(1)
}

func (m *mockELB) mockFindFrontEndElbs(labelValue string, lbDetails []lbDetail, err error) {
	lbs := make(map[string]elb.LoadBalancerDetails)
	if lbDetails != nil {
		for _, lb := range lbDetails {
			lbs[lb.scheme] = elb.LoadBalancerDetails{
				DNSName:      lb.dnsName,
				HostedZoneID: lbHostedZoneID,
			}
		}
	}

	m.On("FindFrontEndElbs", mock.Anything, labelValue).Return(lbs, err)
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

func setup() (*updater, *mockR53Client, *mockELB, *mockALB) {
	dnsUpdater := New(hostedZoneID, awsRegion, "", albNames, 1).(*updater)
	mockR53 := &mockR53Client{}
	dnsUpdater.r53 = mockR53
	mockELB := &mockELB{}
	mockALB := &mockALB{}
	dnsUpdater.findELBs = mockELB.FindFrontEndElbs
	dnsUpdater.alb = mockALB
	return dnsUpdater, mockR53, mockELB, mockALB
}

func TestFailsToQueryFrontends(t *testing.T) {
	dnsUpdater, mockR53, _, mockALB := setup()
	mockALB.mockDescribeLoadBalancers(albNames, nil, errors.New("doh"))
	mockR53.mockGetHostedZoneDomain()

	err := dnsUpdater.Start()

	assert.Error(t, err)
}

func TestQueryFrontendElbsFails(t *testing.T) {
	dnsUpdater, mockR53, mockELB, _ := setup()
	dnsUpdater.albNames = nil
	dnsUpdater.elbLabelValue = elbLabelValue
	mockELB.mockFindFrontEndElbs(elbLabelValue, nil, errors.New("Nope"))
	mockR53.mockGetHostedZoneDomain()

	err := dnsUpdater.Start()

	assert.EqualError(t, err, "unable to find front end load balancers: Nope")
}

func TestQueryFrontendElbsTrailingDotOnDomain(t *testing.T) {
	dnsUpdater, mockR53, mockELB, _ := setup()
	lbDetWithDot := []lbDetail{
		{scheme: internalScheme, dnsName: internalALBDnsName},
		{scheme: externalScheme, dnsName: externalALBDnsNameWithPeriod},
	}
	dnsUpdater.albNames = nil
	dnsUpdater.elbLabelValue = elbLabelValue
	mockELB.mockFindFrontEndElbs(elbLabelValue, lbDetWithDot, nil)
	mockR53.mockGetHostedZoneDomain()

	err := dnsUpdater.Start()

	assert.EqualError(t, err, "unexpected trailing dot on load balancer DNS name: "+externalALBDnsNameWithPeriod)
}

func TestQueryFrontedElbs(t *testing.T) {
	dnsUpdater, mockR53, mockELB, _ := setup()
	dnsUpdater.albNames = nil
	dnsUpdater.elbLabelValue = elbLabelValue
	mockELB.mockFindFrontEndElbs(elbLabelValue, lbDetails, nil)
	mockR53.mockGetHostedZoneDomain()

	assert.NoError(t, dnsUpdater.Start())
	assert.Equal(t, map[string]dnsDetails{
		internalScheme: dnsDetails{dnsName: internalALBDnsNameWithPeriod, hostedZoneID: lbHostedZoneID},
		externalScheme: dnsDetails{dnsName: externalALBDnsNameWithPeriod, hostedZoneID: lbHostedZoneID},
	}, dnsUpdater.schemeToDNS)
	mockR53.AssertExpectations(t)
}

func TestGetsDomainNameFails(t *testing.T) {
	dnsUpdater, mockR53, _, mockALB := setup()
	mockALB.mockDescribeLoadBalancers(albNames, lbDetails, nil)
	mockR53.On("GetHostedZoneDomain").Return("", errors.New("No domain for you"))

	err := dnsUpdater.Start()

	assert.Error(t, err)
}

func TestUpdateRecordSetFail(t *testing.T) {
	// given
	dnsUpdater, mockR53, _, mockALB := setup()
	mockR53.mockGetHostedZoneDomain()
	mockR53.mockGetARecords(nil, nil)
	mockALB.mockDescribeLoadBalancers(albNames, lbDetails, nil)

	ingressUpdate := []controller.IngressEntry{{Host: "verification.james.com", ELbScheme: internalScheme}}

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
		update          controller.IngressEntries
		records         []*route53.ResourceRecordSet
		expectedChanges []*route53.Change
	}{
		{
			"Empty update has no change",
			controller.IngressEntries{},
			[]*route53.ResourceRecordSet{},
			[]*route53.Change{},
		},
		{
			"Add new record",
			[]controller.IngressEntry{{
				Name:        "test-entry",
				Host:        "cats.james.com",
				Path:        "/",
				ELbScheme:   internalScheme,
				ServicePort: 80,
			}},
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
			[]controller.IngressEntry{{
				Name:        "test-entry",
				Host:        "foo.james.com",
				Path:        "/",
				ELbScheme:   externalScheme,
				ServicePort: 80,
			}},
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
			controller.IngressEntries{},
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
			[]controller.IngressEntry{{
				Name:        "test-entry",
				Host:        "foo.james.com",
				Path:        "/",
				ELbScheme:   internalScheme,
				ServicePort: 80,
			}},
			[]*route53.ResourceRecordSet{
				{
					Name: aws.String("bar.james.com."),
					AliasTarget: &route53.AliasTarget{
						DNSName:              aws.String(internalALBDnsNameWithPeriod),
						HostedZoneId:         aws.String(lbHostedZoneID),
						EvaluateTargetHealth: aws.Bool(false),
					},
				},
				{
					Name: aws.String("baz.james.com."),
					AliasTarget: &route53.AliasTarget{
						DNSName:              aws.String(unassocALBDnsNameWithPeriod),
						HostedZoneId:         aws.String(lbHostedZoneID),
						EvaluateTargetHealth: aws.Bool(false),
					},
				},
			},
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
			[]controller.IngressEntry{
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
			},
			nil,
			[]*route53.Change{},
		},
		{
			"Duplicate hosts are not duplicated in changeset",
			[]controller.IngressEntry{
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
			},
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
			[]controller.IngressEntry{
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
			},
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
			[]controller.IngressEntry{{
				Name:        "test-entry",
				Host:        "foo.james.com",
				Path:        "/",
				ELbScheme:   externalScheme,
				ServicePort: 80,
			}},
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

		dnsUpdater, mockR53, _, mockALB := setup()
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
