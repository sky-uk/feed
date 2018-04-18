package cdns

import (
	"github.com/sky-uk/feed/gclb"
	"github.com/sky-uk/feed/dns/adapter"
	"github.com/aws/aws-sdk-go/service/route53"
	"fmt"
	"errors"
	"regexp"
)

// Config for the Cloud DNS adapter
type Config struct {
	InstanceGroupPrefix string
	HostedZone          string
}

// NewAdapter creates an adapter for Cloud DNS
func NewAdapter(config Config) (adapter.FrontendAdapter, error) {
	client, err := gclb.NewClient()
	if err != nil {
		return nil, err
	}
	return &cdnsAdapter{
		Client: client,
		config: config,
	}, nil
}

type cdnsAdapter struct {
	*gclb.Client
	config Config
	instance *gclb.Instance
}

func (a *cdnsAdapter) Initialise() (map[string]adapter.DNSDetails, error) {
	instance, err := a.GetSelfMetadata()
	if err != nil {
		return nil, err
	}
	a.instance = instance
	igs, err := a.FindFrontEndInstanceGroups(instance.Project, instance.Zone, a.config.InstanceGroupPrefix)
	if err != nil {
		return nil, err
	}
	schemeToFrontendMap := make(map[string]adapter.DNSDetails)
	for _, ig := range igs {

		scheme, err := getSchemeFromName(ig.Name, a.config.InstanceGroupPrefix)
		if err!= nil {
			return nil, err
		}
		schemeToFrontendMap[scheme] = adapter.DNSDetails{DNSName: lbDetails.DNSName + ".", HostedZoneID: lbDetails.HostedZoneID}
	}

	return nil, errors.New("not implemented")
}

func getSchemeFromName(instanceGroupName, instanceGroupPrefix string) (string, error) {
	r, _ := regexp.Compile(fmt.Sprintf("%s(%s|%s).*", instanceGroupPrefix, "internet-facing", "internal"))
	groups := r.FindStringSubmatch(instanceGroupName)
	if len(groups) != 2 {
		return "", fmt.Errorf("the instance group %q name does not match the expected pattern: %s-(internet-facing|internal).*", instanceGroupName, instanceGroupPrefix)
	}
	return groups[1], nil
}

func (a *cdnsAdapter) CreateChange(action string, host string, details adapter.DNSDetails, recordExists bool, existingRecord *adapter.ConsolidatedRecord) *route53.Change {
	return nil
}

func (a *cdnsAdapter) IsManaged(*route53.ResourceRecordSet) (*adapter.ConsolidatedRecord, bool) {
	return nil, false
}
