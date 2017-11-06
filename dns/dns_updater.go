package dns

import (
	"fmt"

	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	aws_elb "github.com/aws/aws-sdk-go/service/elb"
	aws_alb "github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/dns/r53"
	"github.com/sky-uk/feed/elb"
)

type hostToIngress map[string]controller.IngressEntry
type findELBsFunc func(elb.ELB, string) (map[string]elb.LoadBalancerDetails, error)
type dnsDetails struct {
	dnsName      string
	hostedZoneID string
}

type record struct {
	name            string
	pointsTo        string
	aliasHostedZone string
}

type updater struct {
	r53           r53.Route53Client
	hostedZoneID  string
	elb           elb.ELB
	alb           ALB
	schemeToDNS   map[string]dnsDetails
	albNames      []string
	elbLabelValue string
	domain        string
	findELBs      findELBsFunc
}

// ALB represents the subset of AWS operations needed for dns_updater.go
type ALB interface {
	DescribeLoadBalancers(input *aws_alb.DescribeLoadBalancersInput) (*aws_alb.DescribeLoadBalancersOutput, error)
}

// New creates an updater for dns
func New(hostedZoneID, region string, addressesWithScheme map[string]string, elbLabelValue string, albNames []string, retries int) controller.Updater {
	initMetrics()
	session := session.New(&aws.Config{Region: &region})

	schemeToDNS := make(map[string]dnsDetails)
	for scheme, address := range addressesWithScheme {
		schemeToDNS[scheme] = dnsDetails{dnsName: address, hostedZoneID: ""}
	}

	return &updater{
		r53:           r53.New(region, hostedZoneID, retries),
		hostedZoneID:  hostedZoneID,
		elb:           aws_elb.New(session),
		alb:           aws_alb.New(session),
		albNames:      albNames,
		elbLabelValue: elbLabelValue,
		findELBs:      elb.FindFrontEndElbs,
		schemeToDNS:   schemeToDNS,
	}
}

func (u *updater) String() string {
	return "route53 updater"
}

func (u *updater) Start() error {
	log.Info("Starting dns updater")

	if len(u.schemeToDNS) == 0 {
		if u.elbLabelValue != "" && len(u.albNames) > 0 {
			return fmt.Errorf("can't specify both elb label value (%s) and alb names (%v) - only one or the other may be"+
				" specified", u.elbLabelValue, u.albNames)
		}

		if err := u.initELBs(); err != nil {
			return err
		}

		if err := u.initALBs(); err != nil {
			return err
		}
	}

	domain, err := u.r53.GetHostedZoneDomain()
	if err != nil {
		return fmt.Errorf("unable to get domain for hosted zone: %v", err)
	}
	u.domain = domain

	log.Info("Dns updater started")
	return nil
}

func (u *updater) initELBs() error {
	if u.elbLabelValue == "" {
		return nil
	}

	elbs, err := u.findELBs(u.elb, u.elbLabelValue)
	if err != nil {
		return fmt.Errorf("unable to find front end load balancers: %v", err)
	}

	u.schemeToDNS = make(map[string]dnsDetails)
	for scheme, lbDetails := range elbs {
		if strings.HasSuffix(lbDetails.DNSName, ".") {
			return fmt.Errorf("unexpected trailing dot on load balancer DNS name: %s", lbDetails.DNSName)
		}

		u.schemeToDNS[scheme] = dnsDetails{dnsName: lbDetails.DNSName + ".", hostedZoneID: lbDetails.HostedZoneID}
	}

	return nil
}

func (u *updater) initALBs() error {
	if len(u.albNames) == 0 {
		return nil
	}

	u.schemeToDNS = make(map[string]dnsDetails)
	req := &aws_alb.DescribeLoadBalancersInput{Names: aws.StringSlice(u.albNames)}

	for {
		resp, err := u.alb.DescribeLoadBalancers(req)
		if err != nil {
			return err
		}

		for _, lb := range resp.LoadBalancers {
			u.schemeToDNS[*lb.Scheme] = dnsDetails{dnsName: *lb.DNSName + ".", hostedZoneID: *lb.CanonicalHostedZoneId}
		}

		if resp.NextMarker == nil {
			break
		}

		req.Marker = resp.NextMarker
	}

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
	records := u.flattenRoute53ResourceRecordSet(route53Records)

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

func (u *updater) flattenRoute53ResourceRecordSet(rrs []*route53.ResourceRecordSet) []record {
	var records []record

	for _, recordSet := range rrs {
		if len(recordSet.ResourceRecords) == 1 {
			records = append(records, record{
				name:     *recordSet.Name,
				pointsTo: *recordSet.ResourceRecords[0].Value,
			})
		} else {
			records = append(records, record{
				name:            *recordSet.Name,
				pointsTo:        *recordSet.AliasTarget.DNSName,
				aliasHostedZone: *recordSet.AliasTarget.HostedZoneId,
			})
		}
	}

	return records
}

func (u *updater) determineManagedRecordSets(rrs []record) []record {
	managedLBs := make(map[string]bool)
	for _, dns := range u.schemeToDNS {
		managedLBs[dns.dnsName] = true
	}
	var managed []record
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

func (u *updater) calculateChanges(originalRecords []record,
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
	originalRecords []record) ([]*route53.Change, []string) {

	type recordKey struct{ host, elbDNSName string }
	var skipped []string
	changes := []*route53.Change{}
	indexedRecords := make(map[recordKey]record)
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

		if _, recordExists := indexedRecords[recordKey{host, dnsDetails.dnsName}]; !recordExists {
			changes = append(changes, newChange("UPSERT", host, dnsDetails.dnsName, dnsDetails.hostedZoneID))
		}
	}

	for _, rec := range originalRecords {
		if _, contains := hostToIngress[rec.name]; !contains {
			changes = append(changes, newChange("DELETE", rec.name, rec.pointsTo, rec.aliasHostedZone))
		}
	}

	return changes, skipped
}

func newChange(action string, host string, targetElbDNSName string, targetElbHostedZoneID string) *route53.Change {
	set := &route53.ResourceRecordSet{
		Name: aws.String(host),
	}

	if targetElbHostedZoneID != "" {
		set.Type = aws.String("A")
		set.AliasTarget = &route53.AliasTarget{
			DNSName:      aws.String(targetElbDNSName),
			HostedZoneId: aws.String(targetElbHostedZoneID),
			// disable this since we only point to a single load balancer
			EvaluateTargetHealth: aws.Bool(false),
		}
	} else {
		ttl := int64(300)
		set.Type = aws.String("CNAME")
		set.TTL = &ttl
		set.ResourceRecords = []*route53.ResourceRecord{
			{
				Value: aws.String(targetElbDNSName),
			},
		}
	}

	return &route53.Change{
		Action:            aws.String(action),
		ResourceRecordSet: set,
	}
}
