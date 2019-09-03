/*
Package elb provides an updater for an ELB frontend to attach NGINX to.
*/
package elb

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	awselbv1 "github.com/aws/aws-sdk-go/service/elb"
)

func findFrontendElbsV1(awsElb V1ELB) (map[string]LoadBalancerDetails, []*string, error) {
	request := &awselbv1.DescribeLoadBalancersInput{}
	var lbNames []*string
	allLbsByName := make(map[string]LoadBalancerDetails)

	for {
		resp, err := awsElb.DescribeLoadBalancers(request)

		if err != nil {
			return nil, nil, fmt.Errorf("unable to describe load balancers: %v", err)
		}

		for _, entry := range resp.LoadBalancerDescriptions {
			allLbsByName[*entry.LoadBalancerName] = LoadBalancerDetails{
				Name:         aws.StringValue(entry.LoadBalancerName),
				DNSName:      aws.StringValue(entry.DNSName),
				HostedZoneID: aws.StringValue(entry.CanonicalHostedZoneNameID),
				Scheme:       aws.StringValue(entry.Scheme),
			}
			lbNames = append(lbNames, entry.LoadBalancerName)
		}

		if resp.NextMarker == nil {
			return allLbsByName, lbNames, nil
		}

		// Set the next marker
		request = &awselbv1.DescribeLoadBalancersInput{
			Marker: resp.NextMarker,
		}
	}
}

func findTagsV1(awsElb V1ELB, lbNames []*string) (map[string][]tag, error) {
	output, err := awsElb.DescribeTags(&awselbv1.DescribeTagsInput{
		LoadBalancerNames: lbNames,
	})
	if err != nil {
		return nil, err
	}

	tagsByLbName := make(map[string][]tag)
	for _, elbDescription := range output.TagDescriptions {
		var tags []tag
		for _, elbTag := range elbDescription.Tags {
			tags = append(tags, tag{Key: *elbTag.Key, Value: *elbTag.Value})
		}
		tagsByLbName[*elbDescription.LoadBalancerName] = tags
	}

	return tagsByLbName, nil
}

func registerWithLoadBalancerV1(awsElb V1ELB, instanceID string, lb LoadBalancerDetails) error {
	_, err := awsElb.RegisterInstancesWithLoadBalancer(&awselbv1.RegisterInstancesWithLoadBalancerInput{
		Instances:        []*awselbv1.Instance{{InstanceId: aws.String(instanceID)}},
		LoadBalancerName: aws.String(lb.Name),
	})
	return err
}

func deregisterFromLoadBalancerV1(awsElb V1ELB, instanceID string, lb LoadBalancerDetails) error {
	_, err := awsElb.DeregisterInstancesFromLoadBalancer(&awselbv1.DeregisterInstancesFromLoadBalancerInput{
		Instances:        []*awselbv1.Instance{{InstanceId: aws.String(instanceID)}},
		LoadBalancerName: aws.String(lb.Name),
	})
	return err
}
