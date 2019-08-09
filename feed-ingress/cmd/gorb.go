package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sky-uk/feed/k8s"

	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/gorb"
	"github.com/spf13/cobra"
)

var (
	gorbIngressInstanceIP          string
	gorbEndpoint                   string
	gorbServicesDefinition         string
	gorbBackendMethod              string
	gorbBackendWeight              int
	gorbVipLoadbalancer            string
	gorbManageLoopback             bool
	gorbBackendHealthcheckInterval string
	gorbBackendHealthcheckType     string
	gorbInterfaceProcFsPath        string
)

const (
	defaultGorbIngressInstanceIP          = "127.0.0.1"
	defaultGorbEndpoint                   = "http://127.0.0.1:80"
	defaultGorbBackendMethod              = "dr"
	defaultGorbBackendWeight              = 1000
	defaultGorbServicesDefinition         = "http-proxy:80,https-proxy:443"
	defaultGorbVipLoadbalancer            = "127.0.0.1"
	defaultGorbManageLoopback             = true
	defaultGorbInterfaceProcFsPath        = "/host-ipv4-proc/"
	defaultGorbBackendHealthcheckInterval = "1s"
	defaultGorbBackendHealthcheckType     = "http"
)

var gorbCmd = &cobra.Command{
	Use:   "gorb",
	Short: "Attach to Gorb Load Balancers (deprecated; use Merlin instead)",
	Long:  `Configure IPVS via Gorb. https://github.com/sky-uk/gorb`,
	Run: func(cmd *cobra.Command, args []string) {
		runCmd(appendGorbIngressUpdaters)
	},
}

func init() {
	rootCmd.AddCommand(gorbCmd)

	gorbCmd.Flags().StringVar(&gorbEndpoint, "gorb-endpoint", defaultGorbEndpoint, "Define the endpoint to talk to gorb for registration.")
	gorbCmd.Flags().StringVar(&gorbIngressInstanceIP, "gorb-ingress-instance-ip", defaultGorbIngressInstanceIP,
		"Define the ingress instance ip, the ip of the node where feed-ingress is running.")
	gorbCmd.Flags().StringVar(&gorbServicesDefinition, "gorb-services-definition", defaultGorbServicesDefinition,
		"Comma separated list of Service Definition (e.g. 'http-proxy:80,https-proxy:443') to register via Gorb")
	gorbCmd.Flags().StringVar(&gorbBackendMethod, "gorb-backend-method", defaultGorbBackendMethod,
		"Define the backend method (e.g. nat, dr, tunnel) to register via Gorb ")
	gorbCmd.Flags().IntVar(&gorbBackendWeight, "gorb-backend-weight", defaultGorbBackendWeight,
		"Define the backend weight to register via Gorb")
	gorbCmd.Flags().StringVar(&gorbVipLoadbalancer, "gorb-vip-loadbalancer", defaultGorbVipLoadbalancer,
		"Define the vip loadbalancer to set the loopback. Only necessary when Direct Return is enabled.")
	gorbCmd.Flags().BoolVar(&gorbManageLoopback, "gorb-management-loopback", defaultGorbManageLoopback,
		"Enable loopback creation. Only necessary when Direct Return is enabled")
	gorbCmd.Flags().StringVar(&gorbInterfaceProcFsPath, "gorb-interface-proc-fs-path", defaultGorbInterfaceProcFsPath,
		"Path to the interface proc file system. Only necessary when Direct Return is enabled")
	gorbCmd.Flags().StringVar(&gorbBackendHealthcheckInterval, "gorb-backend-healthcheck-interval", defaultGorbBackendHealthcheckInterval,
		"Define the gorb healthcheck interval for the backend")
	gorbCmd.Flags().StringVar(&gorbBackendHealthcheckType, "gorb-backend-healthcheck-type", defaultGorbBackendHealthcheckType,
		"Define the gorb healthcheck type for the backend. Must be either 'tcp', 'http' or 'none'")
}

func appendGorbIngressUpdaters(kubernetesClient k8s.Client, updaters []controller.Updater) ([]controller.Updater, error) {
	virtualServices, err := toVirtualServices(gorbServicesDefinition)
	if err != nil {
		return nil, fmt.Errorf("invalid gorb services definition. Must be a comma separated list - e.g. 'http-proxy:80,https-proxy:443', but was %s", gorbServicesDefinition)
	}

	if gorbBackendHealthcheckType != "tcp" && gorbBackendHealthcheckType != "http" && gorbBackendHealthcheckType != "none" {
		return nil, fmt.Errorf("invalid gorb backend healthcheck type. Must be either 'tcp', 'http' or 'none', but was %s", gorbBackendHealthcheckType)
	}

	config := gorb.Config{
		ServerBaseURL:              gorbEndpoint,
		InstanceIP:                 gorbIngressInstanceIP,
		DrainDelay:                 drainDelay,
		ServicesDefinition:         virtualServices,
		BackendMethod:              gorbBackendMethod,
		BackendWeight:              gorbBackendWeight,
		VipLoadbalancer:            gorbVipLoadbalancer,
		ManageLoopback:             gorbManageLoopback,
		BackendHealthcheckInterval: gorbBackendHealthcheckInterval,
		BackendHealthcheckType:     gorbBackendHealthcheckType,
		InterfaceProcFsPath:        gorbInterfaceProcFsPath,
	}

	gorbUpdater, err := gorb.New(&config)
	if err != nil {
		return nil, err
	}
	return append(updaters, gorbUpdater), nil
}

func toVirtualServices(servicesCsv string) ([]gorb.VirtualService, error) {
	virtualServices := make([]gorb.VirtualService, 0)
	servicesDefinitionArr := strings.Split(servicesCsv, ",")
	for _, service := range servicesDefinitionArr {
		servicesArr := strings.Split(service, ":")
		if len(servicesArr) != 2 {
			return nil, fmt.Errorf("unable to convert %s to servicename:port combination", servicesArr)
		}
		port, err := strconv.Atoi(servicesArr[1])
		if err != nil {
			return nil, fmt.Errorf("unable to convert port %s to int", servicesArr[1])
		}
		virtualServices = append(virtualServices, gorb.VirtualService{Name: servicesArr[0], Port: port})
	}
	return virtualServices, nil
}
