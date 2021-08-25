package cmd

import (
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/k8s"
	"github.com/spf13/cobra"
)

var glbCmd = &cobra.Command{
	Use: "gcp-glb",
	Short: "Attach to a GCP Global HTTP(S) Load Balancer",
	Run: func(cmd *cobra.Command, args []string) {
		runCmd(appendGlbIngressUpdaters)
	},
}

func init() {
	rootCmd.AddCommand(glbCmd)

}

func appendGlbIngressUpdaters(kubernetesClient k8s.Client, updaters []controller.Updater) ([]controller.Updater, error) {
	return updaters, nil
}
