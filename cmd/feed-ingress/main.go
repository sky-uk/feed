package main

import (
	"flag"
	"os"

	_ "net/http/pprof"

	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/elb"
	"github.com/sky-uk/feed/nginx"
	"github.com/sky-uk/feed/util/cmd"
	"github.com/sky-uk/feed/util/metrics"
)

var (
	debug                          bool
	apiserverURL                   string
	caCertFile                     string
	tokenFile                      string
	clientCertFile                 string
	clientKeyFile                  string
	ingressPort                    int
	ingressAllow                   string
	ingressHealthPort              int
	ingressStripPath               bool
	healthPort                     int
	nginxBinary                    string
	nginxWorkDir                   string
	nginxWorkerProcesses           int
	nginxWorkerConnections         int
	nginxKeepAliveSeconds          int
	nginxBackendKeepalives         int
	nginxBackendKeepaliveSeconds   int
	nginxLogLevel                  string
	nginxTrustedFrontends          string
	nginxServerNamesHashBucketSize int
	nginxServerNamesHashMaxSize    int
	elbLabelValue                  string
	elbRegion                      string
	elbExpectedNumber              int
	pushgatewayURL                 string
	pushgatewayIntervalSeconds     int
	pushgatewayLabels              cmd.KeyValues
)

func init() {
	const (
		defaultAPIServer                      = "https://kubernetes:443"
		defaultCaCertFile                     = "/run/secrets/kubernetes.io/serviceaccount/ca.crt"
		defaultTokenFile                      = ""
		defaultClientCertFile                 = ""
		defaultClientKeyFile                  = ""
		defaultIngressPort                    = 8080
		defaultIngressAllow                   = ""
		defaultIngressHealthPort              = 8081
		defaultIngressStripPath               = true
		defaultHealthPort                     = 12082
		defaultNginxBinary                    = "/usr/sbin/nginx"
		defaultNginxWorkingDir                = "/nginx"
		defaultNginxWorkers                   = 1
		defaultNginxWorkerConnections         = 1024
		defaultNginxKeepAliveSeconds          = 60
		defaultNginxBackendKeepalives         = 512
		defaultNginxBackendKeepaliveSeconds   = 60
		defaultNginxLogLevel                  = "warn"
		defaultNginxServerNamesHashBucketSize = -1
		defaultNginxServerNamesHashMaxSize    = -1
		defaultElbLabelValue                  = ""
		defaultElbRegion                      = "eu-west-1"
		defaultElbExpectedNumber              = 0
		defaultPushgatewayIntervalSeconds     = 60
	)

	flag.BoolVar(&debug, "debug", false,
		"Enable debug logging.")
	flag.StringVar(&apiserverURL, "apiserver", defaultAPIServer,
		"Kubernetes API server URL.")
	flag.StringVar(&caCertFile, "cacertfile", defaultCaCertFile,
		"File containing the Kubernetes API server certificate.")
	flag.StringVar(&tokenFile, "tokenfile", defaultTokenFile,
		"File containing kubernetes client authentication token. Leave empty to not use a token.")
	flag.StringVar(&clientCertFile, "client-certfile", defaultClientCertFile,
		"File containing client certificate. Leave empty to not use a client certificate.")
	flag.StringVar(&clientKeyFile, "client-keyfile", defaultClientKeyFile,
		"File containing client key. Leave empty to not use a client certificate.")
	flag.IntVar(&ingressPort, "ingress-port", defaultIngressPort,
		"Port to serve ingress traffic to backend services.")
	flag.IntVar(&ingressHealthPort, "ingress-health-port", defaultIngressHealthPort,
		"Port for ingress /health and /status pages. Should be used by frontends to determine if ingress is available.")
	flag.StringVar(&ingressAllow, "ingress-allow", defaultIngressAllow,
		"Source IP or CIDR to allow ingress access by default. This is overridden by the sky.uk/allow "+
			"annotation on ingress resources. Leave empty to deny all access by default.")
	flag.BoolVar(&ingressStripPath, "ingress-strip-path", defaultIngressStripPath,
		"Whether to strip the ingress path from the URL before passing to backend services. For example, "+
			"if enabled 'myhost/myapp/health' would be passed as '/health' to the backend service. If disabled, "+
			"it would be passed as '/myapp/health'. Enabling this requires nginx to process the URL, which has some "+
			"limitations. URL encoded characters will not work correctly in some cases, and backend services will "+
			"need to take care to properly construct URLs, such as by using the 'X-Original-URI' header."+
			"Can be overridden with the sky.uk/strip-path annotation per ingress")
	flag.IntVar(&healthPort, "health-port", defaultHealthPort,
		"Port for checking the health of the ingress controller on /health. Also provides /debug/pprof.")
	flag.StringVar(&nginxBinary, "nginx-binary", defaultNginxBinary,
		"Location of nginx binary.")
	flag.StringVar(&nginxWorkDir, "nginx-workdir", defaultNginxWorkingDir,
		"Directory to store nginx files. Also the location of the nginx.tmpl file.")
	flag.IntVar(&nginxWorkerProcesses, "nginx-workers", defaultNginxWorkers,
		"Number of nginx worker processes.")
	flag.IntVar(&nginxWorkerConnections, "nginx-worker-connections", defaultNginxWorkerConnections,
		"Max number of connections per nginx worker. Includes both client and proxy connections.")
	flag.IntVar(&nginxKeepAliveSeconds, "nginx-keepalive-seconds", defaultNginxKeepAliveSeconds,
		"Keep alive time for persistent client connections to nginx. Should generally be set larger than frontend "+
			"keep alive times to prevent stale connections.")
	flag.IntVar(&nginxBackendKeepalives, "nginx-backend-keepalive-count", defaultNginxBackendKeepalives,
		"Maximum number of keepalive connections per backend service. Keepalive connections count against"+
			" nginx-worker-connections limit, and will be restricted by that global limit as well.")
	flag.IntVar(&nginxBackendKeepaliveSeconds, "nginx-default-backend-keepalive-seconds", defaultNginxBackendKeepaliveSeconds,
		"Time to keep backend keepalive connections open. This should generally be set smaller than backend service keepalive "+
			"times to prevent stale connections. Can be overridden per ingress the sky.uk/backend-keepalive-seconds annotation")
	flag.StringVar(&nginxLogLevel, "nginx-loglevel", defaultNginxLogLevel,
		"Log level for nginx. See http://nginx.org/en/docs/ngx_core_module.html#error_log for levels.")
	flag.IntVar(&nginxServerNamesHashBucketSize, "nginx-server-names-hash-bucket-size", defaultNginxServerNamesHashBucketSize,
		"Sets the bucket size for the server names hash tables. Setting this to 0 or less will exclude this "+
			"config from the nginx conf file. The details of setting up hash tables are provided "+
			"in a separate document. http://nginx.org/en/docs/hash.html")
	flag.IntVar(&nginxServerNamesHashMaxSize, "nginx-server-names-hash-max-size", defaultNginxServerNamesHashMaxSize,
		"Sets the maximum size of the server names hash tables. Setting this to 0 or less will exclude this "+
			"config from the nginx conf file. The details of setting up hash tables are provided "+
			"in a separate document. http://nginx.org/en/docs/hash.html")
	flag.StringVar(&nginxTrustedFrontends, "nginx-trusted-frontends", "",
		"Comma separated list of CIDRs to trust when determining the client's real IP from the "+
			"X-Forwarded-For header. The client IP is used for allowing or denying ingress access. "+
			"This will typically be the ELB subnet.")
	flag.StringVar(&elbLabelValue, "elb-label-value", defaultElbLabelValue,
		"Attach to ELBs tagged with "+elb.ElbTag+"=value. Leave empty to not attach.")
	flag.IntVar(&elbExpectedNumber, "elb-expected-number", defaultElbExpectedNumber,
		"Expected number of ELBs to attach to. If 0 the controller will not check,"+
			" otherwise it fails to start if it can't attach to this number.")
	flag.StringVar(&elbRegion, "elb-region", defaultElbRegion,
		"AWS region for ELBs.")
	flag.StringVar(&pushgatewayURL, "pushgateway", "",
		"Prometheus pushgateway URL for pushing metrics. Leave blank to not push metrics.")
	flag.IntVar(&pushgatewayIntervalSeconds, "pushgateway-interval", defaultPushgatewayIntervalSeconds,
		"Interval in seconds for pushing metrics.")
	flag.Var(&pushgatewayLabels, "pushgateway-label",
		"A label=value pair to attach to metrics pushed to prometheus. Specify multiple times for multiple labels.")
}

func main() {
	flag.Parse()

	cmd.ConfigureLogging(debug)
	cmd.ConfigureMetrics("feed-ingress", pushgatewayLabels, pushgatewayURL, pushgatewayIntervalSeconds)

	client := cmd.CreateK8sClient(caCertFile, tokenFile, apiserverURL, clientCertFile, clientKeyFile)
	updaters := createIngressUpdaters()

	controller := controller.New(controller.Config{
		KubernetesClient:        client,
		Updaters:                updaters,
		DefaultAllow:            ingressAllow,
		DefaultStripPath:        ingressStripPath,
		DefaultBackendKeepAlive: nginxBackendKeepaliveSeconds,
	})

	cmd.AddHealthMetrics(controller, metrics.PrometheusIngressSubsystem)
	cmd.AddHealthPort(controller, healthPort)
	cmd.AddSignalHandler(controller)

	err := controller.Start()
	if err != nil {
		log.Error("Error while starting controller: ", err)
		os.Exit(-1)
	}
	log.Info("Controller started")

	select {}
}

func createIngressUpdaters() []controller.Updater {
	frontend := elb.New(elbRegion, elbLabelValue, elbExpectedNumber)
	trustedFrontends := []string{}
	if nginxTrustedFrontends != "" {
		trustedFrontends = strings.Split(nginxTrustedFrontends, ",")
	}
	proxy := nginx.New(nginx.Conf{
		BinaryLocation:            nginxBinary,
		IngressPort:               ingressPort,
		WorkingDir:                nginxWorkDir,
		WorkerProcesses:           nginxWorkerProcesses,
		WorkerConnections:         nginxWorkerConnections,
		KeepaliveSeconds:          nginxKeepAliveSeconds,
		BackendKeepalives:         nginxBackendKeepalives,
		LogLevel:                  nginxLogLevel,
		ServerNamesHashBucketSize: nginxServerNamesHashBucketSize,
		ServerNamesHashMaxSize:    nginxServerNamesHashMaxSize,
		HealthPort:                ingressHealthPort,
		TrustedFrontends:          trustedFrontends,
	})
	return []controller.Updater{frontend, proxy}
}
