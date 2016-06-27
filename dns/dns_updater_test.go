package dns

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/elb"
	"github.com/stretchr/testify/assert"
)

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
	actualChanges, _ := calculateChanges(frontEnds, aRecords, update)

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
				Host:        "foo.com",
				Path:        "/",
				ELbScheme:   "internal",
				ServicePort: 80,
			},
		},
	}

	// when
	actualChanges, _ := calculateChanges(frontEnds, aRecords, update)

	// then
	expectedRecordSetsInput := []*route53.Change{
		{
			Action: aws.String("UPSERT"),
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
			Name: aws.String("foo.com."),
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
				Host:        "foo.com",
				Path:        "/",
				ELbScheme:   "internet-facing",
				ServicePort: 80,
			},
		},
	}

	// when
	actualChanges, _ := calculateChanges(frontEnds, aRecords, update)

	// then
	expectedRecordSetsInput := []*route53.Change{
		{
			Action: aws.String("UPSERT"),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("foo.com"),
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
	actualChanges, _ := calculateChanges(frontEnds, aRecords, update)

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
			Name: aws.String("bar.com"),
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
				Host:        "foo.com",
				Path:        "/",
				ELbScheme:   "internal",
				ServicePort: 80,
			},
		},
	}

	// when
	actualChanges, err := calculateChanges(frontEnds, aRecords, update)

	// then
	assert.NoError(t, err)
	expectedRecordSetsInput := []*route53.Change{
		{
			Action: aws.String("UPSERT"),
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
		{
			Action: aws.String("DELETE"),
			ResourceRecordSet: &route53.ResourceRecordSet{
				Name: aws.String("bar.com"),
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
				Host:        "foo.com",
				Path:        "/",
				ELbScheme:   "internal",
				ServicePort: 80,
			},
		},
	}

	// when
	_, err := calculateChanges(frontEnds, aRecords, update)

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
	actualChanges, _ := calculateChanges(frontEnds, aRecords, update)

	// then
	assert.Empty(t, actualChanges)
}
