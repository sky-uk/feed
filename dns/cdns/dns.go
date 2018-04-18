package cdns

import (
	"context"
	"fmt"

	"github.com/sky-uk/feed/gclb"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/dns/v1"
)

// NewDNSClient creates a GCP client
func NewDNSClient() (DNSClient, error) {
	ctx := context.Background()
	client, err := google.DefaultClient(ctx, dns.CloudPlatformScope)
	if err != nil {
		return nil, fmt.Errorf("unable to create http client with delegate scope: %v", err)
	}
	computeService, err := compute.New(client)
	if err != nil {
		return nil, fmt.Errorf("unable to create delegate compute service client: %v", err)
	}
	dnsService, err := dns.New(client)
	if err != nil {
		return nil, fmt.Errorf("unable to create delegate dns service client: %v", err)
	}
	return &dnsClient{
		addresses:   computeService.GlobalAddresses,
		changes:     dnsService.Changes,
		records:     dnsService.ResourceRecordSets,
		zones:       dnsService.ManagedZones,
		GCPMetadata: gclb.NewMetadata(),
	}, err
}

// DNSClient is an interface to allow mocking of real calls to GCP as well as
// restricting underlying API to only the methods we use.
type DNSClient interface {
	ListGlobalAddresses(project, page string) (*compute.AddressList, error)
	ListRecords(project, managedZoneName, page string) (*dns.ResourceRecordSetsListResponse, error)
	GetManagedZone(project, managedZoneName string) (*dns.ManagedZone, error)
	ApplyChange(project, managedZoneName string, change *dns.Change) error
	gclb.GCPMetadata
}

type dnsClient struct {
	addresses *compute.GlobalAddressesService
	changes   *dns.ChangesService
	records   *dns.ResourceRecordSetsService
	zones     *dns.ManagedZonesService
	gclb.GCPMetadata
}

func (d *dnsClient) ListGlobalAddresses(project, page string) (*compute.AddressList, error) {
	request := d.addresses.List(project)
	if page != "" {
		request.PageToken(page)
	}
	return request.Do()
}

func (d *dnsClient) ListRecords(project, managedZoneName, page string) (*dns.ResourceRecordSetsListResponse, error) {
	request := d.records.List(project, managedZoneName)
	if page != "" {
		request.PageToken(page)
	}
	return request.Do()
}

func (d *dnsClient) ApplyChange(project, managedZoneName string, change *dns.Change) error {
	_, err := d.changes.Create(project, managedZoneName, change).Do()
	return err
}

func (d *dnsClient) GetManagedZone(project, managedZoneName string) (*dns.ManagedZone, error) {
	return d.zones.Get(project, managedZoneName).Do()
}
