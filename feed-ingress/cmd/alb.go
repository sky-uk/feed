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
}

func appendAlbIngressUpdaters(kubernetesClient k8s.Client, updaters []controller.Updater) ([]controller.Updater, error) {
	albUpdater, err := alb.New(region, targetGroupNames, targetGroupDeregistrationDelay)
	if err != nil {
		return nil, err
	}
	return append(updaters, albUpdater), nil
}
