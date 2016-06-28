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
		return err
	}

	changes, err := calculateChanges(u.frontends, aRecords, update, u.domain)
	if err != nil {
		return err
	}

	u.r53Sdk.UpdateRecordSets(changes)

	return nil
}

// a private function rather than a method on updater to allow isolated testing, however...
// todo make a private method and test through the public interface
func calculateChanges(frontEnds map[string]elb.LoadBalancerDetails,
	aRecords []*route53.ResourceRecordSet,
	update controller.IngressUpdate,
	domain string) ([]*route53.Change, error) {
	log.Info("Current a records: ", aRecords)
	log.Info("Processing ingress update: ", update)
	changes := []*route53.Change{}
	hostToIngresEntry := make(map[string]controller.IngressEntry)
	for _, ingressEntry := range update.Entries {
		log.Infof("Processing entry %v", ingressEntry)
		// Ingress entries in k8s aren't allowed to have the . on the end
		// AWS adds it regardless of whether you specify it
		hostNameWithPeriod := ingressEntry.Host + "."
		// Want to match blah.james.com not blahjames.com for domain james.com
		domainWithLeadingPeriod := "." + domain

		log.Infof("Checking if ingress entry has valid host name (%s, %s)", hostNameWithPeriod, domainWithLeadingPeriod)
		// First we check if this host is actually in the hosted zone's domain
		if !strings.HasSuffix(hostNameWithPeriod, domainWithLeadingPeriod) {
			log.Warnf("Ingress entry does not have a valid hostname for the hosted zone (%s, %s)", hostNameWithPeriod, domainWithLeadingPeriod)
			break
		}

		hostToIngresEntry[hostNameWithPeriod] = ingressEntry
		frontEnd, exists := frontEnds[ingressEntry.ELbScheme]
		if !exists {
			return nil, fmt.Errorf("unable to find front end load balancer with scheme: %v", ingressEntry.ELbScheme)
		}
		changes = append(changes,
			newChange("UPSERT", ingressEntry.Host, frontEnd.DNSName, frontEnd.HostedZoneID))
	}

	log.Info("Host to ingress entry: ", hostToIngresEntry)

	for _, recordSet := range aRecords {
		if _, contains := hostToIngresEntry[*recordSet.Name]; !contains {
			changes = append(changes, newChange(
				"DELETE",
				*recordSet.Name,
				*recordSet.AliasTarget.DNSName,
				*recordSet.AliasTarget.HostedZoneId))
		}
	}

	log.Infof("calculated changes to dns: %v", changes)
	return changes, nil
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
