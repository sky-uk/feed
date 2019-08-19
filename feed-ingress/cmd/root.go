package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/nginx"
	"github.com/sky-uk/feed/util/cmd"
	"github.com/spf13/cobra"
)

var (
	// version of binary, injected by "go tool link -X"
	version string
	// buildTime of binary, injected by "go tool link -X"
	buildTime string

	rootCmd = &cobra.Command{
		Use:     "feed-ingress",
		Version: printVersion(),
		Short:   "Feed Ingress is a Kubernetes Ingress Controller",
		Long: `Feed Ingress attaches to AWS ELBs and routes traffic from them to the Kubernetes
Services declared by Ingress resources. It manages an NGINX instance whose
configuration it updates to keep track with changes in the cluster.`,
	}
)

var (
	debug             bool
	kubeconfig        string
	resyncPeriod      time.Duration
	ingressPort       int
	ingressHTTPSPort  int
	ingressHealthPort int
	controllerConfig  controller.Config
	healthPort        int

	nginxConfig                 nginx.Conf
	nginxLogHeaders             []string
	nginxTrustedFrontends       []string
	nginxSSLPath                string
	nginxVhostStatsSharedMemory int
	nginxOpenTracingPluginPath  string
	nginxOpenTracingConfigPath  string

	ingressControllerName   string
	includeUnnamedIngresses bool
	namespaceSelector       string

	pushgatewayURL             string
	pushgatewayIntervalSeconds int
	pushgatewayLabels          cmd.KeyValues
)

const (
	unset = -1

	defaultResyncPeriod      = time.Minute * 15
	defaultIngressPort       = 8080
	defaultIngressHTTPSPort  = unset
	defaultIngressHealthPort = 8081
	defaultIngressAllow      = "0.0.0.0/0"
	defaultIngressStripPath  = true
	defaultIngressExactPath  = false
	defaultHealthPort        = 12082

	defaultNginxBinary                       = "/usr/sbin/nginx"
	defaultNginxWorkingDir                   = "/nginx"
	defaultNginxWorkers                      = 1
	defaultNginxWorkerConnections            = 1024
	defaultNginxWorkerShutdownTimeoutSeconds = 0
	defaultNginxKeepAliveSeconds             = 60
	defaultNginxBackendKeepalives            = 512
	defaultNginxBackendTimeoutSeconds        = 60
	defaultNginxBackendConnectTimeoutSeconds = 1
	defaultNginxBackendMaxConnections        = 0
	defaultNginxProxyBufferSize              = 16
	defaultNginxProxyBufferBlocks            = 4
	defaultNginxLogLevel                     = "warn"
	defaultNginxServerNamesHashBucketSize    = unset
	defaultNginxServerNamesHashMaxSize       = unset
	defaultNginxProxyProtocol                = false
	defaultNginxUpdatePeriod                 = time.Second * 30
	defaultNginxSSLPath                      = "/etc/ssl/default-ssl/default-ssl"
	defaultNginxVhostStatsSharedMemory       = 1
	defaultNginxOpenTracingPluginPath        = ""
	defaultNginxOpenTracingConfigPath        = ""
	defaultAccessLogDir                      = "/var/log/nginx"
	defaultClientHeaderBufferSize            = 16
	defaultClientBodyBufferSize              = 16
	defaultLargeClientHeaderBufferBlocks     = 4

	defaultIngressControllerName              = ""
	defaultIncludeUnnamedIngresses            = false
	defaultIngressControllerNamespaceSelector = ""

	defaultPushgatewayIntervalSeconds = 60
)

const (
	ingressControllerNameFlag              = "ingress-controller-name"
	includeUnnamedIngressesFlag            = "include-unnamed-ingresses"
	ingressControllerNamespaceSelectorFlag = "ingress-controller-namespace-selector"
)

func init() {
	configureGeneralFlags()
	configureNginxFlags()
	configurePrometheusFlags()
}

func configureGeneralFlags() {
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false,
		"Enable debug logging.")
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "",
		"Path to kubeconfig for connecting to the apiserver. Leave blank to connect inside a cluster.")
	rootCmd.PersistentFlags().DurationVar(&resyncPeriod, "resync-period", defaultResyncPeriod,
		"Resync with the apiserver periodically to handle missed updates.")
	rootCmd.PersistentFlags().IntVar(&ingressPort, "ingress-port", defaultIngressPort,
		"Port to serve ingress traffic to backend services.")
	rootCmd.PersistentFlags().IntVar(&ingressHTTPSPort, "ingress-https-port", defaultIngressHTTPSPort,
		"Port to serve ingress https traffic to backend services.")
	rootCmd.PersistentFlags().IntVar(&ingressHealthPort, "ingress-health-port", defaultIngressHealthPort,
		"Port for ingress /health and /status pages. Should be used by frontends to determine if ingress is available.")
	rootCmd.PersistentFlags().StringVar(&controllerConfig.DefaultAllow, "ingress-allow", defaultIngressAllow,
		"Source IP or CIDR to allow ingress access by default. This is overridden by the sky.uk/allow "+
			"annotation on ingress resources. Leave empty to deny all access by default.")
	rootCmd.PersistentFlags().BoolVar(&controllerConfig.DefaultStripPath, "ingress-strip-path", defaultIngressStripPath,
		"Whether to strip the ingress path from the URL before passing to backend services. For example, "+
			"if enabled 'myhost/myapp/health' would be passed as '/health' to the backend service. If disabled, "+
			"it would be passed as '/myapp/health'. Enabling this requires nginx to process the URL, which has some "+
			"limitations. URL encoded characters will not work correctly in some cases, and backend services will "+
			"need to take care to properly construct URLs, such as by using the 'X-Original-URI' header."+
			"Can be overridden with the sky.uk/strip-path annotation per ingress")
	rootCmd.PersistentFlags().BoolVar(&controllerConfig.DefaultExactPath, "ingress-exact-path", defaultIngressExactPath,
		"Whether to consider the ingress path to be an exact match rather than as a prefix. For example, "+
			"if enabled 'myhost/myapp/health' would match 'myhost/myapp/health' but not 'myhost/myapp/health/x'."+
			" If disabled, it would match both (and redirect requests from 'myhost/myapp/health' to "+
			" '/myhost/myapp/health/'. Can be overridden with the sky.uk/exact-path annotation per ingress")
	rootCmd.PersistentFlags().IntVar(&healthPort, "health-port", defaultHealthPort,
		"Port for checking the health of the ingress controller on /health. Also provides /debug/pprof.")
	rootCmd.PersistentFlags().StringVar(&ingressControllerName, ingressControllerNameFlag, defaultIngressControllerName,
		"The name of this instance. It will consider only ingress resources with matching sky.uk/ingress-controller-name annotation values.")
	rootCmd.PersistentFlags().BoolVar(&includeUnnamedIngresses, includeUnnamedIngressesFlag, defaultIncludeUnnamedIngresses,
		"In addition to ingress resources with matching sky.uk/ingress-controller-name annotations, also consider those with no such annotation.")
	rootCmd.PersistentFlags().StringVar(&namespaceSelector, ingressControllerNamespaceSelectorFlag, defaultIngressControllerNamespaceSelector,
		"Only consider ingresses within namespaces having labels matching this selector (e.g. app=loadtest).")

	_ = rootCmd.PersistentFlags().MarkDeprecated(includeUnnamedIngressesFlag,
		"please annotate ingress resources explicitly with sky.uk/ingress-controller-name")
}

func configureNginxFlags() {
	rootCmd.PersistentFlags().StringVar(&nginxConfig.BinaryLocation, "nginx-binary", defaultNginxBinary,
		"Location of nginx binary.")
	rootCmd.PersistentFlags().StringVar(&nginxConfig.WorkingDir, "nginx-workdir", defaultNginxWorkingDir,
		"Directory to store nginx files. Also the location of the nginx.tmpl file.")
	rootCmd.PersistentFlags().IntVar(&nginxConfig.WorkerProcesses, "nginx-workers", defaultNginxWorkers,
		"Number of nginx worker processes.")
	rootCmd.PersistentFlags().IntVar(&nginxConfig.WorkerConnections, "nginx-worker-connections", defaultNginxWorkerConnections,
		"Max number of connections per nginx worker. Includes both client and proxy connections.")
	rootCmd.PersistentFlags().IntVar(&nginxConfig.WorkerShutdownTimeoutSeconds, "nginx-worker-shutdown-timeout-seconds", defaultNginxWorkerShutdownTimeoutSeconds,
		"Timeout for a graceful shutdown of worker processes.")
	rootCmd.PersistentFlags().IntVar(&nginxConfig.KeepaliveSeconds, "nginx-keepalive-seconds", defaultNginxKeepAliveSeconds,
		"Keep alive time for persistent client connections to nginx. Should generally be set larger than frontend "+
			"keep alive times to prevent stale connections.")
	rootCmd.PersistentFlags().IntVar(&nginxConfig.BackendKeepalives, "nginx-backend-keepalive-count", defaultNginxBackendKeepalives,
		"Maximum number of keepalive connections per backend service. Keepalive connections count against"+
			" nginx-worker-connections limit, and will be restricted by that global limit as well.")
	rootCmd.PersistentFlags().IntVar(&controllerConfig.DefaultBackendTimeoutSeconds, "nginx-default-backend-timeout-seconds",
		defaultNginxBackendTimeoutSeconds,
		"Timeout for requests to backends. Can be overridden per ingress with the sky.uk/backend-timeout-seconds annotation.")
	rootCmd.PersistentFlags().IntVar(&nginxConfig.BackendConnectTimeoutSeconds, "nginx-backend-connect-timeout-seconds",
		defaultNginxBackendConnectTimeoutSeconds,
		"Connect timeout to backend services.")
	rootCmd.PersistentFlags().IntVar(&controllerConfig.DefaultBackendMaxConnections, "nginx-default-backend-max-connections",
		defaultNginxBackendMaxConnections,
		"Maximum number of connections to a single backend. Can be overridden per ingress with the sky.uk/backend-max-connections annotation.")
	rootCmd.PersistentFlags().IntVar(&controllerConfig.DefaultProxyBufferSize, "nginx-default-proxy-buffer-size",
		defaultNginxProxyBufferSize,
		"Proxy buffer size for response. Can be overridden per ingress with the sky.uk/proxy-buffer-size-in-kb annotation.")
	rootCmd.PersistentFlags().IntVar(&controllerConfig.DefaultProxyBufferBlocks, "nginx-default-proxy-buffer-blocks",
		defaultNginxProxyBufferBlocks,
		"Proxy buffer blocks for response. Can be overridden per ingress with the sky.uk/proxy-buffer-blocks annotation.")
	rootCmd.PersistentFlags().StringVar(&nginxConfig.LogLevel, "nginx-loglevel", defaultNginxLogLevel,
		"Log level for nginx. See http://nginx.org/en/docs/ngx_core_module.html#error_log for levels.")
	rootCmd.PersistentFlags().IntVar(&nginxConfig.ServerNamesHashBucketSize, "nginx-server-names-hash-bucket-size", defaultNginxServerNamesHashBucketSize,
		"Sets the bucket size for the server names hash tables. Setting this to 0 or less will exclude this "+
			"config from the nginx conf file. The details of setting up hash tables are provided "+
			"in a separate document. http://nginx.org/en/docs/hash.html")
	rootCmd.PersistentFlags().IntVar(&nginxConfig.ServerNamesHashMaxSize, "nginx-server-names-hash-max-size", defaultNginxServerNamesHashMaxSize,
		"Sets the maximum size of the server names hash tables. Setting this to 0 or less will exclude this "+
			"config from the nginx conf file. The details of setting up hash tables are provided "+
			"in a separate document. http://nginx.org/en/docs/hash.html")
	rootCmd.PersistentFlags().BoolVar(&nginxConfig.ProxyProtocol, "nginx-proxy-protocol", defaultNginxProxyProtocol,
		"Enable PROXY protocol for nginx listeners.")
	rootCmd.PersistentFlags().DurationVar(&nginxConfig.UpdatePeriod, "nginx-update-period", defaultNginxUpdatePeriod,
		"How often nginx reloads can occur. Too frequent will result in many nginx worker processes alive at the same time.")
	rootCmd.PersistentFlags().StringVar(&nginxConfig.AccessLogDir, "access-log-dir", defaultAccessLogDir, "Access logs direcoty.")
	rootCmd.PersistentFlags().BoolVar(&nginxConfig.AccessLog, "access-log", false, "Enable access logs directive.")
	rootCmd.PersistentFlags().StringSliceVar(&nginxLogHeaders, "nginx-log-headers", []string{}, "Comma separated list of headers to be logged in access logs")
	rootCmd.PersistentFlags().StringSliceVar(&nginxTrustedFrontends, "nginx-trusted-frontends", []string{},
		"Comma separated list of CIDRs to trust when determining the client's real IP from "+
			"frontends. The client IP is used for allowing or denying ingress access. "+
			"This will typically be the ELB subnet.")
	rootCmd.PersistentFlags().StringVar(&nginxSSLPath, "ssl-path", defaultNginxSSLPath,
		"Set default ssl path + name file without extension.  Feed expects two files: one ending in .crt (the CA) and the other in .key (the private key).")
	rootCmd.PersistentFlags().IntVar(&nginxVhostStatsSharedMemory, "nginx-vhost-stats-shared-memory", defaultNginxVhostStatsSharedMemory,
		"Memory (in MiB) which should be allocated for use by the vhost statistics module")
	rootCmd.PersistentFlags().StringVar(&nginxOpenTracingPluginPath, "nginx-opentracing-plugin-path", defaultNginxOpenTracingPluginPath,
		"Path to OpenTracing plugin on disk (eg. /usr/local/lib/libjaegertracing_plugin.so)")
	rootCmd.PersistentFlags().StringVar(&nginxOpenTracingConfigPath, "nginx-opentracing-config-path", defaultNginxOpenTracingConfigPath,
		"Path to OpenTracing config on disk (eg. /etc/jaeger-nginx-config.json)")
	rootCmd.PersistentFlags().IntVar(&nginxConfig.ClientHeaderBufferSize, "nginx-client-header-buffer-size-in-kb", defaultClientHeaderBufferSize, "Sets buffer size for reading client request header")
	rootCmd.PersistentFlags().IntVar(&nginxConfig.ClientBodyBufferSize, "nginx-client-body-buffer-size-in-kb", defaultClientBodyBufferSize, "Sets buffer size for reading client request body")
	rootCmd.PersistentFlags().IntVar(&nginxConfig.LargeClientHeaderBufferBlocks, "nginx-large-client-header-buffer-blocks", defaultLargeClientHeaderBufferBlocks, "Sets the maximum number of buffers used for reading large client request header")
}

func configurePrometheusFlags() {
	rootCmd.PersistentFlags().StringVar(&pushgatewayURL, "pushgateway", "",
		"Prometheus pushgateway URL for pushing metrics. Leave blank to not push metrics.")
	rootCmd.PersistentFlags().IntVar(&pushgatewayIntervalSeconds, "pushgateway-interval", defaultPushgatewayIntervalSeconds,
		"Interval in seconds for pushing metrics.")
	rootCmd.PersistentFlags().Var(&pushgatewayLabels, "pushgateway-label",
		"A label=value pair to attach to metrics pushed to prometheus. Specify multiple times for multiple labels.")
}

func printVersion() string {
	return fmt.Sprintf("%s (%s)", version, buildTime)
}

// Execute is the entry point for Cobra commands
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
