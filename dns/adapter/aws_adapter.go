package adapter

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"

	//aws_elb "github.com/aws/aws-sdk-go/service/elb"
	aws_alb "github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/sky-uk/feed/elb"
)

// FindELBsFunc defines a function which find ELBs based on a label
type FindELBsFunc func(*aws_alb.ELBV2, string) (map[string]elb.LoadBalancerDetails, error)

// ALB represents the subset of AWS operations needed for dns_updater.go
type ALB interface {
	DescribeLoadBalancers(input *aws_alb.DescribeLoadBalancersInput) (*aws_alb.DescribeLoadBalancersOutput, error)
}

// AWSAdapterConfig describes the configuration of a FrontendAdapter which uses AWS ELBs and/or ALBs
type AWSAdapterConfig struct {
	Region        string
	HostedZoneID  string
	ELBLabelValue string
	ALBNames      []string
	ALBClient     ALB
	ELBClient     *aws_alb.ELBV2
	ELBFinder     FindELBsFunc
}

type awsAdapter struct {
	hostedZoneID     *string
	elbLabelValue    string
	albNames         []string
	elb              *aws_alb.ELBV2
	alb              ALB
	findFrontEndElbs FindELBsFunc
}

// NewAWSAdapter creates a FrontendAdapter which interacts with AWS ELBs or ALBs.
func NewAWSAdapter(config *AWSAdapterConfig) (FrontendAdapter, error) {
	if config.ALBClient == nil && config.ELBClient == nil {
		session, err := session.NewSession(&aws.Config{Region: &config.Region})
		if err != nil {
			return nil, fmt.Errorf("unable to open AWS session: %v", err)
		}

		config.ALBClient = aws_alb.New(session)
		config.ELBClient = aws_alb.New(session)
	}

	if config.ELBFinder == nil {
		config.ELBFinder = elb.FindFrontEndElbs
	}

	return &awsAdapter{
		hostedZoneID:     aws.String(config.HostedZoneID),
		elbLabelValue:    config.ELBLabelValue,
		albNames:         config.ALBNames,
		elb:              config.ELBClient,
		alb:              config.ALBClient,
		findFrontEndElbs: config.ELBFinder,
	}, nil
}

func (a *awsAdapter) Initialise() (map[string]DNSDetails, error) {
	if a.elbLabelValue != "" && len(a.albNames) > 0 {
		return nil, fmt.Errorf("can't specify both elb label value (%s) and alb names (%v) - only one or the other may be"+
			" specified", a.elbLabelValue, a.albNames)
	}

	schemeToFrontendMap := make(map[string]DNSDetails)
	if err := a.initELBs(schemeToFrontendMap); err != nil {
		return nil, err
	}

	if err := a.initALBs(schemeToFrontendMap); err != nil {
		return nil, err
	}

	return schemeToFrontendMap, nil
}

func (a *awsAdapter) initELBs(schemeToFrontendMap map[string]DNSDetails) error {
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

		schemeToFrontendMap[scheme] = DNSDetails{DNSName: lbDetails.DNSName + ".", HostedZoneID: lbDetails.HostedZoneID}
	}

	return nil
}

func (a *awsAdapter) initALBs(schemeToFrontendMap map[string]DNSDetails) error {
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
			schemeToFrontendMap[*lb.Scheme] = DNSDetails{DNSName: *lb.DNSName + ".", HostedZoneID: *lb.CanonicalHostedZoneId}
		}

		if resp.NextMarker == nil {
			break
		}

		req.Marker = resp.NextMarker
	}

	return nil
}

func (a *awsAdapter) CreateChange(action string, host string, details DNSDetails, recordExists bool, existingRecord *ConsolidatedRecord) *route53.Change {
	if !recordExists {
		set := &route53.ResourceRecordSet{
			Name: aws.String(host),
		}

		set.Type = aws.String("A")
		set.AliasTarget = &route53.AliasTarget{
			DNSName:      aws.String(details.DNSName),
			HostedZoneId: aws.String(details.HostedZoneID),
			// disable this since we only point to a single load balancer
			EvaluateTargetHealth: aws.Bool(false),
		}

		return &route53.Change{
			Action:            aws.String(action),
			ResourceRecordSet: set,
		}
	}

	return nil
}

func (a *awsAdapter) IsManaged(rrs *route53.ResourceRecordSet) (*ConsolidatedRecord, bool) {
	if *rrs.Type == route53.RRTypeA && rrs.AliasTarget != nil {
		return &ConsolidatedRecord{
			Name:            *rrs.Name,
			PointsTo:        *rrs.AliasTarget.DNSName,
			AliasHostedZone: *rrs.AliasTarget.HostedZoneId,
		}, true
	}

	return nil, false
}
