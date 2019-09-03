package cmd

import (
	"time"

	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/elb"
	elbstatus "github.com/sky-uk/feed/elb/status"
	"github.com/sky-uk/feed/k8s"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	region                         string
	loadBalancerType               loadBalancerTypeValue
	elbFrontendTagValue            string
	elbExpectedNumber              int
	drainDelay                     time.Duration
	targetGroupNames               []string
	targetGroupDeregistrationDelay time.Duration
)

const (
	defaultRegion                         = "eu-west-1"
	defaultLoadBalancerType               = elb.Classic
	defaultElbFrontendTagValue            = ""
	defaultElbExpectedNumber              = 0
	defaultDrainDelay                     = time.Second * 60
	defaultTargetGroupDeregistrationDelay = time.Second * 300
)

var elbCmd = &cobra.Command{
	Use:   "elb",
	Short: "Attach to AWS Elastic Load Balancers",
	Run: func(cmd *cobra.Command, args []string) {
		runCmd(appendElbIngressUpdaters)
	},
}

type loadBalancerTypeValue struct {
	pflag.Value
	value elb.LoadBalancerType
}

func (t *loadBalancerTypeValue) Set(s string) error {
	if s == "standard" {
		t.value = elb.Standard
	} else {
		t.value = defaultLoadBalancerType
	}
	return nil
}

func (t *loadBalancerTypeValue) Get() string {
	if t.value == elb.Classic {
		return "classic"
	} else if t.value == elb.Standard {
		return "standard"
	} else {
		return "unsupported"
	}
}

func (t *loadBalancerTypeValue) String() string {
	return t.Get()
}

func init() {
	rootCmd.AddCommand(elbCmd)

	elbCmd.Flags().StringVar(&region, "region", defaultRegion,
		"AWS region for frontend attachment.")
	elbCmd.Flags().Var(&loadBalancerType, "lb-type", "Type of load balancer: classic or standard (default classic)")
	elbCmd.Flags().StringVar(&elbFrontendTagValue, "elb-frontend-tag-value", defaultElbFrontendTagValue,
		"Attach to ELBs tagged with "+elb.FrontendTag+"=value. Leave empty to not attach.")
	elbCmd.Flags().IntVar(&elbExpectedNumber, "elb-expected-number", defaultElbExpectedNumber,
		"Expected number of ELBs to attach to. If 0 the controller will not check,"+
			" otherwise it fails to start if it can't attach to this number.")
	elbCmd.Flags().DurationVar(&drainDelay, "drain-delay", defaultDrainDelay, "Delay to wait"+
		" for feed-ingress to drain from the registration component on shutdown. Should match the ELB's drain time.")
}

func appendElbIngressUpdaters(kubernetesClient k8s.Client, updaters []controller.Updater) ([]controller.Updater, error) {
	elbUpdater, err := elb.New(loadBalancerType.value, region, elbFrontendTagValue, ingressClassName, elbExpectedNumber, drainDelay)
	if err != nil {
		return nil, err
	}
	updaters = append(updaters, elbUpdater)

	statusConfig := elbstatus.Config{
		Region:              region,
		FrontendTagValue:    elbFrontendTagValue,
		IngressNameTagValue: ingressClassName,
		KubernetesClient:    kubernetesClient,
		LoadBalancerType:    loadBalancerType.value,
	}
	elbStatusUpdater, err := elbstatus.New(statusConfig)
	if err != nil {
		return nil, err
	}
	return append(updaters, elbStatusUpdater), nil
}
