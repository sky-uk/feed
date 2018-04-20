package cdns

import (
	"errors"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/util"
	"google.golang.org/api/dns/v1"
)

const (
	internetFacingScheme = "internet-facing"
	upsert               = "UPSERT"
	delete               = "DELETE"
	maxRecordChanges     = 100
)

// Config for the Cloud DNS adapter
type Config struct {
	InstanceGroupPrefix string
	ManagedZone         string
}

//NewUpdater for the Google Cloud DNS
func NewUpdater(config Config) (controller.Updater, error) {
	client, err := NewDNSClient()
	if err != nil {
		return nil, err
	}
	return &dnsUpdater{
		config:    config,
		dnsClient: client,
	}, nil
}

type dnsUpdater struct {
	dnsClient           DNSClient
	config              Config
	schemeToFrontendMap map[string]lbDetails
	projectID           string
	managedZone         *dns.ManagedZone
}

func (u *dnsUpdater) Start() error {
	log.Infof("Starting %s", u)
	projectID, err := u.dnsClient.ProjectID()
	if err != nil {
		return fmt.Errorf("unable to retrieve project id: %v", err)
	}
	u.projectID = projectID
	managedZone, err := u.dnsClient.GetManagedZone(projectID, u.config.ManagedZone)
	if err != nil {
		return fmt.Errorf("unable to retrieve the managed zone %q details: %v", u.config.ManagedZone, err)
	}
	u.managedZone = managedZone

	schemeToFrontendMap, err := u.getLoadBalancers()
	if err != nil {
		return fmt.Errorf("unable to initialise updater: %v", err)
	}
	u.schemeToFrontendMap = schemeToFrontendMap
	return nil
}

func (u *dnsUpdater) getLoadBalancers() (map[string]lbDetails, error) {
	externalLB, err := u.getExternalLoadBalancer(u.projectID, u.config.InstanceGroupPrefix)
	if err != nil {
		return nil, err
	}
	details := make(map[string]lbDetails)
	if externalLB != nil {
		details[externalLB.Type] = *externalLB
	}
	return details, nil
}

// Stop is a no-op as there is nothing that needs to be stopped
func (u *dnsUpdater) Stop() error {
	return nil
}

func (u *dnsUpdater) Update(entries controller.IngressEntries) error {
	records, err := u.getExistingRecords()
	if err != nil {
		return err
	}
	records = u.collectManagedRecords(records)
	changes := u.calculateChanges(records, entries)
	err = u.updateRecordSets(changes)
	if err != nil {
		return fmt.Errorf("unable to update record sets: %v", err)
	}

	return nil
}

func (u *dnsUpdater) updateRecordSets(changes []*dns.Change) error {
	failed := false
	for _, change := range changes {
		if err := u.dnsClient.ApplyChange(u.projectID, u.config.ManagedZone, change); err != nil {
			log.Errorf("unable to create A records %q: %v", change.Additions, err)
			failed = true
		}
	}
	if failed {
		return errors.New("unable to create one or more A records")
	}
	return nil
}

func (u *dnsUpdater) calculateChanges(records []*dns.ResourceRecordSet, entries controller.IngressEntries) []*dns.Change {
	log.Infof("Current %s records: %d", u.managedZone.DnsName, len(records))
	log.Debugf("Current %s record set: %v", u.managedZone.DnsName, records)
	log.Debug("Processing ingress update: ", entries)

	hostToIngress, skipped := u.indexByHost(entries)
	changes, skipped2 := u.createChanges(hostToIngress, records)

	skipped = append(skipped, skipped2...)

	if len(skipped) > 0 {
		log.Warnf("%d skipped entries for zone '%s': %v",
			len(skipped), u.managedZone.DnsName, skipped)
	}

	log.Debug("Host to ingress entry: ", hostToIngress)
	log.Infof("Calculated changes to dns: %v", changes)
	return changes
}

func (u *dnsUpdater) createChanges(hostToIngress hostToIngress,
	records []*dns.ResourceRecordSet) ([]*dns.Change, []string) {

	type recordKey struct{ host, targetLB string }
	indexedRecords := make(map[recordKey]*dns.ResourceRecordSet)
	for _, rec := range records {
		indexedRecords[recordKey{rec.Name, rec.Rrdatas[0]}] = rec
	}

	var skipped []string
	var changes []*dns.Change
	var additions []*dns.ResourceRecordSet
	var deletions []*dns.ResourceRecordSet
	for host, entry := range hostToIngress {
		dnsDetails, exists := u.schemeToFrontendMap[entry.ELbScheme]
		if !exists {
			skipped = append(skipped, entry.NamespaceName()+":scheme:"+entry.ELbScheme)
			continue
		}

		record, recordExists := indexedRecords[recordKey{host, dnsDetails.IP}]
		addition, deletion := u.createChange(upsert, host, dnsDetails, recordExists, record)
		additions = append(additions, addition)
		if deletion != nil {
			deletions = append(deletions, deletion)
		}
		if len(additions)+len(deletions)+1 >= maxRecordChanges {
			changes = append(changes, &dns.Change{
				Additions: additions,
				Deletions: deletions,
			})
			additions = nil
			deletions = nil
		}
	}
	if len(additions) > 0 || len(deletions) > 0 {
		changes = append(changes, &dns.Change{
			Additions: additions,
			Deletions: deletions,
		})
	}
	additions = nil
	deletions = nil
	for _, rec := range records {
		if _, contains := hostToIngress[rec.Name]; !contains {
			_, deletion := u.createChange(delete, rec.Name, lbDetails{}, false, nil)
			deletions = append(deletions, deletion)
		}
	}
	partitions := util.Partition(len(deletions), maxRecordChanges)
	for _, partition := range partitions {
		batch := deletions[partition.Low:partition.High]
		changes = append(changes, &dns.Change{Deletions: batch})
	}
	return changes, skipped
}

func (u *dnsUpdater) createChange(action string, host string, dnsDetails lbDetails, recordExists bool, record *dns.ResourceRecordSet) (addition *dns.ResourceRecordSet, deletion *dns.ResourceRecordSet) {
	if action == upsert {
		//TODO: verify ttl
		addition = &dns.ResourceRecordSet{
			Name:    host,
			Type:    "A",
			Ttl:     300,
			Rrdatas: []string{dnsDetails.IP},
		}
	}
	if recordExists {
		// To update we must also delete
		deletion = record
	}
	return addition, deletion
}

type hostToIngress map[string]controller.IngressEntry

func (u *dnsUpdater) indexByHost(entries []controller.IngressEntry) (hostToIngress, []string) {
	var skipped []string
	mapping := make(hostToIngress)

	for _, entry := range entries {
		log.Debugf("Processing entry %v", entry)
		// Ingress entries in k8s aren't allowed to have the . on the end.
		// GCP adds it regardless of whether you specify it.
		hostNameWithPeriod := entry.Host + "."

		log.Debugf("Checking if ingress entry hostname %s is in domain %s", hostNameWithPeriod, u.managedZone.DnsName)
		if !strings.HasSuffix(hostNameWithPeriod, "."+u.managedZone.DnsName) {
			skipped = append(skipped, entry.NamespaceName()+":host:"+hostNameWithPeriod)
			continue
		}

		if previous, exists := mapping[hostNameWithPeriod]; exists {
			if previous.ELbScheme != entry.ELbScheme {
				skipped = append(skipped, entry.NamespaceName()+":conflicting-scheme:"+entry.ELbScheme)
			}
		} else {
			mapping[hostNameWithPeriod] = entry
		}
	}
	return mapping, skipped
}

func (u *dnsUpdater) collectManagedRecords(records []*dns.ResourceRecordSet) []*dns.ResourceRecordSet {
	managedLBs := make(map[string]bool)
	for _, dns := range u.schemeToFrontendMap {
		managedLBs[dns.IP] = true
	}
	var managed []*dns.ResourceRecordSet
	var nonManaged []string
	for _, record := range records {
		if managedLBs[record.Rrdatas[0]] {
			managed = append(managed, record)
		} else {
			nonManaged = append(nonManaged, record.Name)
		}
	}
	if len(nonManaged) > 0 {
		log.Infof("Filtered %d non-managed resource record sets: %v", len(nonManaged), nonManaged)
	}

	return managed
}

func (u *dnsUpdater) getExistingRecords() ([]*dns.ResourceRecordSet, error) {
	page := ""
	var records []*dns.ResourceRecordSet
	for {
		recordSet, err := u.dnsClient.ListRecords(u.projectID, u.config.ManagedZone, page)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve records for project's %q managed zone %q: %v", u.projectID, u.config.ManagedZone, err)
		}
		for _, record := range recordSet.Rrsets {
			if record.Type == "A" && len(record.Rrdatas) == 1 && record.Name != "" {
				records = append(records, record)
			}
		}
		if page = recordSet.NextPageToken; page == "" {
			break
		}
	}
	return records, nil
}

func (u *dnsUpdater) Health() error {
	return nil
}

func (u *dnsUpdater) String() string {
	return "cdns updater"
}

func (u *dnsUpdater) getExternalLoadBalancer(project string, prefix string) (*lbDetails, error) {
	lbName := fmt.Sprintf("%s-%s", prefix, internetFacingScheme)
	page := ""
	for {
		addressList, err := u.dnsClient.ListGlobalAddresses(project, page)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve the list of global addresses for project %q: %v", project, err)
		}
		for _, address := range addressList.Items {
			if address.Name == lbName {
				lb := &lbDetails{
					Name: lbName,
					Type: internetFacingScheme,
					IP:   address.Address,
				}
				log.Infof("Found external load balancer: %s", lb)
				return lb, nil
			}
		}
		if page = addressList.NextPageToken; page == "" {
			break
		}
	}
	log.Infof("No external load balancer found.")
	return nil, nil
}

type lbDetails struct {
	Name string
	IP   string
	Type string
}

func (l *lbDetails) String() string {
	return fmt.Sprintf("lb: {Name: %s, Type:%s, IP:%s}", l.Name, l.Type, l.IP)
}

