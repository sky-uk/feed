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
	onlyDelAssoc  bool
}

// ALB represents the subset of AWS operations needed for dns_updater.go
type ALB interface {
	DescribeLoadBalancers(input *aws_alb.DescribeLoadBalancersInput) (*aws_alb.DescribeLoadBalancersOutput, error)
}

// New creates an updater for dns
func New(hostedZoneID, region string, elbLabelValue string, albNames []string, retries int, onlyDelAssoc bool) controller.Updater {
	initMetrics()
	session := session.New(&aws.Config{Region: &region})
	return &updater{
		r53:           r53.New(region, hostedZoneID, retries),
		hostedZoneID:  hostedZoneID,
		elb:           aws_elb.New(session),
		alb:           aws_alb.New(session),
		albNames:      albNames,
		elbLabelValue: elbLabelValue,
		findELBs:      elb.FindFrontEndElbs,
		onlyDelAssoc:  onlyDelAssoc,
	}
}

func (u *updater) String() string {
	return "route53 updater"
}

func (u *updater) Start() error {
	log.Info("Starting dns updater")

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
		u.schemeToDNS[scheme] = dnsDetails{dnsName: lbDetails.DNSName, hostedZoneID: lbDetails.HostedZoneID}
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

func (u *updater) Update(update controller.IngressUpdate) error {
	aRecords, err := u.r53.GetARecords()
	if err != nil {
		log.Warn("Unable to get A records from Route53. Not updating Route53.", err)
		failedCount.Inc()
		return err
	}

	if u.onlyDelAssoc {
		aRecords = u.filterUnassociated(aRecords)
	}
	recordsGauge.Set(float64(len(aRecords)))

	changes := u.calculateChanges(aRecords, update)

	updateCount.Add(float64(len(changes)))

	err = u.r53.UpdateRecordSets(changes)
	if err != nil {
		failedCount.Inc()
		return fmt.Errorf("unable to update record sets: %v", err)
	}

	return nil
}

func (u *updater) filterUnassociated(aRecords []*route53.ResourceRecordSet) []*route53.ResourceRecordSet {
	managedLBs := make(map[string]bool)
	for _, dns := range u.schemeToDNS {
		managedLBs[dns.dnsName] = true
	}
	filtered := make([]*route53.ResourceRecordSet, 0)
	for _, rec := range aRecords {
		if rec.AliasTarget != nil && rec.AliasTarget.DNSName != nil && managedLBs[*rec.AliasTarget.DNSName] {
			log.Debugf("Keeping managed A record %s", *rec.Name)
			filtered = append(filtered, rec)
		} else {
			log.Infof("Filtering non-managed A record %s", *rec.Name)
		}
	}

	return filtered
}

func (u *updater) calculateChanges(originalRecords []*route53.ResourceRecordSet,
	update controller.IngressUpdate) []*route53.Change {

	log.Infof("Current %s records: %v", u.domain, originalRecords)
	log.Debug("Processing ingress update: ", update)

	hostToIngress, skipped := u.indexByHost(update.Entries)
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
	originalRecords []*route53.ResourceRecordSet) ([]*route53.Change, []string) {

	type recordKey struct{ host, elbDNSName string }
	var skipped []string
	changes := []*route53.Change{}
	indexedRecords := make(map[recordKey]*route53.ResourceRecordSet)
	for _, recordSet := range originalRecords {
		indexedRecords[recordKey{*recordSet.Name, *recordSet.AliasTarget.DNSName}] = recordSet
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

	for _, recordSet := range originalRecords {
		if _, contains := hostToIngress[*recordSet.Name]; !contains {
			changes = append(changes, newChange(
				"DELETE",
				*recordSet.Name,
				*recordSet.AliasTarget.DNSName,
				*recordSet.AliasTarget.HostedZoneId))
		}
	}

	return changes, skipped
}

func newChange(action string, host string, targetElbDNSName string, targetElbHostedZoneID string) *route53.Change {
	return &route53.Change{
		Action: aws.String(action),
		ResourceRecordSet: &route53.ResourceRecordSet{
			Name: aws.String(host),
			Type: aws.String("A"),
			AliasTarget: &route53.AliasTarget{
				DNSName:      aws.String(targetElbDNSName),
				HostedZoneId: aws.String(targetElbHostedZoneID),
				// disable this since we only point to a single load balancer
				EvaluateTargetHealth: aws.Bool(false),
			},
		},
	}
}
