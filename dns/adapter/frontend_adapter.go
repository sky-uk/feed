package adapter

import "github.com/aws/aws-sdk-go/service/route53"

// FrontendAdapter defines operations which vary based on the type of load balancer being used for ingress.
type FrontendAdapter interface {
	Initialise() (map[string]DNSDetails, error)
	CreateChange(action string, host string, details DNSDetails, recordExists bool, existingRecord *ConsolidatedRecord) *route53.Change
	IsManaged(*route53.ResourceRecordSet) (*ConsolidatedRecord, bool)
}

// DNSDetails defines a DNS name and, optionally, how it maps to an AWS Route53 zone
type DNSDetails struct {
	DNSName      string
	HostedZoneID string
}

// ConsolidatedRecord describes how a DNS name maps to a static load balancer or AWS ELBs or ALBs.
type ConsolidatedRecord struct {
	Name            string
	PointsTo        string
	AliasHostedZone string
	TTL             int64
}
