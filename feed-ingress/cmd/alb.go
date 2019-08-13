package cmd

import (
	"github.com/sky-uk/feed/alb"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/k8s"

	"github.com/spf13/cobra"
)

var albCmd = &cobra.Command{
	Use:   "alb",
	Short: "Attach to AWS Application Load Balancers",
	Long: `Unfortunately, ALBs have a bug that prevents non-disruptive deployments of feed.
Specifically, they don't respect the deregistration delay. As a result, we don't recommend using ALBs at this time.`,
	Run: func(cmd *cobra.Command, args []string) {
		runCmd(appendAlbIngressUpdaters)
	},
}

func init() {
	rootCmd.AddCommand(albCmd)

	albCmd.Flags().StringVar(&region, "region", defaultRegion,
		"AWS region for frontend attachment.")
	albCmd.Flags().StringSliceVar(&targetGroupNames, "alb-target-group-names", []string{},
		"Names of ALB target groups to attach to, separated by commas.")
	albCmd.Flags().DurationVar(&targetGroupDeregistrationDelay, "alb-target-group-deregistration-delay",
		defaultTargetGroupDeregistrationDelay,
		"Delay to wait for feed-ingress to deregister from the ALB target group on shutdown. Should match"+
			" the target group setting in AWS.")
}

func appendAlbIngressUpdaters(kubernetesClient k8s.Client, updaters []controller.Updater) ([]controller.Updater, error) {
	albUpdater, err := alb.New(region, targetGroupNames, targetGroupDeregistrationDelay)
	if err != nil {
		return nil, err
	}
	return append(updaters, albUpdater), nil
}
