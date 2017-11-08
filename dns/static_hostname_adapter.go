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

func (s *staticHostnameAdapter) newChange(action string, host string, details dnsDetails) *route53.Change {
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

func (s *staticHostnameAdapter) changeExistingIfRequired(record consolidatedRecord, host string, details dnsDetails) *route53.Change {
	if record.ttl != *s.ttl {
		return s.newChange("UPSERT", host, details)
	}

	return nil
}
