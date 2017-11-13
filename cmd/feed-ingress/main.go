package main

import (
	"flag"

	_ "net/http/pprof"

	"time"

	"fmt"

	"strconv"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/alb"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/elb"
	"github.com/sky-uk/feed/gorb"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/nginx"
	"github.com/sky-uk/feed/util/cmd"
	"github.com/sky-uk/feed/util/metrics"
)

var (
	debug             bool
	kubeconfig        string
	resyncPeriod      time.Duration
	ingressPort       int
	ingressHealthPort int
	healthPort        int
	region            string
	elbLabelValue     string
	elbExpectedNumber int
	// Deprecated: retained to maintain backwards compatibility. Use drainDelay instead
	legacyDrainDelay               time.Duration
	drainDelay                     time.Duration
	targetGroupNames               cmd.CommaSeparatedValues
	targetGroupDeregistrationDelay time.Duration
	pushgatewayURL                 string
	pushgatewayIntervalSeconds     int
	pushgatewayLabels              cmd.KeyValues
	controllerConfig               controller.Config
	nginxConfig                    nginx.Conf
	nginxLogHeaders                cmd.CommaSeparatedValues
	nginxTrustedFrontends          cmd.CommaSeparatedValues
	legacyBackendKeepaliveSeconds  int
	registrationFrontendType       string
	gorbIngressInstanceIP          string
	gorbEndpoint                   string
	gorbServicesDefinition         string
	gorbBackendMethod              string
	gorbBackendWeight              int
	gorbVipLoadbalancer            string
	gorbManageLoopback             bool
	gorbBackendHealthcheckInterval string
	gorbInterfaceProcFsPath        string
)

const (
	unset                                    = -1
	defaultResyncPeriod                      = time.Minute * 15
	defaultIngressPort                       = 8080
	defaultIngressAllow                      = "0.0.0.0/0"
	defaultIngressHealthPort                 = 8081
	defaultIngressStripPath                  = true
	defaultHealthPort                        = 12082
	defaultNginxBinary                       = "/usr/sbin/nginx"
	defaultNginxWorkingDir                   = "/nginx"
	defaultNginxWorkers                      = 1
	defaultNginxWorkerConnections            = 1024
	defaultNginxKeepAliveSeconds             = 60
	defaultNginxBackendKeepalives            = 512
	defaultNginxBackendTimeoutSeconds        = 60
	defaultNginxBackendConnectTimeoutSeconds = 1
	defaultNginxLogLevel                     = "warn"
	defaultNginxServerNamesHashBucketSize    = unset
	defaultNginxServerNamesHashMaxSize       = unset
	defaultNginxProxyProtocol                = false
	defaultNginxUpdatePeriod                 = time.Second * 30
	defaultElbLabelValue                     = ""
	defaultDrainDelay                        = time.Second * 30
	defaultTargetGroupDeregistrationDelay    = time.Second * 300
	defaultRegion                            = "eu-west-1"
	defaultElbExpectedNumber                 = 0
	defaultPushgatewayIntervalSeconds        = 60
	defaultAccessLogDir                      = "/var/log/nginx"
	defaultRegistrationFrontendType          = "elb"
	defaultGorbIngressInstanceIP             = "127.0.0.1"
	defaultGorbEndpoint                      = "http://127.0.0.1:80"
	defaultGorbBackendMethod                 = "dr"
	defaultGorbBackendWeight                 = 1000
	defaultGorbServicesDefinition            = "http-proxy:80,https-proxy:443"
	defaultGorbVipLoadbalancer               = "127.0.0.1"
	defaultGorbManageLoopback                = true
	defaultGorbInterfaceProcFsPath           = "/host-ipv4-proc/"
	defaultGorbBackendHealthcheckInterval    = "1s"
)

func init() {
	flag.BoolVar(&debug, "debug", false,
		"Enable debug logging.")
	flag.StringVar(&kubeconfig, "kubeconfig", "",
		"Path to kubeconfig for connecting to the apiserver. Leave blank to connect inside a cluster.")
	flag.DurationVar(&resyncPeriod, "resync-period", defaultResyncPeriod,
		"Resync with the apiserver periodically to handle missed updates.")
	flag.IntVar(&ingressPort, "ingress-port", defaultIngressPort,
		"Port to serve ingress traffic to backend services.")
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
	flag.IntVar(&healthPort, "health-port", defaultHealthPort,
		"Port for checking the health of the ingress controller on /health. Also provides /debug/pprof.")

	flag.StringVar(&nginxConfig.BinaryLocation, "nginx-binary", defaultNginxBinary,
		"Location of nginx binary.")
	flag.StringVar(&nginxConfig.WorkingDir, "nginx-workdir", defaultNginxWorkingDir,
		"Directory to store nginx files. Also the location of the nginx.tmpl file.")
	flag.IntVar(&nginxConfig.WorkerProcesses, "nginx-workers", defaultNginxWorkers,
		"Number of nginx worker processes.")
	flag.IntVar(&nginxConfig.WorkerConnections, "nginx-worker-connections", defaultNginxWorkerConnections,
		"Max number of connections per nginx worker. Includes both client and proxy connections.")
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
	flag.StringVar(&gorbEndpoint, "gorb-endpoint", defaultGorbEndpoint, "Define the endpoint to talk to gorb for registration.")
	flag.StringVar(&gorbIngressInstanceIP, "gorb-ingress-instance-ip", defaultGorbIngressInstanceIP,
		"Define the ingress instance ip, the ip of the node where feed-ingress is running.")
	flag.BoolVar(&nginxConfig.AccessLog, "access-log", false, "Enable access logs directive.")
	flag.Var(&nginxLogHeaders, "nginx-log-headers", "Comma separated list of headers to be logged in access logs")
	flag.Var(&nginxTrustedFrontends, "nginx-trusted-frontends",
		"Comma separated list of CIDRs to trust when determining the client's real IP from "+
			"frontends. The client IP is used for allowing or denying ingress access. "+
			"This will typically be the ELB subnet.")

	flag.StringVar(&region, "region", defaultRegion,
		"AWS region for frontend attachment.")

	flag.StringVar(&elbLabelValue, "elb-label-value", defaultElbLabelValue,
		"Attach to ELBs tagged with "+elb.ElbTag+"=value. Leave empty to not attach.")
	flag.StringVar(&registrationFrontendType, "registration-frontend-type", defaultRegistrationFrontendType,
		"Define the registration frontend type. Must be gorb, elb or alb.")
	flag.IntVar(&elbExpectedNumber, "elb-expected-number", defaultElbExpectedNumber,
		"Expected number of ELBs to attach to. If 0 the controller will not check,"+
			" otherwise it fails to start if it can't attach to this number.")
	flag.DurationVar(&legacyDrainDelay, "elb-drain-delay", unset, "Deprecated. Used drain-delay instead.")
	flag.DurationVar(&drainDelay, "drain-delay", unset, "Delay to wait"+
		" for feed-ingress to drain from the registration component on shutdown. Should match the ELB's drain time.")
	flag.Var(&targetGroupNames, "alb-target-group-names",
		"Names of ALB target groups to attach to, separated by commas.")
	flag.DurationVar(&targetGroupDeregistrationDelay, "alb-target-group-deregistration-delay",
		defaultTargetGroupDeregistrationDelay,
		"Delay to wait for feed-ingress to deregister from the ALB target group on shutdown. Should match"+
			" the target group setting in AWS.")

	flag.StringVar(&pushgatewayURL, "pushgateway", "",
		"Prometheus pushgateway URL for pushing metrics. Leave blank to not push metrics.")
	flag.IntVar(&pushgatewayIntervalSeconds, "pushgateway-interval", defaultPushgatewayIntervalSeconds,
		"Interval in seconds for pushing metrics.")
	flag.Var(&pushgatewayLabels, "pushgateway-label",
		"A label=value pair to attach to metrics pushed to prometheus. Specify multiple times for multiple labels.")
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
		"Define the gorb interval http healthcheck for the backend")

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

	controllerConfig.Updaters, err = createIngressUpdaters()
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

func createIngressUpdaters() ([]controller.Updater, error) {
	nginxConfig.IngressPort = ingressPort
	nginxConfig.HealthPort = ingressHealthPort
	nginxConfig.TrustedFrontends = nginxTrustedFrontends
	nginxConfig.LogHeaders = nginxLogHeaders
	nginxUpdater := nginx.New(nginxConfig)

	if legacyDrainDelay != unset && drainDelay == unset {
		drainDelay = legacyDrainDelay
	} else if drainDelay == unset {
		drainDelay = defaultDrainDelay
	}

	updaters := []controller.Updater{nginxUpdater}

	if registrationFrontendType == "elb" {
		elbUpdater, err := elb.New(region, elbLabelValue, elbExpectedNumber, drainDelay)
		if err != nil {
			return updaters, err
		}
		updaters = append(updaters, elbUpdater)
	} else if registrationFrontendType == "alb" {
		albUpdater, err := alb.New(region, targetGroupNames, targetGroupDeregistrationDelay)
		if err != nil {
			return updaters, err
		}
		updaters = append(updaters, albUpdater)
	} else if registrationFrontendType == "gorb" {
		virtualServices, err := toVirtualServices(gorbServicesDefinition)
		if err != nil {
			return nil, fmt.Errorf("invalid gorb services definition. Must be a comma separated list - e.g. 'http-proxy:80,https-proxy:443', but was %s", gorbServicesDefinition)
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
			InterfaceProcFsPath:        gorbInterfaceProcFsPath,
		}
		gorbUpdater, err := gorb.New(&config)
		if err != nil {
			return updaters, err
		}
		updaters = append(updaters, gorbUpdater)
	} else {
		return nil, fmt.Errorf("invalid registration frontend type. Must be either gorb, elb, alb but was %s", registrationFrontendType)
	}

	// update nginx before attaching to front ends
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
