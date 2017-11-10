package dns

import (
	"errors"
	"fmt"
	"testing"

	"time"

	"github.com/aws/aws-sdk-go/aws"
	aws_elb "github.com/aws/aws-sdk-go/service/elb"
	aws_alb "github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/dns/adapter"
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
	internalAddressArgument      = "ha-ingress-internal"
	externalAddressArgument      = "ha-ingress-external"
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

func (m *mockELB) DescribeLoadBalancers(input *aws_elb.DescribeLoadBalancersInput) (*aws_elb.DescribeLoadBalancersOutput, error) {
	return nil, nil
}

func (m *mockELB) DescribeTags(input *aws_elb.DescribeTagsInput) (*aws_elb.DescribeTagsOutput, error) {
	return nil, nil
}

func (m *mockELB) RegisterInstancesWithLoadBalancer(input *aws_elb.RegisterInstancesWithLoadBalancerInput) (*aws_elb.RegisterInstancesWithLoadBalancerOutput, error) {
	return nil, nil
}

func (m *mockELB) DeregisterInstancesFromLoadBalancer(input *aws_elb.DeregisterInstancesFromLoadBalancerInput) (*aws_elb.DeregisterInstancesFromLoadBalancerOutput, error) {
	return nil, nil
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

func (m *mockR53Client) GetRecords() ([]*route53.ResourceRecordSet, error) {
	args := m.Called()
	if args.Error(1) != nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]*route53.ResourceRecordSet), args.Error(1)
}

func (m *mockR53Client) mockGetRecords(rs []*route53.ResourceRecordSet, err error) {
	m.On("GetRecords").Return(rs, err)
}

func (m *mockR53Client) mockGetHostedZoneDomain() {
	m.On("GetHostedZoneDomain").Return(domain, nil)
}

func setupForELB(albNames []string, elbLabelValue string) (*updater, *mockR53Client, *mockELB, *mockALB) {
	mockALB := &mockALB{}
	mockELB := &mockELB{}

	config := adapter.AWSAdapterConfig{
		HostedZoneID:  hostedZoneID,
		ELBLabelValue: elbLabelValue,
		ALBNames:      albNames,
		ELBClient:     mockELB,
		ALBClient:     mockALB,
		ELBFinder:     mockELB.FindFrontEndElbs,
	}
	lbAdapter, _ := adapter.NewAWSAdapter(&config)
	dnsUpdater := New(hostedZoneID, lbAdapter, 1).(*updater)

	mockR53 := &mockR53Client{}
	dnsUpdater.r53 = mockR53
	return dnsUpdater, mockR53, mockELB, mockALB
}

func setupForExplicitAddresses(definedFrontends map[string]string) (*updater, *mockR53Client) {
	lbAdapter := adapter.NewStaticHostnameAdapter(definedFrontends, 5*time.Minute)

	dnsUpdater := New(hostedZoneID, lbAdapter, 1).(*updater)
	mockR53 := &mockR53Client{}
	dnsUpdater.r53 = mockR53
	return dnsUpdater, mockR53
}

func TestFailsToQueryFrontends(t *testing.T) {
	dnsUpdater, mockR53, _, mockALB := setupForELB(albNames, "")
	mockALB.mockDescribeLoadBalancers(albNames, nil, errors.New("doh"))
	mockR53.mockGetHostedZoneDomain()

	err := dnsUpdater.Start()

	assert.Error(t, err)
}

func TestQueryFrontendElbsFails(t *testing.T) {
	dnsUpdater, mockR53, mockELB, _ := setupForELB(nil, elbLabelValue)
	mockELB.mockFindFrontEndElbs(elbLabelValue, nil, errors.New("nope"))
	mockR53.mockGetHostedZoneDomain()

	err := dnsUpdater.Start()

	assert.EqualError(t, err, "unable to find front end load balancers: nope")
}

func TestQueryFrontendElbsTrailingDotOnDomain(t *testing.T) {
	dnsUpdater, mockR53, mockELB, _ := setupForELB(nil, elbLabelValue)
	lbDetWithDot := []lbDetail{
		{scheme: internalScheme, dnsName: internalALBDnsName},
		{scheme: externalScheme, dnsName: externalALBDnsNameWithPeriod},
	}
	mockELB.mockFindFrontEndElbs(elbLabelValue, lbDetWithDot, nil)
	mockR53.mockGetHostedZoneDomain()

	err := dnsUpdater.Start()

	assert.EqualError(t, err, "unexpected trailing dot on load balancer DNS name: "+externalALBDnsNameWithPeriod)
}

func TestQueryFrontedElbs(t *testing.T) {
	dnsUpdater, mockR53, mockELB, _ := setupForELB(nil, elbLabelValue)
	mockELB.mockFindFrontEndElbs(elbLabelValue, lbDetails, nil)
	mockR53.mockGetHostedZoneDomain()

	assert.NoError(t, dnsUpdater.Start())
	assert.Equal(t, map[string]adapter.DNSDetails{
		internalScheme: {DNSName: internalALBDnsNameWithPeriod, HostedZoneID: lbHostedZoneID},
		externalScheme: {DNSName: externalALBDnsNameWithPeriod, HostedZoneID: lbHostedZoneID},
	}, dnsUpdater.schemeToFrontendMap)
	mockR53.AssertExpectations(t)
}

func TestGetsDomainNameFails(t *testing.T) {
	dnsUpdater, mockR53, _, mockALB := setupForELB(albNames, "")
	mockALB.mockDescribeLoadBalancers(albNames, lbDetails, nil)
	mockR53.On("GetHostedZoneDomain").Return("", errors.New("no domain for you"))

	err := dnsUpdater.Start()

	assert.Error(t, err)
}

func TestUpdateRecordSetFail(t *testing.T) {
	// given
	dnsUpdater, mockR53, _, mockALB := setupForELB(albNames, "")
	mockR53.mockGetHostedZoneDomain()
	mockR53.mockGetRecords(nil, nil)
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
					Type: aws.String(route53.RRTypeA),
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
				Type: aws.String(route53.RRTypeA),
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
					Type: aws.String(route53.RRTypeA),
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
					Type: aws.String(route53.RRTypeA),
					AliasTarget: &route53.AliasTarget{
						DNSName:              aws.String(internalALBDnsNameWithPeriod),
						HostedZoneId:         aws.String(lbHostedZoneID),
						EvaluateTargetHealth: aws.Bool(false),
					},
				},
				{
					Name: aws.String("bar.com."),
					Type: aws.String(route53.RRTypeA),
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
					Type: aws.String(route53.RRTypeA),
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
					Type: aws.String(route53.RRTypeA),
					AliasTarget: &route53.AliasTarget{
						DNSName:              aws.String(internalALBDnsNameWithPeriod),
						HostedZoneId:         aws.String(lbHostedZoneID),
						EvaluateTargetHealth: aws.Bool(false),
					},
				},
				{
					Name: aws.String("baz.james.com."),
					Type: aws.String(route53.RRTypeA),
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
					Type: aws.String(route53.RRTypeA),
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
						Type: aws.String(route53.RRTypeA),
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
					Type: aws.String(route53.RRTypeA),
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
					Type: aws.String(route53.RRTypeA),
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
				Type: aws.String(route53.RRTypeA),
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

		dnsUpdater, mockR53, _, mockALB := setupForELB(albNames, "")
		mockALB.mockDescribeLoadBalancers(albNames, lbDetails, nil)
		mockR53.mockGetHostedZoneDomain()
		mockR53.mockGetRecords(test.records, nil)
		mockR53.On("UpdateRecordSets", test.expectedChanges).Return(nil)

		assert.NoError(t, dnsUpdater.Start())
		assert.NoError(t, dnsUpdater.Update(test.update))

		mockR53.AssertExpectations(t)

		if t.Failed() {
			t.FailNow()
		}
	}
}

func TestRecordSetUpdatesWithAddressArguments(t *testing.T) {
	ttl := aws.Int64(300)
	internalAndExternalFrontends := map[string]string{internalScheme: internalAddressArgument, externalScheme: externalAddressArgument}

	var tests = []struct {
		name             string
		definedFrontends map[string]string
		update           controller.IngressEntries
		records          []*route53.ResourceRecordSet
		expectedChanges  []*route53.Change
	}{
		{
			"Empty update has no change",
			internalAndExternalFrontends,
			controller.IngressEntries{},
			[]*route53.ResourceRecordSet{},
			[]*route53.Change{},
		},
		{
			"Add new record",
			internalAndExternalFrontends,
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
					Type: aws.String("CNAME"),
					ResourceRecords: []*route53.ResourceRecord{
						{
							Value: aws.String(internalAddressArgument),
						},
					},
					TTL: ttl,
				},
			}},
		},
		{
			"Updating existing record to a new scheme",
			internalAndExternalFrontends,
			[]controller.IngressEntry{{
				Name:        "test-entry",
				Host:        "foo.james.com",
				Path:        "/",
				ELbScheme:   externalScheme,
				ServicePort: 80,
			}},
			[]*route53.ResourceRecordSet{{
				Name: aws.String("foo.james.com."),
				Type: aws.String(route53.RRTypeCname),
				ResourceRecords: []*route53.ResourceRecord{
					{
						Value: aws.String(internalAddressArgument),
					},
				},
				TTL: ttl,
			}},
			[]*route53.Change{{
				Action: aws.String("UPSERT"),
				ResourceRecordSet: &route53.ResourceRecordSet{
					Name: aws.String("foo.james.com."),
					Type: aws.String("CNAME"),
					ResourceRecords: []*route53.ResourceRecord{
						{
							Value: aws.String(externalAddressArgument),
						},
					},
					TTL: ttl,
				},
			}},
		},
		{
			"Deleting existing record",
			internalAndExternalFrontends,
			controller.IngressEntries{},
			[]*route53.ResourceRecordSet{
				{
					Name: aws.String("foo.com."),
					Type: aws.String(route53.RRTypeCname),
					ResourceRecords: []*route53.ResourceRecord{
						{
							Value: aws.String(internalAddressArgument),
						},
					},
					TTL: ttl,
				},
				{
					Name: aws.String("bar.com."),
					Type: aws.String(route53.RRTypeCname),
					ResourceRecords: []*route53.ResourceRecord{
						{
							Value: aws.String("some-ingress-we-dont-manage.james.com"),
						},
					},
					TTL: ttl,
				},
			},
			[]*route53.Change{{
				Action: aws.String("DELETE"),
				ResourceRecordSet: &route53.ResourceRecordSet{
					Name: aws.String("foo.com."),
					Type: aws.String("CNAME"),
					ResourceRecords: []*route53.ResourceRecord{
						{
							Value: aws.String(internalAddressArgument),
						},
					},
					TTL: ttl,
				},
			}},
		},
		{
			"Adding and deleting records",
			internalAndExternalFrontends,
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
					Type: aws.String(route53.RRTypeCname),
					ResourceRecords: []*route53.ResourceRecord{
						{
							Value: aws.String(internalAddressArgument),
						},
					},
					TTL: ttl,
				},
				{
					Name: aws.String("baz.james.com."),
					Type: aws.String(route53.RRTypeCname),
					ResourceRecords: []*route53.ResourceRecord{
						{
							Value: aws.String("somerandom-ingress.s.sandbox.james.com"),
						},
					},
					TTL: ttl,
				},
			},
			[]*route53.Change{{
				Action: aws.String("UPSERT"),
				ResourceRecordSet: &route53.ResourceRecordSet{
					Name: aws.String("foo.james.com."),
					Type: aws.String("CNAME"),
					ResourceRecords: []*route53.ResourceRecord{
						{
							Value: aws.String(internalAddressArgument),
						},
					},
					TTL: ttl,
				}},
				{
					Action: aws.String("DELETE"),
					ResourceRecordSet: &route53.ResourceRecordSet{
						Name: aws.String("bar.james.com."),
						Type: aws.String("CNAME"),
						ResourceRecords: []*route53.ResourceRecord{
							{
								Value: aws.String(internalAddressArgument),
							},
						},
						TTL: ttl,
					},
				},
			},
		},
		{
			"Non-matching schemes and domains are ignored",
			internalAndExternalFrontends,
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
			internalAndExternalFrontends,
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
					Type: aws.String("CNAME"),
					ResourceRecords: []*route53.ResourceRecord{
						{
							Value: aws.String(internalAddressArgument),
						},
					},
					TTL: ttl,
				},
			}},
		},
		{
			"Should choose first conflicting scheme",
			internalAndExternalFrontends,
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
					Type: aws.String("CNAME"),
					ResourceRecords: []*route53.ResourceRecord{
						{
							Value: aws.String(externalAddressArgument),
						},
					},
					TTL: ttl,
				},
			}},
		},
		{
			"Does not update records when current and new entry are the same",
			internalAndExternalFrontends,
			[]controller.IngressEntry{{
				Name:        "test-entry",
				Host:        "foo.james.com",
				Path:        "/",
				ELbScheme:   externalScheme,
				ServicePort: 80,
			}},
			[]*route53.ResourceRecordSet{{
				Name: aws.String("foo.james.com."),
				Type: aws.String(route53.RRTypeCname),
				TTL:  ttl,
				ResourceRecords: []*route53.ResourceRecord{
					{
						Value: aws.String(externalAddressArgument),
					},
				},
			}},
			[]*route53.Change{},
		},
		{
			"Updates TTL on a CNAME record when it has changed",
			internalAndExternalFrontends,
			[]controller.IngressEntry{{
				Name:        "test-entry",
				Host:        "foo.james.com",
				Path:        "/",
				ELbScheme:   externalScheme,
				ServicePort: 80,
			}},
			[]*route53.ResourceRecordSet{{
				Name: aws.String("foo.james.com."),
				Type: aws.String(route53.RRTypeCname),
				TTL:  aws.Int64(999),
				ResourceRecords: []*route53.ResourceRecord{
					{
						Value: aws.String(externalAddressArgument),
					},
				},
			}},
			[]*route53.Change{{
				Action: aws.String("UPSERT"),
				ResourceRecordSet: &route53.ResourceRecordSet{
					Name: aws.String("foo.james.com."),
					Type: aws.String("CNAME"),
					ResourceRecords: []*route53.ResourceRecord{
						{
							Value: aws.String(externalAddressArgument),
						},
					},
					TTL: ttl,
				},
			}},
		},
		{
			"Handles existing records which have no TTL",
			internalAndExternalFrontends,
			[]controller.IngressEntry{{
				Name:        "test-entry",
				Host:        "foo.james.com",
				Path:        "/",
				ELbScheme:   externalScheme,
				ServicePort: 80,
			}},
			[]*route53.ResourceRecordSet{{
				Name: aws.String("foo.james.com."),
				Type: aws.String(route53.RRTypeCname),
				ResourceRecords: []*route53.ResourceRecord{
					{
						Value: aws.String(externalAddressArgument),
					},
				},
			}},
			[]*route53.Change{{
				Action: aws.String("UPSERT"),
				ResourceRecordSet: &route53.ResourceRecordSet{
					Name: aws.String("foo.james.com."),
					Type: aws.String("CNAME"),
					ResourceRecords: []*route53.ResourceRecord{
						{
							Value: aws.String(externalAddressArgument),
						},
					},
					TTL: ttl,
				},
			}},
		},
		{
			"Ignores ingresses which use a scheme for which no frontend is defined",
			map[string]string{internalScheme: internalAddressArgument},
			[]controller.IngressEntry{{
				Name:        "test-entry",
				Host:        "foo.james.com",
				Path:        "/",
				ELbScheme:   externalScheme,
				ServicePort: 80,
			}},
			[]*route53.ResourceRecordSet{},
			[]*route53.Change{},
		},
	}

	for _, test := range tests {
		fmt.Printf("=== test: TestRecordSetUpdatesWithAddressArguments: %s\n", test.name)

		dnsUpdater, mockR53 := setupForExplicitAddresses(test.definedFrontends)
		mockR53.mockGetHostedZoneDomain()
		mockR53.mockGetRecords(test.records, nil)
		mockR53.On("UpdateRecordSets", test.expectedChanges).Return(nil)

		assert.NoError(t, dnsUpdater.Start())
		assert.NoError(t, dnsUpdater.Update(test.update))

		mockR53.AssertExpectations(t)

		if t.Failed() {
			t.FailNow()
		}
	}
}
