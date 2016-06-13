package main

import (
	"flag"
	"os"

	_ "net/http/pprof"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/elb"
	"github.com/sky-uk/feed/nginx"
	"github.com/sky-uk/feed/util/cmd"
)

var (
	debug                  bool
	apiServer              string
	caCertFile             string
	tokenFile              string
	ingressPort            int
	ingressAllow           string
	ingressHealthPort      int
	healthPort             int
	nginxBinary            string
	nginxWorkDir           string
	nginxWorkerProcesses   int
	nginxWorkerConnections int
	nginxKeepAliveSeconds  int
	nginxLogLevel          string
	clusterName            string
	region                 string
	expectedFrontends      int
)

func init() {
	const (
		defaultAPIServer              = "https://kubernetes:443"
		defaultCaCertFile             = "/run/secrets/kubernetes.io/serviceaccount/ca.crt"
		defaultTokenFile              = "/run/secrets/kubernetes.io/serviceaccount/token"
		defaultIngressPort            = 8080
		defaultIngressAllow           = ""
		defaultIngressHealthPort      = 8081
		defaultHealthPort             = 12082
		defaultNginxBinary            = "/usr/sbin/nginx"
		defaultNginxWorkingDir        = "/nginx"
		defaultNginxWorkers           = 1
		defaultNginxWorkerConnections = 1024
		defaultNginxKeepAliveSeconds  = 65
		defaultNginxLogLevel          = "info"
		defaultClusterName            = "cluster"
		defaultRegion                 = "eu-west-1"
		defaultExpectedFrontends      = 0
	)

	flag.BoolVar(&debug, "debug", false,
		"Enable debug logging.")
	flag.StringVar(&apiServer, "apiserver", defaultAPIServer,
		"Kubernetes API server URL.")
	flag.StringVar(&caCertFile, "cacertfile", defaultCaCertFile,
		"File containing kubernetes ca certificate.")
	flag.StringVar(&tokenFile, "tokenfile", defaultTokenFile,
		"File containing kubernetes client authentication token.")
	flag.IntVar(&ingressPort, "ingress-port", defaultIngressPort,
		"Port to serve ingress traffic to backend services.")
	flag.IntVar(&ingressHealthPort, "ingress-health-port", defaultIngressHealthPort,
		"Port for ingress /health and /status pages. Should be used by frontends to determine if ingress is available.")
	flag.StringVar(&ingressAllow, "ingress-allow", defaultIngressAllow,
		"Source IP or CIDR to allow ingress access by default. This is in addition to the sky.uk/allow "+
			"annotation on ingress resources. Leave empty to deny all access by default.")
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
		"Keep alive time for persistent client connections to nginx.")
	flag.StringVar(&nginxLogLevel, "nginx-loglevel", defaultNginxLogLevel,
		"Log level for nginx. See http://nginx.org/en/docs/ngx_core_module.html#error_log for levels.")
	flag.StringVar(&clusterName, "cluster-name", defaultClusterName,
		"Kubernetes cluster name. ELBs tagged wtih `sky.uk/KubernetesClusterFrontend` will be added as frontends.")
	flag.StringVar(&region, "aws-region", defaultRegion,
		"AWS region")
	flag.IntVar(&expectedFrontends, "expected-frontends", defaultExpectedFrontends,
		"Expected number of front ends. If 0 the controller will not attempt to attach. A warning is logged if the incorrect number are found.")
}

func main() {
	flag.Parse()
	cmd.ConfigureLogging(debug)

	client := cmd.CreateK8sClient(caCertFile, tokenFile, apiServer)
	updaters := createIngressUpdaters()

	controller := controller.New(controller.Config{
		KubernetesClient: client,
		Updaters:         updaters,
	})

	cmd.ConfigureHealthPort(controller, healthPort)
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
	frontend := elb.New(region, clusterName, expectedFrontends)
	proxy := nginx.New(nginx.Conf{
		BinaryLocation:    nginxBinary,
		IngressPort:       ingressPort,
		WorkingDir:        nginxWorkDir,
		WorkerProcesses:   nginxWorkerProcesses,
		WorkerConnections: nginxWorkerConnections,
		KeepAliveSeconds:  nginxKeepAliveSeconds,
		HealthPort:        ingressHealthPort,
		DefaultAllow:      ingressAllow,
	})
	return []controller.Updater{frontend, proxy}
}
