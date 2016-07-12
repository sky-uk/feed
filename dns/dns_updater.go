package dns

import (
	"fmt"

	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	aws_elb "github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/dns/r53"
	"github.com/sky-uk/feed/elb"
)

type frontends map[string]elb.LoadBalancerDetails
type hostToIngress map[string]controller.IngressEntry
type findElbs func(elb.ELB, string) (map[string]elb.LoadBalancerDetails, error)

type updater struct {
	r53Sdk       r53.Route53Client
	elb          elb.ELB
	frontends    frontends
	elbLabelName string
	domain       string
	findElbs     findElbs
}

// New creates an updater for dns
func New(r53HostedZone, elbRegion string, elbLabelName string) controller.Updater {
	initMetrics()
	return &updater{
		r53Sdk:       r53.New(elbRegion, r53HostedZone),
		elb:          aws_elb.New(session.New(&aws.Config{Region: &elbRegion})),
		elbLabelName: elbLabelName,
		findElbs:     elb.FindFrontEndElbs,
	}
}

func (u *updater) String() string {
	return "route53 updater"
}

func (u *updater) Start() error {
	log.Info("Starting dns updater")
	frontends, err := u.findElbs(u.elb, u.elbLabelName)
	if err != nil {
		return fmt.Errorf("unable to find front end load balancers: %v", err)
	}
	u.frontends = frontends

	if u.domain, err = u.r53Sdk.GetHostedZoneDomain(); err != nil {
		return fmt.Errorf("unable to get domain for hosted zone: %v", err)
	}

	log.Info("Dns updater started")
	return nil
}

func (u *updater) Stop() error {
	return nil
}

func (u *updater) Health() error {
	return nil
}

func (u *updater) Update(update controller.IngressUpdate) error {
	aRecords, err := u.r53Sdk.GetARecords()
	if err != nil {
		log.Warn("Unable to get A records from Route53. Not updating Route53.", err)
		failedCount.Inc()
		return err
	}
	recordsGauge.Set(float64(len(aRecords)))

	changes := calculateChanges(u.frontends, aRecords, update, u.domain)

	updateCount.Add(float64(len(changes)))

	err = u.r53Sdk.UpdateRecordSets(changes)
	if err != nil {
		failedCount.Inc()
		return fmt.Errorf("unable to update record sets: %v", err)
	}

	return nil
}

// todo (chbatey) make a private method and test through the public interface
func calculateChanges(frontEnds frontends, originalRecords []*route53.ResourceRecordSet,
	update controller.IngressUpdate, domain string) []*route53.Change {

	log.Infof("Current %s records: %v", domain, originalRecords)
	log.Debug("Processing ingress update: ", update)

	hostToIngress, skipped := indexByHost(update.Entries, domain)
	changes, skipped2 := createChanges(hostToIngress, originalRecords, frontEnds)

	skipped = append(skipped, skipped2...)

	if len(skipped) > 0 {
		log.Warnf("%d skipped entries for zone '%s': %v",
			len(skipped), domain, skipped)
	}

	log.Debug("Host to ingress entry: ", hostToIngress)
	log.Infof("Calculated changes to dns: %v", changes)
	return changes
}

func indexByHost(entries []controller.IngressEntry, domain string) (hostToIngress, []string) {
	var skipped []string
	mapping := make(hostToIngress)

	for _, entry := range entries {
		log.Debugf("Processing entry %v", entry)
		// Ingress entries in k8s aren't allowed to have the . on the end.
		// AWS adds it regardless of whether you specify it.
		hostNameWithPeriod := entry.Host + "."

		log.Debugf("Checking if ingress entry hostname %s is in domain %s", hostNameWithPeriod, domain)
		if !strings.HasSuffix(hostNameWithPeriod, "."+domain) {
			skipped = append(skipped, entry.Name+":host:"+hostNameWithPeriod)
			skippedCount.Inc()
			continue
		}

		if previous, exists := mapping[hostNameWithPeriod]; exists {
			if previous.ELbScheme != entry.ELbScheme {
				skipped = append(skipped, entry.Name+":conflicting-scheme:"+entry.ELbScheme)
				skippedCount.Inc()
			}
		} else {
			mapping[hostNameWithPeriod] = entry
		}
	}

	return mapping, skipped
}

func createChanges(hostToIngress hostToIngress, originalRecords []*route53.ResourceRecordSet,
	frontEnds frontends) ([]*route53.Change, []string) {

	var skipped []string
	changes := []*route53.Change{}

	for host, entry := range hostToIngress {
		frontEnd, exists := frontEnds[entry.ELbScheme]
		if !exists {
			skipped = append(skipped, entry.Name+":scheme:"+entry.ELbScheme)
			skippedCount.Inc()
			continue
		}

		changes = append(changes, newChange("UPSERT", host, frontEnd.DNSName, frontEnd.HostedZoneID))
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
				DNSName:              aws.String(targetElbDNSName),
				HostedZoneId:         aws.String(targetElbHostedZoneID),
				EvaluateTargetHealth: aws.Bool(true),
			},
		},
	}
}
