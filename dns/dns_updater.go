package dns

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/service/route53"
	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/dns/adapter"
	"github.com/sky-uk/feed/dns/r53"
)

type hostToIngress map[string]controller.IngressEntry

type updater struct {
	r53                 r53.Route53Client
	schemeToFrontendMap map[string]adapter.DNSDetails
	domain              string
	lbAdapter           adapter.FrontendAdapter
}

// New creates an updater for dns
func New(hostedZoneID string, lbAdapter adapter.FrontendAdapter, retries int) controller.Updater {
	initMetrics()

	return &updater{
		r53:                 r53.New(hostedZoneID, retries),
		lbAdapter:           lbAdapter,
		schemeToFrontendMap: make(map[string]adapter.DNSDetails),
	}
}

func (u *updater) String() string {
	return "route53 updater"
}

func (u *updater) Start() error {
	log.Info("Starting dns updater")

	schemeToFrontendMap, err := u.lbAdapter.Initialise()
	if err != nil {
		return err
	}
	u.schemeToFrontendMap = schemeToFrontendMap

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

func (u *updater) Readiness() error {
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

func (u *updater) consolidateRecordsFromRoute53(rrs []*route53.ResourceRecordSet) []adapter.ConsolidatedRecord {
	var records []adapter.ConsolidatedRecord

	for _, recordSet := range rrs {
		if record, managed := u.lbAdapter.IsManaged(recordSet); managed {
			records = append(records, *record)
		}
	}

	return records
}

func (u *updater) determineManagedRecordSets(rrs []adapter.ConsolidatedRecord) []adapter.ConsolidatedRecord {
	managedLBs := make(map[string]bool)
	for _, dns := range u.schemeToFrontendMap {
		managedLBs[dns.DNSName] = true
	}
	var managed []adapter.ConsolidatedRecord
	var nonManaged []string
	for _, rec := range rrs {
		if rec.Name != "" && managedLBs[rec.PointsTo] {
			managed = append(managed, rec)
		} else {
			nonManaged = append(nonManaged, rec.Name)
		}
	}

	if len(nonManaged) > 0 {
		log.Infof("Filtered %d non-managed resource record sets: %v", len(nonManaged), nonManaged)
	}

	return managed
}

func (u *updater) calculateChanges(originalRecords []adapter.ConsolidatedRecord,
	entries controller.IngressEntries) []*route53.Change {

	log.Infof("Current %s records: %d", u.domain, len(originalRecords))
	log.Debugf("Current %s record set: %v", u.domain, originalRecords)
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
			if previous.LbScheme != entry.LbScheme {
				skipped = append(skipped, entry.NamespaceName()+":conflicting-scheme:"+entry.LbScheme)
				skippedCount.Inc()
			}
		} else {
			mapping[hostNameWithPeriod] = entry
		}
	}

	return mapping, skipped
}

func (u *updater) createChanges(hostToIngress hostToIngress,
	originalRecords []adapter.ConsolidatedRecord) ([]*route53.Change, []string) {

	type recordKey struct{ host, elbDNSName string }
	var changes []*route53.Change
	indexedRecords := make(map[recordKey]adapter.ConsolidatedRecord)
	for _, rec := range originalRecords {
		indexedRecords[recordKey{rec.Name, rec.PointsTo}] = rec
	}

	var skipped []string
	for host, entry := range hostToIngress {
		dnsDetails, exists := u.schemeToFrontendMap[entry.LbScheme]
		if !exists {
			skipped = append(skipped, entry.NamespaceName()+":scheme:"+entry.LbScheme)
			skippedCount.Inc()
			continue
		}

		existingRecord, recordExists := indexedRecords[recordKey{host, dnsDetails.DNSName}]
		change := u.lbAdapter.CreateChange("UPSERT", host, dnsDetails, recordExists, &existingRecord)
		if change != nil {
			changes = append(changes, change)
		}
	}

	for _, rec := range originalRecords {
		if _, contains := hostToIngress[rec.Name]; !contains {
			changes = append(changes, u.lbAdapter.CreateChange("DELETE", rec.Name, adapter.DNSDetails{
				DNSName:      rec.PointsTo,
				HostedZoneID: rec.AliasHostedZone,
			}, false, nil))
		}
	}

	return changes, skipped
}
