package dns

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

func (s *staticHostnameAdapter) initialise() (map[string]dnsDetails, error) {
	schemeToFrontendMap := make(map[string]dnsDetails)
	for scheme, address := range s.addressesWithScheme {
		schemeToFrontendMap[scheme] = dnsDetails{dnsName: address, hostedZoneID: ""}
	}

	return schemeToFrontendMap, nil
}

func (s *staticHostnameAdapter) createChange(action string, host string, details dnsDetails,
	recordExists bool, existingRecord *consolidatedRecord) *route53.Change {

	if recordExists && existingRecord.ttl != *s.ttl || !recordExists || action == "DELETE" {
		rrs := &route53.ResourceRecordSet{
			Name: aws.String(host),
			Type: aws.String("CNAME"),
			TTL:  s.ttl,
			ResourceRecords: []*route53.ResourceRecord{
				{
					Value: aws.String(details.dnsName),
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

func (s *staticHostnameAdapter) recognise(rrs *route53.ResourceRecordSet) (*consolidatedRecord, bool) {
	if *rrs.Type == route53.RRTypeCname {
		record := consolidatedRecord{
			name:     *rrs.Name,
			pointsTo: *rrs.ResourceRecords[0].Value,
		}
		if rrs.TTL != nil {
			record.ttl = *rrs.TTL
		}
		return &record, true
	}

	return nil, false
}
