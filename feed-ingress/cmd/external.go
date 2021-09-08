package cmd

import (
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/k8s"
	"github.com/spf13/cobra"
)

var externalCmd = &cobra.Command{
	Use:   "external",
	Short: "Don't attach to any external load balancers",
	Run: func(cmd *cobra.Command, args []string) {
		runCmd(empty)
	},
}

func init() {
	rootCmd.AddCommand(externalCmd)

}

func empty(kubernetesClient k8s.Client, updaters []controller.Updater) ([]controller.Updater, error) {
	return updaters, nil
}
