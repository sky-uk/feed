/*
Package elb provides an updater for an ELB frontend to attach NGINX to.
*/
package elb

import (
	"fmt"

	"github.com/prometheus/common/log"

	"github.com/aws/aws-sdk-go/aws"
	awselbv2 "github.com/aws/aws-sdk-go/service/elbv2"
)

func findFrontendElbsV2(awsElb V2ELB) (map[string]LoadBalancerDetails, []*string, error) {
	lbRequest := &awselbv2.DescribeLoadBalancersInput{}
	var lbArns []*string
	allLbsByArn := make(map[string]LoadBalancerDetails)

	for {
		lbResp, err := awsElb.DescribeLoadBalancers(lbRequest)

		if err != nil {
			return nil, nil, fmt.Errorf("unable to describe load balancers: %v", err)
		}

		for _, entry := range lbResp.LoadBalancers {
			allLbsByArn[*entry.LoadBalancerArn] = LoadBalancerDetails{
				Name:            aws.StringValue(entry.LoadBalancerArn),
				TargetGroupArns: findTargetGroupArnsV2(awsElb, entry),
				DNSName:         aws.StringValue(entry.DNSName),
				HostedZoneID:    aws.StringValue(entry.CanonicalHostedZoneId),
				Scheme:          aws.StringValue(entry.Scheme),
			}
			lbArns = append(lbArns, entry.LoadBalancerArn)
		}

		if lbResp.NextMarker == nil {
			return allLbsByArn, lbArns, nil
		}

		// Set the next marker
		lbRequest = &awselbv2.DescribeLoadBalancersInput{
			Marker: lbResp.NextMarker,
		}
	}
}

func findTargetGroupArnsV2(awsElb V2ELB, loadBalancer *awselbv2.LoadBalancer) []string {
	request := &awselbv2.DescribeTargetGroupsInput{
		LoadBalancerArn: loadBalancer.LoadBalancerArn,
	}

	response, err := awsElb.DescribeTargetGroups(request)
	if err != nil {
		log.Errorf("Could not query Target Groups for %s: %v", *loadBalancer.LoadBalancerArn, err)
		return nil
	}

	var arns []string
	for _, tg := range response.TargetGroups {
		arns = append(arns, *tg.TargetGroupArn)
	}

	return arns
}

func findTagsV2(awsElb V2ELB, lbArns []*string) (map[string][]tag, error) {
	output, err := awsElb.DescribeTags(&awselbv2.DescribeTagsInput{
		ResourceArns: lbArns,
	})
	if err != nil {
		return nil, err
	}

	tagsByLbArn := make(map[string][]tag)
	for _, elbDescription := range output.TagDescriptions {
		var tags []tag
		for _, elbTag := range elbDescription.Tags {
			tags = append(tags, tag{Key: *elbTag.Key, Value: *elbTag.Value})
		}
		tagsByLbArn[*elbDescription.ResourceArn] = tags
	}

	return tagsByLbArn, nil
}

func registerWithLoadBalancerV2(awsElb V2ELB, instanceID string, lb LoadBalancerDetails) error {
	var failedArns []string

	for _, arn := range lb.TargetGroupArns {
		_, err := awsElb.RegisterTargets(&awselbv2.RegisterTargetsInput{
			Targets:        []*awselbv2.TargetDescription{{Id: aws.String(instanceID)}},
			TargetGroupArn: aws.String(arn),
		})
		if err != nil {
			log.Errorf("Could not register instance %s with Target Group %s: %v", instanceID, arn, err)
			failedArns = append(failedArns, arn)
		}
	}

	if failedArns != nil {
		return fmt.Errorf("could not register Target Group(s) with Instance %s: %v", instanceID, failedArns)
	}

	return nil
}

func deregisterFromLoadBalancerV2(awsElb V2ELB, instanceID string, lb LoadBalancerDetails) error {
	_, err := awsElb.DeregisterTargets(&awselbv2.DeregisterTargetsInput{
		Targets:        []*awselbv2.TargetDescription{{Id: aws.String(instanceID)}},
		TargetGroupArn: aws.String(lb.Name),
	})
	return err
}
