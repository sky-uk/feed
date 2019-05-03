package main

import (
	"errors"
	"fmt"
	_ "net/http/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/sky-uk/feed/gclb"

	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/alb"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/elb"
	elb_status "github.com/sky-uk/feed/elb/status"
	"github.com/sky-uk/feed/gorb"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/merlin"
	merlin_status "github.com/sky-uk/feed/merlin/status"
	"github.com/sky-uk/feed/nginx"
	"github.com/sky-uk/feed/util/cmd"
	"github.com/sky-uk/feed/util/metrics"
	flag "github.com/spf13/pflag"
)

var (
	debug                                   bool
	kubeconfig                              string
	resyncPeriod                            time.Duration
	ingressPort                             int
	ingressHTTPSPort                        int
	ingressHealthPort                       int
	healthPort                              int
	region                                  string
	elbLabelValue                           string
	elbExpectedNumber                       int
	drainDelay                              time.Duration
	targetGroupNames                        []string
	targetGroupDeregistrationDelay          time.Duration
	pushgatewayURL                          string
	pushgatewayIntervalSeconds              int
	pushgatewayLabels                       []string
	controllerConfig                        controller.Config
	nginxConfig                             nginx.Conf
	nginxLogHeaders                         []string
	nginxTrustedFrontends                   []string
	nginxSSLPath                            string
	nginxVhostStatsSharedMemory             int
	nginxOpenTracingPluginPath              string
	nginxOpenTracingConfigPath              string
	legacyBackendKeepaliveSeconds           int
	registrationFrontendType                string
	gorbIngressInstanceIP                   string
	gorbEndpoint                            string
	gorbServicesDefinition                  string
	gorbBackendMethod                       string
	gorbBackendWeight                       int
	gorbVipLoadbalancer                     string
	gorbManageLoopback                      bool
	gorbBackendHealthcheckInterval          string
	gorbBackendHealthcheckType              string
	gorbInterfaceProcFsPath                 string
	merlinEndpoint                          string
	merlinRequestTimeout                    time.Duration
	merlinServiceID                         string
	merlinHTTPSServiceID                    string
	merlinInstanceIP                        string
	merlinForwardMethod                     string
	merlinDrainDelay                        time.Duration
	merlinHealthUpThreshold                 uint
	merlinHealthDownThreshold               uint
	merlinHealthPeriod                      time.Duration
	merlinHealthTimeout                     time.Duration
	merlinVIP                               string
	merlinVIPInterface                      string
	merlinInternalHostname                  string
	merlinInternetFacingHostname            string
	gclbTargetPoolPrefix                    string
	gclbTargetPoolConnectionDrainingTimeout time.Duration
	gclbInstanceGroupPrefix                 string
	gclbExpectedNumber                      int
	gclbShutdownGracePeriod                 bool
)

const (
	unset                                          = -1
	defaultResyncPeriod                            = time.Minute * 15
	defaultIngressPort                             = 8080
	defaultIngressHTTPSPort                        = unset
	defaultIngressAllow                            = "0.0.0.0/0"
	defaultIngressHealthPort                       = 8081
	defaultIngressStripPath                        = true
	defaultIngressExactPath                        = false
	defaultHealthPort                              = 12082
	defaultNginxBinary                             = "/usr/sbin/nginx"
	defaultNginxWorkingDir                         = "/nginx"
	defaultNginxWorkers                            = 1
	defaultNginxWorkerConnections                  = 1024
	defaultNginxWorkerShutdownTimeoutSeconds       = 0
	defaultNginxKeepAliveSeconds                   = 60
	defaultNginxBackendKeepalives                  = 512
	defaultNginxBackendTimeoutSeconds              = 60
	defaultNginxBackendConnectTimeoutSeconds       = 1
	defaultNginxBackendMaxConnections              = 0
	defaultNginxProxyBufferSize                    = 16
	defaultNginxProxyBufferBlocks                  = 4
	defaultNginxLogLevel                           = "warn"
	defaultNginxServerNamesHashBucketSize          = unset
	defaultNginxServerNamesHashMaxSize             = unset
	defaultNginxProxyProtocol                      = false
	defaultNginxUpdatePeriod                       = time.Second * 30
	defaultNginxSSLPath                            = "/etc/ssl/default-ssl/default-ssl"
	defaultNginxVhostStatsSharedMemory             = 1
	defaultNginxOpenTracingPluginPath              = ""
	defaultNginxOpenTracingConfigPath              = ""
	defaultElbLabelValue                           = ""
	defaultDrainDelay                              = time.Second * 60
	defaultTargetGroupDeregistrationDelay          = time.Second * 300
	defaultRegion                                  = "eu-west-1"
	defaultElbExpectedNumber                       = 0
	defaultPushgatewayIntervalSeconds              = 60
	defaultAccessLogDir                            = "/var/log/nginx"
	defaultRegistrationFrontendType                = "elb"
	defaultGorbIngressInstanceIP                   = "127.0.0.1"
	defaultGorbEndpoint                            = "http://127.0.0.1:80"
	defaultGorbBackendMethod                       = "dr"
	defaultGorbBackendWeight                       = 1000
	defaultGorbServicesDefinition                  = "http-proxy:80,https-proxy:443"
	defaultGorbVipLoadbalancer                     = "127.0.0.1"
	defaultGorbManageLoopback                      = true
	defaultGorbInterfaceProcFsPath                 = "/host-ipv4-proc/"
	defaultGorbBackendHealthcheckInterval          = "1s"
	defaultGorbBackendHealthcheckType              = "http"
	defaultMerlinForwardMethod                     = "route"
	defaultMerlinHealthUpThreshold                 = 3
	defaultMerlinHealthDownThreshold               = 2
	defaultMerlinHealthPeriod                      = 10 * time.Second
	defaultMerlinHealthTimeout                     = time.Second
	defaultMerlinVIPInterface                      = "lo"
	defaultClientHeaderBufferSize                  = 16
	defaultClientBodyBufferSize                    = 16
	defaultLargeClientHeaderBufferBlocks           = 4
	defaultGclbTargetPoolPrefix                    = ""
	defaultGclbTargetPoolConnectionDrainingTimeout = 28 * time.Second
	defaultGclbInstanceGroupPrefix                 = ""
	defaultGclbExpectedNumber                      = 0
)

func init() {
	// general flags
	flag.BoolVar(&debug, "debug", false,
		"Enable debug logging.")
	flag.StringVar(&kubeconfig, "kubeconfig", "",
		"Path to kubeconfig for connecting to the apiserver. Leave blank to connect inside a cluster.")
	flag.DurationVar(&resyncPeriod, "resync-period", defaultResyncPeriod,
		"Resync with the apiserver periodically to handle missed updates.")
	flag.IntVar(&ingressPort, "ingress-port", defaultIngressPort,
		"Port to serve ingress traffic to backend services.")
	flag.IntVar(&ingressHTTPSPort, "ingress-https-port", defaultIngressHTTPSPort,
		"Port to serve ingress https traffic to backend services.")
	flag.IntVar(&ingressHealthPort, "ingress-health-port", defaultIngressHealthPort,
		"Port for ingress /health and /status pages. Should be used by frontends to determine if ingress is available.")
	flag.StringVar(&controllerConfig.DefaultAllow, "ingress-allow", defaultIngressAllow,
		"Source IP or CIDR to allow ingress access by default. This is overridden by the sky.uk/allow "+
			"annotation on ingress resources. Leave empty to deny all access by default.")
	flag.BoolVar(&controllerConfig.DefaultStripPath, "ingress-strip-path", defaultIngressStripPath,
		"Whether to strip the ingress path from the URL before passing to backend services. For example, "+
			"if enabled 'myhost/myapp/health' would be passed as '/health' to the backend service. If disabled, "+
			"it would be passed as '/myapp/health'. Enabling this requires nginx to process the URL, which has some "+
			"limitations. URL encoded characters will not work correctly in some cases, and backend services will "+
			"need to take care to properly construct URLs, such as by using the 'X-Original-URI' header."+
			"Can be overridden with the sky.uk/strip-path annotation per ingress")
	flag.BoolVar(&controllerConfig.DefaultExactPath, "ingress-exact-path", defaultIngressExactPath,
		"Whether to consider the ingress path to be an exact match rather than as a prefix. For example, "+
			"if enabled 'myhost/myapp/health' would match 'myhost/myapp/health' but not 'myhost/myapp/health/x'."+
			" If disabled, it would match both (and redirect requests from 'myhost/myapp/health' to "+
			" '/myhost/myapp/health/'. Can be overridden with the sky.uk/exact-path annotation per ingress")
	flag.IntVar(&healthPort, "health-port", defaultHealthPort,
		"Port for checking the health of the ingress controller on /health. Also provides /debug/pprof.")

	// nginx flags
	flag.StringVar(&nginxConfig.BinaryLocation, "nginx-binary", defaultNginxBinary,
		"Location of nginx binary.")
	flag.StringVar(&nginxConfig.WorkingDir, "nginx-workdir", defaultNginxWorkingDir,
		"Directory to store nginx files. Also the location of the nginx.tmpl file.")
	flag.IntVar(&nginxConfig.WorkerProcesses, "nginx-workers", defaultNginxWorkers,
		"Number of nginx worker processes.")
	flag.IntVar(&nginxConfig.WorkerConnections, "nginx-worker-connections", defaultNginxWorkerConnections,
		"Max number of connections per nginx worker. Includes both client and proxy connections.")
	flag.IntVar(&nginxConfig.WorkerShutdownTimeoutSeconds, "nginx-worker-shutdown-timeout-seconds", defaultNginxWorkerShutdownTimeoutSeconds,
		"Timeout for a graceful shutdown of worker processes.")
	flag.IntVar(&nginxConfig.KeepaliveSeconds, "nginx-keepalive-seconds", defaultNginxKeepAliveSeconds,
		"Keep alive time for persistent client connections to nginx. Should generally be set larger than frontend "+
			"keep alive times to prevent stale connections.")
	flag.IntVar(&nginxConfig.BackendKeepalives, "nginx-backend-keepalive-count", defaultNginxBackendKeepalives,
		"Maximum number of keepalive connections per backend service. Keepalive connections count against"+
			" nginx-worker-connections limit, and will be restricted by that global limit as well.")
	flag.IntVar(&legacyBackendKeepaliveSeconds, "nginx-default-backend-keepalive-seconds", unset,
		"Deprecated. Use -nginx-default-backend-timeout-seconds instead.")
	flag.IntVar(&controllerConfig.DefaultBackendTimeoutSeconds, "nginx-default-backend-timeout-seconds",
		defaultNginxBackendTimeoutSeconds,
		"Timeout for requests to backends. Can be overridden per ingress with the sky.uk/backend-timeout-seconds annotation.")
	flag.IntVar(&nginxConfig.BackendConnectTimeoutSeconds, "nginx-backend-connect-timeout-seconds",
		defaultNginxBackendConnectTimeoutSeconds,
		"Connect timeout to backend services.")
	flag.IntVar(&controllerConfig.DefaultBackendMaxConnections, "nginx-default-backend-max-connections",
		defaultNginxBackendMaxConnections,
		"Maximum number of connections to a single backend. Can be overridden per ingress with the sky.uk/backend-max-connections annotation.")
	flag.IntVar(&controllerConfig.DefaultProxyBufferSize, "nginx-default-proxy-buffer-size",
		defaultNginxProxyBufferSize,
		"Proxy buffer size for response. Can be overridden per ingress with the sky.uk/proxy-buffer-size-in-kb annotation.")
	flag.IntVar(&controllerConfig.DefaultProxyBufferBlocks, "nginx-default-proxy-buffer-blocks",
		defaultNginxProxyBufferBlocks,
		"Proxy buffer blocks for response. Can be overridden per ingress with the sky.uk/proxy-buffer-blocks annotation.")
	flag.StringVar(&nginxConfig.LogLevel, "nginx-loglevel", defaultNginxLogLevel,
		"Log level for nginx. See http://nginx.org/en/docs/ngx_core_module.html#error_log for levels.")
	flag.IntVar(&nginxConfig.ServerNamesHashBucketSize, "nginx-server-names-hash-bucket-size", defaultNginxServerNamesHashBucketSize,
		"Sets the bucket size for the server names hash tables. Setting this to 0 or less will exclude this "+
			"config from the nginx conf file. The details of setting up hash tables are provided "+
			"in a separate document. http://nginx.org/en/docs/hash.html")
	flag.IntVar(&nginxConfig.ServerNamesHashMaxSize, "nginx-server-names-hash-max-size", defaultNginxServerNamesHashMaxSize,
		"Sets the maximum size of the server names hash tables. Setting this to 0 or less will exclude this "+
			"config from the nginx conf file. The details of setting up hash tables are provided "+
			"in a separate document. http://nginx.org/en/docs/hash.html")
	flag.BoolVar(&nginxConfig.ProxyProtocol, "nginx-proxy-protocol", defaultNginxProxyProtocol,
		"Enable PROXY protocol for nginx listeners.")
	flag.DurationVar(&nginxConfig.UpdatePeriod, "nginx-update-period", defaultNginxUpdatePeriod,
		"How often nginx reloads can occur. Too frequent will result in many nginx worker processes alive at the same time.")
	flag.StringVar(&nginxConfig.AccessLogDir, "access-log-dir", defaultAccessLogDir, "Access logs direcoty.")
	flag.BoolVar(&nginxConfig.AccessLog, "access-log", false, "Enable access logs directive.")
	flag.StringSliceVar(&nginxLogHeaders, "nginx-log-headers", []string{},
		"Comma separated list of headers to be logged in access logs")
	flag.StringSliceVar(&nginxTrustedFrontends, "nginx-trusted-frontends", []string{},
		"Comma separated list of CIDRs to trust when determining the client's real IP from "+
			"frontends. The client IP is used for allowing or denying ingress access. "+
			"This will typically be the ELB subnet.")
	flag.StringVar(&nginxSSLPath, "ssl-path", defaultNginxSSLPath,
		"Set default ssl path + name file without extension.  Feed expects two files: one ending in .crt (the CA) and the other in .key (the private key).")
	flag.IntVar(&nginxVhostStatsSharedMemory, "nginx-vhost-stats-shared-memory", defaultNginxVhostStatsSharedMemory,
		"Memory (in MiB) which should be allocated for use by the vhost statistics module")
	flag.StringVar(&nginxOpenTracingPluginPath, "nginx-opentracing-plugin-path", defaultNginxOpenTracingPluginPath,
		"Path to OpenTracing plugin on disk (eg. /usr/local/lib/libjaegertracing_plugin.so)")
	flag.StringVar(&nginxOpenTracingConfigPath, "nginx-opentracing-config-path", defaultNginxOpenTracingConfigPath,
		"Path to OpenTracing config on disk (eg. /etc/jaeger-nginx-config.json)")
	flag.IntVar(&nginxConfig.ClientHeaderBufferSize, "nginx-client-header-buffer-size-in-kb", defaultClientHeaderBufferSize, "Sets buffer size for reading client request header")
	flag.IntVar(&nginxConfig.ClientBodyBufferSize, "nginx-client-body-buffer-size-in-kb", defaultClientBodyBufferSize, "Sets buffer size for reading client request body")
	flag.IntVar(&nginxConfig.LargeClientHeaderBufferBlocks, "nginx-large-client-header-buffer-blocks", defaultLargeClientHeaderBufferBlocks, "Sets the maximum number of buffers used for reading large client request header")

	// elb/alb/gclb flags
	flag.StringVar(&region, "region", defaultRegion,
		"AWS region for frontend attachment.")
	flag.StringVar(&elbLabelValue, "elb-label-value", defaultElbLabelValue,
		"Attach to ELBs tagged with "+elb.ElbTag+"=value. Leave empty to not attach.")
	flag.StringVar(&registrationFrontendType, "registration-frontend-type", defaultRegistrationFrontendType,
		"Define the registration frontend type. Must be merlin, gorb, elb or alb.")
	flag.IntVar(&elbExpectedNumber, "elb-expected-number", defaultElbExpectedNumber,
		"Expected number of ELBs to attach to. If 0 the controller will not check,"+
			" otherwise it fails to start if it can't attach to this number.")
	flag.DurationVar(&drainDelay, "drain-delay", defaultDrainDelay, "Delay to wait"+
		" for feed-ingress to drain from the registration component on shutdown. Should match the ELB's drain time.")
	flag.StringSliceVar(&targetGroupNames, "alb-target-group-names", []string{},
		"Names of ALB target groups to attach to, separated by commas.")
	flag.DurationVar(&targetGroupDeregistrationDelay, "alb-target-group-deregistration-delay",
		defaultTargetGroupDeregistrationDelay,
		"Delay to wait for feed-ingress to deregister from the ALB target group on shutdown. Should match"+
			" the target group setting in AWS.")

	// prometheus flags
	flag.StringVar(&pushgatewayURL, "pushgateway", "",
		"Prometheus pushgateway URL for pushing metrics. Leave blank to not push metrics.")
	flag.IntVar(&pushgatewayIntervalSeconds, "pushgateway-interval", defaultPushgatewayIntervalSeconds,
		"Interval in seconds for pushing metrics.")
	flag.StringSliceVar(&pushgatewayLabels, "pushgateway-label", []string{},
		"A label=value pair to attach to metrics pushed to prometheus. Specify multiple times for multiple labels.")

	// gorb flags
	flag.StringVar(&gorbEndpoint, "gorb-endpoint", defaultGorbEndpoint, "Define the endpoint to talk to gorb for registration.")
	flag.StringVar(&gorbIngressInstanceIP, "gorb-ingress-instance-ip", defaultGorbIngressInstanceIP,
		"Define the ingress instance ip, the ip of the node where feed-ingress is running.")
	flag.StringVar(&gorbServicesDefinition, "gorb-services-definition", defaultGorbServicesDefinition,
		"Comma separated list of Service Definition (e.g. 'http-proxy:80,https-proxy:443') to register via Gorb")
	flag.StringVar(&gorbBackendMethod, "gorb-backend-method", defaultGorbBackendMethod,
		"Define the backend method (e.g. nat, dr, tunnel) to register via Gorb ")
	flag.IntVar(&gorbBackendWeight, "gorb-backend-weight", defaultGorbBackendWeight,
		"Define the backend weight to register via Gorb")
	flag.StringVar(&gorbVipLoadbalancer, "gorb-vip-loadbalancer", defaultGorbVipLoadbalancer,
		"Define the vip loadbalancer to set the loopback. Only necessary when Direct Return is enabled.")
	flag.BoolVar(&gorbManageLoopback, "gorb-management-loopback", defaultGorbManageLoopback,
		"Enable loopback creation. Only necessary when Direct Return is enabled")
	flag.StringVar(&gorbInterfaceProcFsPath, "gorb-interface-proc-fs-path", defaultGorbInterfaceProcFsPath,
		"Path to the interface proc file system. Only necessary when Direct Return is enabled")
	flag.StringVar(&gorbBackendHealthcheckInterval, "gorb-backend-healthcheck-interval", defaultGorbBackendHealthcheckInterval,
		"Define the gorb healthcheck interval for the backend")
	flag.StringVar(&gorbBackendHealthcheckType, "gorb-backend-healthcheck-type", defaultGorbBackendHealthcheckType,
		"Define the gorb healthcheck type for the backend. Must be either 'tcp', 'http' or 'none'")

	// merlin flags
	flag.StringVar(&merlinEndpoint, "merlin-endpoint", "",
		"Merlin gRPC endpoint to connect to. Expected format is scheme://authority/endpoint_name (see "+
			"https://github.com/grpc/grpc/blob/master/doc/naming.md). Will load balance between all available servers.")
	flag.DurationVar(&merlinRequestTimeout, "merlin-request-timeout", time.Second*10,
		"Timeout for any requests to merlin.")
	flag.StringVar(&merlinServiceID, "merlin-service-id", "", "Merlin http virtual service ID to attach to.")
	flag.StringVar(&merlinHTTPSServiceID, "merlin-https-service-id", "", "Merlin https virtual service ID to attach to.")
	flag.StringVar(&merlinInstanceIP, "merlin-instance-ip", "", "Ingress IP to register with merlin")
	flag.StringVar(&merlinForwardMethod, "merlin-forward-method", defaultMerlinForwardMethod, "IPVS forwarding method,"+
		" must be one of route, tunnel, or masq.")
	flag.DurationVar(&merlinDrainDelay, "merlin-drain-delay", defaultDrainDelay, "Delay to wait after for connections"+
		" to bleed off when deregistering from merlin. Real server weight is set to 0 during this delay.")
	flag.UintVar(&merlinHealthUpThreshold, "merlin-health-up-threshold", defaultMerlinHealthUpThreshold,
		"Number of checks before merlin will consider this instance healthy.")
	flag.UintVar(&merlinHealthDownThreshold, "merlin-health-down-threshold", defaultMerlinHealthDownThreshold,
		"Number of checks before merlin will consider this instance unhealthy.")
	flag.DurationVar(&merlinHealthPeriod, "merlin-health-period", defaultMerlinHealthPeriod,
		"The time between health checks.")
	flag.DurationVar(&merlinHealthTimeout, "merlin-health-timeout", defaultMerlinHealthTimeout,
		"The timeout for health checks.")
	flag.StringVar(&merlinVIP, "merlin-vip", "", "VIP to assign to loopback to support direct route and tunnel.")
	flag.StringVar(&merlinVIPInterface, "merlin-vip-interface", defaultMerlinVIPInterface,
		"VIP interface to assign the VIP.")
	flag.StringVar(&merlinInternalHostname, "merlin-internal-hostname", "",
		"Hostname of the internal facing load-balancer.")
	flag.StringVar(&merlinInternetFacingHostname, "merlin-internet-facing-hostname", "",
		"Hostname of the internet facing load-balancer")

	// gclb flags
	flag.IntVar(&gclbExpectedNumber, "gclb-expected-number", defaultGclbExpectedNumber,
		"Expected number of GCLBs to attach to. If 0 the controller will not check,"+
			" otherwise it fails to start if it can't attach to this number.")
	flag.StringVar(&gclbTargetPoolPrefix, "gclb-target-pool-prefix", defaultGclbTargetPoolPrefix,
		"GLCB backend target pool prefix.")
	flag.DurationVar(&gclbTargetPoolConnectionDrainingTimeout, "gclb-target-pool-connection-draining-timeout", defaultGclbTargetPoolConnectionDrainingTimeout,
		"GCLB backend target pool draining timeout.")
	flag.StringVar(&gclbInstanceGroupPrefix, "gclb-instance-group-prefix", defaultGclbInstanceGroupPrefix,
		"GCLB backend Instance Group prefix.")
}

func main() {
	flag.Parse()

	cmd.ConfigureLogging(debug)
	cmd.ConfigureMetrics("feed-ingress", pushgatewayLabels, pushgatewayURL, pushgatewayIntervalSeconds)

	client, err := k8s.New(kubeconfig, resyncPeriod)
	if err != nil {
		log.Fatal("Unable to create k8s client: ", err)
	}
	controllerConfig.KubernetesClient = client

	controllerConfig.Updaters, err = createIngressUpdaters(client)
	if err != nil {
		log.Fatal("Unable to create ingress updaters: ", err)
	}

	// If the legacy setting is set, use it instead to preserve backwards compatibility.
	if legacyBackendKeepaliveSeconds != unset {
		controllerConfig.DefaultBackendTimeoutSeconds = legacyBackendKeepaliveSeconds
	}
	feedController := controller.New(controllerConfig)

	cmd.AddHealthMetrics(feedController, metrics.PrometheusIngressSubsystem)
	cmd.AddHealthPort(feedController, healthPort)
	cmd.AddSignalHandler(feedController)

	if err = feedController.Start(); err != nil {
		log.Fatal("Error while starting controller: ", err)
	}
	log.Info("Controller started")

	select {}
}

func createIngressUpdaters(kubernetesClient k8s.Client) ([]controller.Updater, error) {
	nginxConfig.Ports = []nginx.Port{{Name: "http", Port: ingressPort}}
	if ingressHTTPSPort != unset {
		nginxConfig.Ports = append(nginxConfig.Ports, nginx.Port{Name: "https", Port: ingressHTTPSPort})
	}

	nginxConfig.HealthPort = ingressHealthPort
	nginxConfig.SSLPath = nginxSSLPath
	nginxConfig.TrustedFrontends = nginxTrustedFrontends
	nginxConfig.LogHeaders = nginxLogHeaders
	nginxConfig.VhostStatsSharedMemory = nginxVhostStatsSharedMemory
	nginxConfig.OpenTracingPlugin = nginxOpenTracingPluginPath
	nginxConfig.OpenTracingConfig = nginxOpenTracingConfigPath
	if registrationFrontendType == "gclb" && gclbTargetPoolPrefix != "" {
		nginxConfig.GclbHealthCheckEnabled = true
		nginxConfig.GclbTargetPoolDrainDuration = gclbTargetPoolConnectionDrainingTimeout
	}
	nginxUpdater := nginx.New(nginxConfig)

	updaters := []controller.Updater{nginxUpdater}

	switch registrationFrontendType {

	case "elb":
		elbUpdater, err := elb.New(region, elbLabelValue, elbExpectedNumber, drainDelay)
		if err != nil {
			return updaters, err
		}
		updaters = append(updaters, elbUpdater)

		statusConfig := elb_status.Config{
			Region:           region,
			LabelValue:       elbLabelValue,
			KubernetesClient: kubernetesClient,
		}
		elbStatusUpdater, err := elb_status.New(statusConfig)
		if err != nil {
			return updaters, err
		}
		updaters = append(updaters, elbStatusUpdater)

	case "alb":
		albUpdater, err := alb.New(region, targetGroupNames, targetGroupDeregistrationDelay)
		if err != nil {
			return updaters, err
		}
		updaters = append(updaters, albUpdater)

	case "gorb":
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
			return updaters, err
		}
		updaters = append(updaters, gorbUpdater)

	case "merlin":
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
			return updaters, err
		}
		updaters = append(updaters, merlinUpdater)

		if merlinInternalHostname != "" || merlinInternetFacingHostname != "" {
			statusConfig := merlin_status.Config{
				InternalHostname:       merlinInternalHostname,
				InternetFacingHostname: merlinInternetFacingHostname,
				KubernetesClient:       kubernetesClient,
			}
			merlinStatusUpdater, err := merlin_status.New(statusConfig)
			if err != nil {
				return updaters, err
			}
			updaters = append(updaters, merlinStatusUpdater)
		}

	case "gclb":
		if gclbInstanceGroupPrefix == "" && gclbTargetPoolPrefix == "" {
			return nil, errors.New("registration-frontend-type was specified as 'gclb' but neither target pool prefix or instance group prefix are set")
		}

		config := gclb.Config{
			ExpectedFrontends:                   gclbExpectedNumber,
			TargetPoolConnectionDrainingTimeout: gclbTargetPoolConnectionDrainingTimeout,
			TargetPoolPrefix:                    gclbTargetPoolPrefix,
			InstanceGroupPrefix:                 gclbInstanceGroupPrefix,
		}

		gclbStatusUpdater, err := gclb.NewUpdater(config)
		if err != nil {
			return updaters, err
		}
		updaters = append(updaters, gclbStatusUpdater)

	default:
		return nil, fmt.Errorf("invalid registration frontend type. Must be either gorb, elb, alb, merlin or gclb but"+
			"was %s", registrationFrontendType)
	}

	return updaters, nil
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
