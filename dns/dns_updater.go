package dns

import (
	"fmt"

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
	dns          r53.Route53Client
	elb          elb.ELB
	frontends    map[string]elb.LoadBalancerDetails
	elbLabelName string
	findElbs     findElbs
}

// New creates an updater for dns
func New(r53HostedZone, elbRegion string, elbLabelName string) controller.Updater {
	return &updater{
		dns:          r53.New(elbRegion, r53HostedZone),
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
	aRecords, err := u.dns.GetARecords()
	if err != nil {
		log.Warn("Unable to get A records from Route53. Not updating Route53.", err)
		return err
	}

	changes, err := calculateChanges(u.frontends, aRecords, update)
	if err != nil {
		return err
	}

	u.dns.UpdateRecordSets(changes)

	return nil
}

func calculateChanges(frontEnds map[string]elb.LoadBalancerDetails, aRecords []*route53.ResourceRecordSet, update controller.IngressUpdate) ([]*route53.Change, error) {
	log.Info("Current a records: ", aRecords)
	changes := []*route53.Change{}
	hostToIngresEntry := make(map[string]controller.IngressEntry)
	for _, ingressEntry := range update.Entries {
		log.Infof("Processing entry %v", ingressEntry)
		hostToIngresEntry[ingressEntry.Host+"."] = ingressEntry
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
