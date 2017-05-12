package main

import (
	"flag"

	_ "net/http/pprof"

	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/alb"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/elb"
	"github.com/sky-uk/feed/haproxy"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/nginx"
	"github.com/sky-uk/feed/util/cmd"
	"github.com/sky-uk/feed/util/metrics"
)

var (
	debug                          bool
	kubeconfig                     string
	resyncPeriod                   time.Duration
	ingressPort                    int
	ingressHealthPort              int
	healthPort                     int
	elbLabelValue                  string
	region                         string
	elbExpectedNumber              int
	elbDrainDelay                  time.Duration
	targetGroupNames               cmd.CommaSeparatedValues
	targetGroupDeregistrationDelay time.Duration
	pushgatewayURL                 string
	pushgatewayIntervalSeconds     int
	pushgatewayLabels              cmd.KeyValues
	controllerConfig               controller.Config
	nginxConfig                    nginx.Conf
	nginxLogHeaders                cmd.CommaSeparatedValues
	nginxTrustedFrontends          cmd.CommaSeparatedValues
	useHaproxy                     bool
)

func init() {
	const (
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
		defaultNginxBackendKeepaliveSeconds      = 60
		defaultNginxBackendConnectTimeoutSeconds = 1
		defaultNginxLogLevel                     = "warn"
		defaultNginxServerNamesHashBucketSize    = -1
		defaultNginxServerNamesHashMaxSize       = -1
		defaultNginxProxyProtocol                = false
		defaultNginxUpdatePeriod                 = time.Second * 30
		defaultElbLabelValue                     = ""
		defaultElbDrainDelay                     = time.Second * 60
		defaultTargetGroupDeregistrationDelay    = time.Second * 300
		defaultRegion                            = "eu-west-1"
		defaultElbExpectedNumber                 = 0
		defaultPushgatewayIntervalSeconds        = 60
		defaultAccessLogDir                      = "/var/log/nginx"
	)

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
	flag.IntVar(&controllerConfig.DefaultBackendKeepAlive, "nginx-default-backend-keepalive-seconds",
		defaultNginxBackendKeepaliveSeconds,
		"Time to keep backend keepalive connections open. This should generally be set smaller than backend service keepalive "+
			"times to prevent stale connections. Can be overridden per ingress with the sky.uk/backend-keepalive-seconds annotation.")
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
	flag.IntVar(&elbExpectedNumber, "elb-expected-number", defaultElbExpectedNumber,
		"Expected number of ELBs to attach to. If 0 the controller will not check,"+
			" otherwise it fails to start if it can't attach to this number.")
	flag.DurationVar(&elbDrainDelay, "elb-drain-delay", defaultElbDrainDelay, "Delay to wait"+
		" for feed-ingress to drain from the ELB on shutdown. Should match the ELB's drain time.")

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

	flag.BoolVar(&useHaproxy, "haproxy", false,
		"Experimental: Enable haproxy implementation instead of nginx.")
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
	controllerConfig.Updaters = createIngressUpdaters()
	controller := controller.New(controllerConfig)

	cmd.AddHealthMetrics(controller, metrics.PrometheusIngressSubsystem)
	cmd.AddHealthPort(controller, healthPort)
	cmd.AddSignalHandler(controller)

	if err = controller.Start(); err != nil {
		log.Fatal("Error while starting controller: ", err)
	}
	log.Info("Controller started")

	select {}
}

func createIngressUpdaters() []controller.Updater {
	var proxy controller.Updater

	if !useHaproxy {
		nginxConfig.IngressPort = ingressPort
		nginxConfig.HealthPort = ingressHealthPort
		nginxConfig.TrustedFrontends = nginxTrustedFrontends
		nginxConfig.LogHeaders = nginxLogHeaders
		proxy = nginx.New(nginxConfig)
	} else {
		proxy = haproxy.New(haproxy.Config{})
	}

	elbAttacher := elb.New(region, elbLabelValue, elbExpectedNumber, elbDrainDelay)
	albAttacher := alb.New(region, targetGroupNames, targetGroupDeregistrationDelay)
	// update ingress proxy before attaching to front ends
	return []controller.Updater{proxy, elbAttacher, albAttacher}
}
