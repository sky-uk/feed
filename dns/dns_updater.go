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

type findElbs func(elb.ELB, string) (map[string]elb.LoadBalancerDetails, error)

type updater struct {
	r53Sdk       r53.Route53Client
	elb          elb.ELB
	frontends    map[string]elb.LoadBalancerDetails
	elbLabelName string
	domain       string
	findElbs     findElbs
}

// New creates an updater for dns
func New(r53HostedZone, elbRegion string, elbLabelName string) controller.Updater {
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
	frontEnds, err := u.findElbs(u.elb, u.elbLabelName)
	if err != nil {
		return fmt.Errorf("unable to find front end load balancers: %v", err)
	}
	u.frontends = frontEnds
	u.domain, err = u.r53Sdk.GetHostedZoneDomain()

	if err != nil {
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
func calculateChanges(frontEnds map[string]elb.LoadBalancerDetails,
	aRecords []*route53.ResourceRecordSet,
	update controller.IngressUpdate,
	domain string) []*route53.Change {

	log.Info("Current a records: ", aRecords)
	log.Debug("Processing ingress update: ", update)
	changes := []*route53.Change{}

	var invalidIngresses []string
	hostToIngressEntries := make(map[string]controller.IngressEntry)

	for _, ingressEntry := range update.Entries {
		log.Debugf("Processing entry %v", ingressEntry)
		// Ingress entries in k8s aren't allowed to have the . on the end
		// AWS adds it regardless of whether you specify it
		hostNameWithPeriod := ingressEntry.Host + "."
		// Want to match blah.james.com not blahjames.com for domain james.com

		log.Debugf("Checking if ingress entry has valid host name (%s, %s)", hostNameWithPeriod, domain)
		// First we check if this host is actually in the hosted zone's domain
		if !strings.HasSuffix(hostNameWithPeriod, "."+domain) {
			invalidIngresses = append(invalidIngresses, ingressEntry.Name+":host:"+hostNameWithPeriod)
			invalidIngressCount.Inc()
			continue
		}

		hostToIngressEntries[hostNameWithPeriod] = ingressEntry
		frontEnd, exists := frontEnds[ingressEntry.ELbScheme]
		if !exists {
			invalidIngresses = append(invalidIngresses, ingressEntry.Name+":scheme:"+ingressEntry.ELbScheme)
			invalidIngressCount.Inc()
			continue
		}

		changes = append(changes,
			newChange("UPSERT", ingressEntry.Host, frontEnd.DNSName, frontEnd.HostedZoneID))
	}

	if len(invalidIngresses) > 0 {
		log.Warnf("%d invalid entries for zone '%s': %v",
			len(invalidIngresses), domain, invalidIngresses)
	}

	log.Debug("Host to ingress entry: ", hostToIngressEntries)

	for _, recordSet := range aRecords {
		if _, contains := hostToIngressEntries[*recordSet.Name]; !contains {
			changes = append(changes, newChange(
				"DELETE",
				*recordSet.Name,
				*recordSet.AliasTarget.DNSName,
				*recordSet.AliasTarget.HostedZoneId))
		}
	}

	log.Infof("Calculated changes to dns: %v", changes)
	return changes
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
