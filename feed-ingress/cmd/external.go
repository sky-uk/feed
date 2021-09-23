package cmd

import (
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/external"
	"github.com/sky-uk/feed/k8s"
	"github.com/spf13/cobra"
)

var (
	externalHostname string
	internalHostName string
)

var externalCmd = &cobra.Command{
	Use:   "external",
	Short: "Attaching to load balancers happens external to feed",
	Run: func(cmd *cobra.Command, args []string) {
		runCmd(empty)
	},
}

func init() {
	rootCmd.AddCommand(externalCmd)

	externalCmd.Flags().StringVar(&externalHostname, "external-hostname", "",
		"Hostname for external ingress")
	externalCmd.Flags().StringVar(&internalHostName, "internal-hostname", "",
		"Hostname for internal ingress")

}

func empty(kubernetesClient k8s.Client, updaters []controller.Updater) ([]controller.Updater, error) {

	config := external.Config{
		InternalHostname: internalHostName,
		ExternalHostname: externalHostname,
		KubernetesClient: kubernetesClient,
	}
	statusUpdater, err := external.New(config)
	if err != nil {
		return nil, err
	}

	return append(updaters, statusUpdater), nil
}
