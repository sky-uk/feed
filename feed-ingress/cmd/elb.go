package cmd

import (
	"time"

	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/k8s"

	"github.com/sky-uk/feed/elb"
	elbstatus "github.com/sky-uk/feed/elb/status"
	"github.com/spf13/cobra"
)

var (
	region                         string
	elbFrontendTagValue            string
	elbExpectedNumber              int
	drainDelay                     time.Duration
	targetGroupNames               []string
	targetGroupDeregistrationDelay time.Duration
)

const (
	defaultElbFrontendTagValue            = ""
	defaultDrainDelay                     = time.Second * 60
	defaultTargetGroupDeregistrationDelay = time.Second * 300
	defaultRegion                         = "eu-west-1"
	defaultElbExpectedNumber              = 0
)

var elbCmd = &cobra.Command{
	Use:   "elb",
	Short: "Attach to AWS Elastic Load Balancers",
	Run: func(cmd *cobra.Command, args []string) {
		runCmd(appendElbIngressUpdaters)
	},
}

func init() {
	rootCmd.AddCommand(elbCmd)

	elbCmd.Flags().StringVar(&region, "region", defaultRegion,
		"AWS region for frontend attachment.")
	elbCmd.Flags().StringVar(&elbFrontendTagValue, "elb-frontend-tag-value", defaultElbFrontendTagValue,
		"Attach to ELBs tagged with "+elb.ElbTag+"=value. Leave empty to not attach.")
	elbCmd.Flags().IntVar(&elbExpectedNumber, "elb-expected-number", defaultElbExpectedNumber,
		"Expected number of ELBs to attach to. If 0 the controller will not check,"+
			" otherwise it fails to start if it can't attach to this number.")
	elbCmd.Flags().DurationVar(&drainDelay, "drain-delay", defaultDrainDelay, "Delay to wait"+
		" for feed-ingress to drain from the registration component on shutdown. Should match the ELB's drain time.")
}

func appendElbIngressUpdaters(kubernetesClient k8s.Client, updaters []controller.Updater) ([]controller.Updater, error) {
	elbUpdater, err := elb.New(region, elbFrontendTagValue, ingressClassName, elbExpectedNumber, drainDelay)
	if err != nil {
		return nil, err
	}
	updaters = append(updaters, elbUpdater)

	statusConfig := elbstatus.Config{
		Region:              region,
		FrontendTagValue:    elbFrontendTagValue,
		IngressNameTagValue: ingressClassName,
		KubernetesClient:    kubernetesClient,
	}
	elbStatusUpdater, err := elbstatus.New(statusConfig)
	if err != nil {
		return nil, err
	}
	return append(updaters, elbStatusUpdater), nil
}
