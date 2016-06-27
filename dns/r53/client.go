package r53

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
)

// Route53Client enables interaction with aws route53
type Route53Client struct {
	client     route53.Route53
	hostedZone string
}

// New creates a route53 client used to interact with aws
func New(region string, hostedZone string) Route53Client {
	return Route53Client{
		client:     *route53.New(session.New(), &aws.Config{Region: aws.String(region)}),
		hostedZone: hostedZone,
	}
}

// UpdateRecordSets updates records in aws based on the change list.
func (dns *Route53Client) UpdateRecordSets(changes []*route53.Change) error {

	recordSetsInput := &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(dns.hostedZone),
		ChangeBatch: &route53.ChangeBatch{
			Changes: changes,
		},
	}

	_, err := dns.client.ChangeResourceRecordSets(recordSetsInput)

	if err != nil {
		return fmt.Errorf("failed to create A record: %v", err)
	}

	return nil
}

// GetARecords gets a list of A Records from aws.
func (dns *Route53Client) GetARecords() ([]*route53.ResourceRecordSet, error) {

	recordSetsOutput, err := dns.client.ListResourceRecordSets(&route53.ListResourceRecordSetsInput{
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
