package cmd

import (
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/elb"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/nlb"
	"github.com/sky-uk/feed/nlb/nlbstatus"
	"github.com/spf13/cobra"
)

var nlbCmd = &cobra.Command{
	Use:   "nlb",
	Short: "Attach to AWS Network Load Balancers",
	Run: func(cmd *cobra.Command, args []string) {
		runCmd(appendNlbIngressUpdaters)
	},
}

func init() {
	rootCmd.AddCommand(nlbCmd)

	nlbCmd.Flags().StringVar(&region, "region", defaultRegion,
		"AWS region for frontend attachment.")
	nlbCmd.Flags().StringVar(&elbFrontendTagValue, "nlb-frontend-tag-value", defaultLbFrontendTagValue,
		"Attach to NLBs tagged with "+elb.FrontendTag+"=value. Leave empty to not attach.")
	nlbCmd.Flags().IntVar(&elbExpectedNumber, "nlb-expected-number", defaultLbExpectedNumber,
		"Expected number of NLBs to attach to. If 0 the controller will not check,"+
			" otherwise it fails to start if it can't attach to this number.")
	nlbCmd.Flags().DurationVar(&drainDelay, "drain-delay", defaultDrainDelay, "Delay to wait"+
		" for feed-ingress to drain from the registration component on shutdown. Should match the NLB's drain time.")
}

func appendNlbIngressUpdaters(kubernetesClient k8s.Client, updaters []controller.Updater) ([]controller.Updater, error) {
	updater, err := nlb.New(region, elbFrontendTagValue, ingressClassName, elbExpectedNumber, drainDelay)
	if err != nil {
		return nil, err
	}
	updaters = append(updaters, updater)

	statusConfig := nlbstatus.Config{
		Region:              region,
		FrontendTagValue:    elbFrontendTagValue,
		IngressNameTagValue: ingressClassName,
		KubernetesClient:    kubernetesClient,
	}
	statusUpdater, err := nlbstatus.New(statusConfig)
	if err != nil {
		return nil, err
	}
	return append(updaters, statusUpdater), nil
}
