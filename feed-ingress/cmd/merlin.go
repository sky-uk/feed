package cmd

import (
	"time"

	"github.com/sky-uk/feed/merlin"

	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/k8s"

	"github.com/sky-uk/feed/merlin/status"
	"github.com/spf13/cobra"
)

var (
	merlinEndpoint               string
	merlinRequestTimeout         time.Duration
	merlinServiceID              string
	merlinHTTPSServiceID         string
	merlinInstanceIP             string
	merlinForwardMethod          string
	merlinDrainDelay             time.Duration
	merlinHealthUpThreshold      uint
	merlinHealthDownThreshold    uint
	merlinHealthPeriod           time.Duration
	merlinHealthTimeout          time.Duration
	merlinVIP                    string
	merlinVIPInterface           string
	merlinInternalHostname       string
	merlinInternetFacingHostname string
)

const (
	defaultMerlinForwardMethod       = "route"
	defaultMerlinHealthUpThreshold   = 3
	defaultMerlinHealthDownThreshold = 2
	defaultMerlinHealthPeriod        = 10 * time.Second
	defaultMerlinHealthTimeout       = time.Second
	defaultMerlinVIPInterface        = "lo"
)

var merlinCmd = &cobra.Command{
	Use:   "merlin",
	Short: "Attach to Merlin Load Balancers",
	Long: `Merlin is a distributed load balancer based on IPVS, with a gRPC based API.
Feed Ingress supports attaching to merlin as a frontend. https://github.com/sky-uk/merlin`,
	Run: func(cmd *cobra.Command, args []string) {
		runCmd(appendMerlinIngressUpdaters)
	},
}

func init() {
	rootCmd.AddCommand(merlinCmd)

	merlinCmd.Flags().StringVar(&merlinEndpoint, "merlin-endpoint", "",
		"Merlin gRPC endpoint to connect to. Expected format is scheme://authority/endpoint_name (see "+
			"https://github.com/grpc/grpc/blob/master/doc/naming.md). Will load balance between all available servers.")
	merlinCmd.Flags().DurationVar(&merlinRequestTimeout, "merlin-request-timeout", time.Second*10,
		"Timeout for any requests to merlin.")
	merlinCmd.Flags().StringVar(&merlinServiceID, "merlin-service-id", "", "Merlin http virtual service ID to attach to.")
	merlinCmd.Flags().StringVar(&merlinHTTPSServiceID, "merlin-https-service-id", "", "Merlin https virtual service ID to attach to.")
	merlinCmd.Flags().StringVar(&merlinInstanceIP, "merlin-instance-ip", "", "Ingress IP to register with merlin")
	merlinCmd.Flags().StringVar(&merlinForwardMethod, "merlin-forward-method", defaultMerlinForwardMethod, "IPVS forwarding method,"+
		" must be one of route, tunnel, or masq.")
	merlinCmd.Flags().DurationVar(&merlinDrainDelay, "merlin-drain-delay", defaultDrainDelay, "Delay to wait after for connections"+
		" to bleed off when deregistering from merlin. Real server weight is set to 0 during this delay.")
	merlinCmd.Flags().UintVar(&merlinHealthUpThreshold, "merlin-health-up-threshold", defaultMerlinHealthUpThreshold,
		"Number of checks before merlin will consider this instance healthy.")
	merlinCmd.Flags().UintVar(&merlinHealthDownThreshold, "merlin-health-down-threshold", defaultMerlinHealthDownThreshold,
		"Number of checks before merlin will consider this instance unhealthy.")
	merlinCmd.Flags().DurationVar(&merlinHealthPeriod, "merlin-health-period", defaultMerlinHealthPeriod,
		"The time between health checks.")
	merlinCmd.Flags().DurationVar(&merlinHealthTimeout, "merlin-health-timeout", defaultMerlinHealthTimeout,
		"The timeout for health checks.")
	merlinCmd.Flags().StringVar(&merlinVIP, "merlin-vip", "", "VIP to assign to loopback to support direct route and tunnel.")
	merlinCmd.Flags().StringVar(&merlinVIPInterface, "merlin-vip-interface", defaultMerlinVIPInterface,
		"VIP interface to assign the VIP.")
	merlinCmd.Flags().StringVar(&merlinInternalHostname, "merlin-internal-hostname", "",
		"Hostname of the internal facing load-balancer.")
	merlinCmd.Flags().StringVar(&merlinInternetFacingHostname, "merlin-internet-facing-hostname", "",
		"Hostname of the internet facing load-balancer")
}

func appendMerlinIngressUpdaters(kubernetesClient k8s.Client, updaters []controller.Updater) ([]controller.Updater, error) {
	config := merlin.Config{
		Endpoint:          merlinEndpoint,
		Timeout:           merlinRequestTimeout,
		ServiceID:         merlinServiceID,
		HTTPSServiceID:    merlinHTTPSServiceID,
		InstanceIP:        merlinInstanceIP,
		InstancePort:      uint16(ingressPort),
		InstanceHTTPSPort: uint16(ingressHTTPSPort),
		ForwardMethod:     merlinForwardMethod,
		DrainDelay:        merlinDrainDelay,
		HealthPort:        uint16(ingressHealthPort),
		// This value is hardcoded into the nginx template.
		HealthPath:          "health",
		HealthUpThreshold:   uint32(merlinHealthUpThreshold),
		HealthDownThreshold: uint32(merlinHealthDownThreshold),
		HealthPeriod:        merlinHealthPeriod,
		HealthTimeout:       merlinHealthTimeout,
		VIP:                 merlinVIP,
		VIPInterface:        merlinVIPInterface,
	}
	merlinUpdater, err := merlin.New(config)
	if err != nil {
		return nil, err
	}
	updaters = append(updaters, merlinUpdater)

	if merlinInternalHostname != "" || merlinInternetFacingHostname != "" {
		statusConfig := status.Config{
			InternalHostname:       merlinInternalHostname,
			InternetFacingHostname: merlinInternetFacingHostname,
			KubernetesClient:       kubernetesClient,
		}
		merlinStatusUpdater, err := status.New(statusConfig)
		if err != nil {
			return nil, err
		}
		updaters = append(updaters, merlinStatusUpdater)
	}

	return updaters, nil
}
