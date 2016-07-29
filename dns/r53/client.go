package r53

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/sky-uk/feed/util"
)

const maxRecordChanges = 100

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
type client struct {
	r53              r53
	hostedZone       string
	maxRecordChanges int
}

// New creates a route53 client used to interact with aws
func New(region string, hostedZone string, retries int) Route53Client {
	config := aws.Config{Region: aws.String(region), MaxRetries: aws.Int(retries)}
	return &client{
		r53:              route53.New(session.New(), &config),
		hostedZone:       hostedZone,
		maxRecordChanges: maxRecordChanges,
	}
}

// GetHostedZone gets the domain for the hosted zone
func (dns *client) GetHostedZoneDomain() (string, error) {
	input := &route53.GetHostedZoneInput{Id: aws.String(dns.hostedZone)}
	hostedZone, err := dns.r53.GetHostedZone(input)
	if err != nil {
		return "", fmt.Errorf("unable to get Hosted Zone Info: %v", err)
	}
	return *hostedZone.HostedZone.Name, nil
}

// UpdateRecordSets updates records in aws based on the change list.
func (dns *client) UpdateRecordSets(changes []*route53.Change) error {
	partitions := util.Partition(len(changes), dns.maxRecordChanges)
	for _, partition := range partitions {
		batch := changes[partition.Low:partition.High]
		recordSetsInput := &route53.ChangeResourceRecordSetsInput{
			HostedZoneId: aws.String(dns.hostedZone),
			ChangeBatch: &route53.ChangeBatch{
				Changes: batch,
			},
		}

		_, err := dns.r53.ChangeResourceRecordSets(recordSetsInput)

		if err != nil {
			return fmt.Errorf("failed to create A record: %v", err)
		}
	}

	return nil
}

// GetARecords gets a list of A Records from aws.
func (dns *client) GetARecords() ([]*route53.ResourceRecordSet, error) {
	aRecords := []*route53.ResourceRecordSet{}
	request := &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(dns.hostedZone),
	}
	for {
		recordSetsOutput, err := dns.r53.ListResourceRecordSets(request)

		if err != nil {
			return nil, fmt.Errorf("failed to fetch A records: %v", err)
		}

		recordSets := recordSetsOutput.ResourceRecordSets

		for _, recordSet := range recordSets {
			if *recordSet.Type == route53.RRTypeA {
				aRecords = append(aRecords, recordSet)
			}
		}

		if !aws.BoolValue(recordSetsOutput.IsTruncated) {
			break
		}

		request = &route53.ListResourceRecordSetsInput{
			HostedZoneId:    aws.String(dns.hostedZone),
			StartRecordName: recordSetsOutput.NextRecordName,
			StartRecordType: recordSetsOutput.NextRecordType,
		}
	}

	return aRecords, nil
}
