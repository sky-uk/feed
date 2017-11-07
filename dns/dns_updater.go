package dns

import (
	"fmt"

	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/dns/r53"
)

type hostToIngress map[string]controller.IngressEntry
type dnsDetails struct {
	dnsName      string
	hostedZoneID string
}

type consolidatedRecord struct {
	name            string
	pointsTo        string
	aliasHostedZone string
	ttl             int64
}

type updater struct {
	r53         r53.Route53Client
	schemeToDNS map[string]dnsDetails
	domain      string
	lbAdapter   LoadBalancerAdapter
}

// LoadBalancerAdapter defines operations which vary based on the type of load balancer being used for ingress.
type LoadBalancerAdapter interface {
	initialise(schemeToDNS map[string]dnsDetails) error
	newChange(action string, host string, details dnsDetails) *route53.Change
	changeExistingIfRequired(record consolidatedRecord, host string, details dnsDetails) *route53.Change
}

// New creates an updater for dns
func New(hostedZoneID string, lbAdapter LoadBalancerAdapter, region string, retries int) controller.Updater {
	initMetrics()

	return &updater{
		r53:         r53.New(region, hostedZoneID, retries),
		lbAdapter:   lbAdapter,
		schemeToDNS: make(map[string]dnsDetails),
	}
}

func (u *updater) String() string {
	return "route53 updater"
}

func (u *updater) Start() error {
	log.Info("Starting dns updater")

	if err := u.lbAdapter.initialise(u.schemeToDNS); err != nil {
		return err
	}

	domain, err := u.r53.GetHostedZoneDomain()
	if err != nil {
		return fmt.Errorf("unable to get domain for hosted zone: %v", err)
	}
	u.domain = domain

	log.Info("Dns updater started")
	return nil
}

func (u *updater) Stop() error {
	return nil
}

func (u *updater) Health() error {
	return nil
}

func (u *updater) Update(entries controller.IngressEntries) error {
	route53Records, err := u.r53.GetRecords()
	if err != nil {
		log.Warn("Unable to get records from Route53. Not updating Route53.", err)
		failedCount.Inc()
		return err
	}

	// Flatten Alias (A) and CNAME records into a common structure
	records := u.consolidateRecordsFromRoute53(route53Records)

	records = u.determineManagedRecordSets(records)
	recordsGauge.Set(float64(len(records)))

	changes := u.calculateChanges(records, entries)

	updateCount.Add(float64(len(changes)))

	err = u.r53.UpdateRecordSets(changes)
	if err != nil {
		failedCount.Inc()
		return fmt.Errorf("unable to update record sets: %v", err)
	}

	return nil
}

func (u *updater) consolidateRecordsFromRoute53(rrs []*route53.ResourceRecordSet) []consolidatedRecord {
	var records []consolidatedRecord

	for _, recordSet := range rrs {
		if *recordSet.Type == route53.RRTypeA && recordSet.AliasTarget != nil {
			records = append(records, consolidatedRecord{
				name:            *recordSet.Name,
				pointsTo:        *recordSet.AliasTarget.DNSName,
				aliasHostedZone: *recordSet.AliasTarget.HostedZoneId,
			})
		} else if *recordSet.Type == route53.RRTypeCname {
			record := consolidatedRecord{
				name:     *recordSet.Name,
				pointsTo: *recordSet.ResourceRecords[0].Value,
			}
			if recordSet.TTL != nil {
				record.ttl = *recordSet.TTL
			}
			records = append(records, record)
		}
	}

	return records
}

func (u *updater) determineManagedRecordSets(rrs []consolidatedRecord) []consolidatedRecord {
	managedLBs := make(map[string]bool)
	for _, dns := range u.schemeToDNS {
		managedLBs[dns.dnsName] = true
	}
	var managed []consolidatedRecord
	var nonManaged []string
	for _, rec := range rrs {
		if rec.name != "" && managedLBs[rec.pointsTo] {
			managed = append(managed, rec)
		} else {
			nonManaged = append(nonManaged, rec.name)
		}
	}

	if len(nonManaged) > 0 {
		log.Infof("Filtered %d non-managed resource record sets: %v", len(nonManaged), nonManaged)
	}

	return managed
}

func (u *updater) calculateChanges(originalRecords []consolidatedRecord,
	entries controller.IngressEntries) []*route53.Change {

	log.Infof("Current %s records: %v", u.domain, originalRecords)
	log.Debug("Processing ingress update: ", entries)

	hostToIngress, skipped := u.indexByHost(entries)
	changes, skipped2 := u.createChanges(hostToIngress, originalRecords)

	skipped = append(skipped, skipped2...)

	if len(skipped) > 0 {
		log.Warnf("%d skipped entries for zone '%s': %v",
			len(skipped), u.domain, skipped)
	}

	log.Debug("Host to ingress entry: ", hostToIngress)
	log.Infof("Calculated changes to dns: %v", changes)
	return changes
}

func (u *updater) indexByHost(entries []controller.IngressEntry) (hostToIngress, []string) {
	var skipped []string
	mapping := make(hostToIngress)

	for _, entry := range entries {
		log.Debugf("Processing entry %v", entry)
		// Ingress entries in k8s aren't allowed to have the . on the end.
		// AWS adds it regardless of whether you specify it.
		hostNameWithPeriod := entry.Host + "."

		log.Debugf("Checking if ingress entry hostname %s is in domain %s", hostNameWithPeriod, u.domain)
		if !strings.HasSuffix(hostNameWithPeriod, "."+u.domain) {
			skipped = append(skipped, entry.NamespaceName()+":host:"+hostNameWithPeriod)
			skippedCount.Inc()
			continue
		}

		if previous, exists := mapping[hostNameWithPeriod]; exists {
			if previous.ELbScheme != entry.ELbScheme {
				skipped = append(skipped, entry.NamespaceName()+":conflicting-scheme:"+entry.ELbScheme)
				skippedCount.Inc()
			}
		} else {
			mapping[hostNameWithPeriod] = entry
		}
	}

	return mapping, skipped
}

func (u *updater) createChanges(hostToIngress hostToIngress,
	originalRecords []consolidatedRecord) ([]*route53.Change, []string) {

	type recordKey struct{ host, elbDNSName string }
	var skipped []string
	changes := []*route53.Change{}
	indexedRecords := make(map[recordKey]consolidatedRecord)
	for _, rec := range originalRecords {
		indexedRecords[recordKey{rec.name, rec.pointsTo}] = rec
	}

	for host, entry := range hostToIngress {
		dnsDetails, exists := u.schemeToDNS[entry.ELbScheme]
		if !exists {
			skipped = append(skipped, entry.NamespaceName()+":scheme:"+entry.ELbScheme)
			skippedCount.Inc()
			continue
		}

		if existingRecord, recordExists := indexedRecords[recordKey{host, dnsDetails.dnsName}]; !recordExists {
			changes = append(changes, u.lbAdapter.newChange("UPSERT", host, dnsDetails))
		} else if changedRecord := u.lbAdapter.changeExistingIfRequired(existingRecord, host, dnsDetails); changedRecord != nil {
			changes = append(changes, changedRecord)
		}
	}

	for _, rec := range originalRecords {
		if _, contains := hostToIngress[rec.name]; !contains {
			changes = append(changes, u.lbAdapter.newChange("DELETE", rec.name, dnsDetails{rec.pointsTo, rec.aliasHostedZone}))
		}
	}

	return changes, skipped
}
