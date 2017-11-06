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

// NewStaticHostnameAdapter creates a LoadBalancerAdapter which interacts with load balancers accessed by static hostnames.
func NewStaticHostnameAdapter(addressesWithScheme map[string]string, ttl time.Duration) LoadBalancerAdapter {
	return &staticHostnameAdapter{addressesWithScheme, aws.Int64(int64(ttl.Seconds()))}
}

func (s staticHostnameAdapter) initialise(schemeToDNS map[string]dnsDetails) error {
	for scheme, address := range s.addressesWithScheme {
		schemeToDNS[scheme] = dnsDetails{dnsName: address, hostedZoneID: ""}
	}

	return nil
}

func (s staticHostnameAdapter) newChange(action string, host string, details dnsDetails) *route53.Change {
	set := &route53.ResourceRecordSet{
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
		ResourceRecordSet: set,
	}
}
