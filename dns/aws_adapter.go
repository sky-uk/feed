package dns

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	aws_elb "github.com/aws/aws-sdk-go/service/elb"
	aws_alb "github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/route53"

	"strings"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/sky-uk/feed/elb"
)

type findELBsFunc func(elb.ELB, string) (map[string]elb.LoadBalancerDetails, error)

type awsAdapter struct {
	hostedZoneID     *string
	elbLabelValue    string
	albNames         []string
	elb              elb.ELB
	alb              ALB
	findFrontEndElbs findELBsFunc
}

// ALB represents the subset of AWS operations needed for dns_updater.go
type ALB interface {
	DescribeLoadBalancers(input *aws_alb.DescribeLoadBalancersInput) (*aws_alb.DescribeLoadBalancersOutput, error)
}

// NewAWSAdapter a LoadBalancerAdapter which interacts with AWS ELBs or ALBs.
func NewAWSAdapter(region string, hostedZoneID string, elbLabelValue string, albNames []string) (LoadBalancerAdapter, error) {
	session, err := session.NewSession(&aws.Config{Region: &region})
	if err != nil {
		return nil, fmt.Errorf("unable to open AWS session: %v", err)
	}

	return &awsAdapter{
		hostedZoneID:     aws.String(hostedZoneID),
		elbLabelValue:    elbLabelValue,
		albNames:         albNames,
		elb:              aws_elb.New(session),
		alb:              aws_alb.New(session),
		findFrontEndElbs: elb.FindFrontEndElbs,
	}, nil
}

func (a awsAdapter) newChange(action string, host string, details dnsDetails) *route53.Change {
	set := &route53.ResourceRecordSet{
		Name: aws.String(host),
	}

	set.Type = aws.String("A")
	set.AliasTarget = &route53.AliasTarget{
		DNSName:      aws.String(details.dnsName),
		HostedZoneId: aws.String(details.hostedZoneID),
		// disable this since we only point to a single load balancer
		EvaluateTargetHealth: aws.Bool(false),
	}

	return &route53.Change{
		Action:            aws.String(action),
		ResourceRecordSet: set,
	}
}

func (a awsAdapter) initialise(schemeToDNS map[string]dnsDetails) error {
	if a.elbLabelValue != "" && len(a.albNames) > 0 {
		return fmt.Errorf("can't specify both elb label value (%s) and alb names (%v) - only one or the other may be"+
			" specified", a.elbLabelValue, a.albNames)
	}

	if err := a.initELBs(schemeToDNS); err != nil {
		return err
	}

	return a.initALBs(schemeToDNS)
}

func (a awsAdapter) initELBs(schemeToDNS map[string]dnsDetails) error {
	if a.elbLabelValue == "" {
		return nil
	}

	elbs, err := a.findFrontEndElbs(a.elb, a.elbLabelValue)
	if err != nil {
		return fmt.Errorf("unable to find front end load balancers: %v", err)
	}

	for scheme, lbDetails := range elbs {
		if strings.HasSuffix(lbDetails.DNSName, ".") {
			return fmt.Errorf("unexpected trailing dot on load balancer DNS name: %s", lbDetails.DNSName)
		}

		schemeToDNS[scheme] = dnsDetails{dnsName: lbDetails.DNSName + ".", hostedZoneID: lbDetails.HostedZoneID}
	}

	return nil
}

func (a awsAdapter) initALBs(schemeToDNS map[string]dnsDetails) error {
	if len(a.albNames) == 0 {
		return nil
	}

	req := &aws_alb.DescribeLoadBalancersInput{Names: aws.StringSlice(a.albNames)}

	for {
		resp, err := a.alb.DescribeLoadBalancers(req)
		if err != nil {
			return err
		}

		for _, lb := range resp.LoadBalancers {
			schemeToDNS[*lb.Scheme] = dnsDetails{dnsName: *lb.DNSName + ".", hostedZoneID: *lb.CanonicalHostedZoneId}
		}

		if resp.NextMarker == nil {
			break
		}

		req.Marker = resp.NextMarker
	}

	return nil
}
