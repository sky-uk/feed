package adapter

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/route53"
)

type staticHostnameAdapter struct {
	addressesWithScheme map[string]string
	ttl                 *int64
}

// NewStaticHostnameAdapter creates a FrontendAdapter which interacts with load balancers accessed by static hostnames.
func NewStaticHostnameAdapter(addressesWithScheme map[string]string, ttl time.Duration) FrontendAdapter {
	return &staticHostnameAdapter{addressesWithScheme, aws.Int64(int64(ttl.Seconds()))}
}

func (s *staticHostnameAdapter) Initialise() (map[string]DNSDetails, error) {
	schemeToFrontendMap := make(map[string]DNSDetails)
	for scheme, address := range s.addressesWithScheme {
		schemeToFrontendMap[scheme] = DNSDetails{DNSName: address}
	}

	return schemeToFrontendMap, nil
}

func (s *staticHostnameAdapter) CreateChange(action string, host string, details DNSDetails,
	recordExists bool, existingRecord *ConsolidatedRecord) *route53.Change {

	if recordExists && existingRecord.TTL != *s.ttl || !recordExists || action == "DELETE" {
		rrs := &route53.ResourceRecordSet{
			Name: aws.String(host),
			Type: aws.String("CNAME"),
			TTL:  s.ttl,
			ResourceRecords: []*route53.ResourceRecord{
				{
					Value: aws.String(details.DNSName),
				},
			},
		}

		return &route53.Change{
			Action:            aws.String(action),
			ResourceRecordSet: rrs,
		}
	}

	return nil
}

func (s *staticHostnameAdapter) Recognise(rrs *route53.ResourceRecordSet) (*ConsolidatedRecord, bool) {
	if *rrs.Type == route53.RRTypeCname {
		record := ConsolidatedRecord{
			Name:     *rrs.Name,
			PointsTo: *rrs.ResourceRecords[0].Value,
		}
		if rrs.TTL != nil {
			record.TTL = *rrs.TTL
		}
		return &record, true
	}

	return nil, false
}
