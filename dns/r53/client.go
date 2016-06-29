package r53

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
)

// Route53Client is the public interface
type Route53Client interface {
	GetHostedZoneDomain() (string, error)
	UpdateRecordSets(changes []*route53.Change) error
	GetARecords() ([]*route53.ResourceRecordSet, error)
}

// r53 interface exposes the subset of methods we use of the aws sdk
type r53 interface {
	GetHostedZone(input *route53.GetHostedZoneInput) (*route53.GetHostedZoneOutput, error)
	ChangeResourceRecordSets(input *route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error)
	ListResourceRecordSets(input *route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error)
}

// Route53Client enables interaction with aws route53
type dns struct {
	r53        r53
	hostedZone string
}

// New creates a route53 client used to interact with aws
func New(region string, hostedZone string) Route53Client {
	return &dns{
		r53:        route53.New(session.New(), &aws.Config{Region: aws.String(region)}),
		hostedZone: hostedZone,
	}
}

// GetHostedZone gets the domain for the hosted zone
func (dns *dns) GetHostedZoneDomain() (string, error) {
	input := &route53.GetHostedZoneInput{Id: aws.String(dns.hostedZone)}
	hostedZone, err := dns.r53.GetHostedZone(input)
	if err != nil {
		return "", fmt.Errorf("unable to get Hosted Zone Info: %v", err)
	}
	return *hostedZone.HostedZone.Name, nil
}

// UpdateRecordSets updates records in aws based on the change list.
// Todo add tests
func (dns *dns) UpdateRecordSets(changes []*route53.Change) error {
	recordSetsInput := &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(dns.hostedZone),
		ChangeBatch: &route53.ChangeBatch{
			Changes: changes,
		},
	}

	_, err := dns.r53.ChangeResourceRecordSets(recordSetsInput)

	if err != nil {
		return fmt.Errorf("failed to create A record: %v", err)
	}

	return nil
}

// GetARecords gets a list of A Records from aws.
// Todo tests
func (dns *dns) GetARecords() ([]*route53.ResourceRecordSet, error) {
	recordSetsOutput, err := dns.r53.ListResourceRecordSets(&route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(dns.hostedZone),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to fetch A records: %v", err)
	}

	recordSets := recordSetsOutput.ResourceRecordSets

	aRecords := []*route53.ResourceRecordSet{}
	for _, recordSet := range recordSets {
		if *recordSet.Type == "A" {
			aRecords = append(aRecords, recordSet)
		}
	}

	return aRecords, nil
}
