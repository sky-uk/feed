package main

import (
	"flag"
	"os"

	_ "net/http/pprof"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/ingress"
	"github.com/sky-uk/feed/ingress/nginx"
	"github.com/sky-uk/feed/ingress/types"
	"github.com/sky-uk/feed/util/cmd"
)

var (
	debug                  bool
	apiServer              string
	caCertFile             string
	tokenFile              string
	ingressPort            int
	ingressAllow           string
	ingressStatusPort      int
	healthPort             int
	nginxBinary            string
	nginxWorkDir           string
	nginxResolver          string
	serviceDomain          string
	nginxWorkerProcesses   int
	nginxWorkerConnections int
	nginxKeepAliveSeconds  int
)

func init() {
	const (
		defaultAPIServer              = "https://kubernetes:443"
		defaultCaCertFile             = "/run/secrets/kubernetes.io/serviceaccount/ca.crt"
		defaultTokenFile              = "/run/secrets/kubernetes.io/serviceaccount/token"
		defaultIngressPort            = 8080
		defaultIngressAllow           = ""
		defaultIngressStatusPort      = 8081
		defaultHealthPort             = 12082
		defaultServiceDomain          = "svc.cluster"
		defaultNginxBinary            = "/usr/sbin/nginx"
		defaultNginxWorkingDir        = "/nginx"
		defaultNginxWorkers           = 1
		defaultNginxWorkerConnections = 1024
		defaultNginxKeepAliveSeconds  = 65
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
	flag.IntVar(&ingressStatusPort, "ingress-status-port", defaultIngressStatusPort,
		"Port for ingress /health and /status pages. Should be used by frontends to determine if ingress is available.")
	flag.StringVar(&ingressAllow, "ingress-allow", defaultIngressAllow,
		"Source IP or CIDR to allow ingress access by default. This is in addition to the sky.uk/allow "+
			"annotation on ingress resources. Leave empty to deny all access by default.")
	flag.IntVar(&healthPort, "health-port", defaultHealthPort,
		"Port for checking the health of the ingress controller.")
	flag.StringVar(&serviceDomain, "service-domain", defaultServiceDomain,
		"Search domain for backend services. For kube2sky, this should be svc.<cluster-name>.")
	flag.StringVar(&nginxBinary, "nginx-binary", defaultNginxBinary,
		"Location of nginx binary.")
	flag.StringVar(&nginxWorkDir, "nginx-workdir", defaultNginxWorkingDir,
		"Directory to store nginx files. Also the location of the nginx.tmpl file.")
	flag.StringVar(&nginxResolver, "nginx-resolver", "",
		"Address to resolve DNS entries for backends. Leave blank to use host DNS resolving.")
	flag.IntVar(&nginxWorkerProcesses, "nginx-workers", defaultNginxWorkers,
		"Number of nginx worker processes.")
	flag.IntVar(&nginxWorkerConnections, "nginx-worker-connections", defaultNginxWorkerConnections,
		"Max number of connections per nginx worker. Includes both client and proxy connections.")
	flag.IntVar(&nginxKeepAliveSeconds, "nginx-keepalive-seconds", defaultNginxKeepAliveSeconds,
		"Keep alive time for persistent client connections to nginx.")
}

func main() {
	flag.Parse()
	cmd.ConfigureLogging(debug)

	lb := createLB()
	client := cmd.CreateK8sClient(caCertFile, tokenFile, apiServer)
	controller := ingress.New(ingress.Config{
		LoadBalancer:     lb,
		KubernetesClient: client,
		ServiceDomain:    serviceDomain,
	})

	cmd.ConfigureHealthPort(controller, healthPort)
	cmd.AddSignalHandler(controller)

	err := controller.Start()
	if err != nil {
		log.Error("Error while starting controller: ", err)
		os.Exit(-1)
	}

	select {}
}

func createLB() types.LoadBalancer {
	return nginx.NewNginxLB(nginx.Conf{
		BinaryLocation:    nginxBinary,
		IngressPort:       ingressPort,
		WorkingDir:        nginxWorkDir,
		WorkerProcesses:   nginxWorkerProcesses,
		WorkerConnections: nginxWorkerConnections,
		KeepAliveSeconds:  nginxKeepAliveSeconds,
		StatusPort:        ingressStatusPort,
		Resolver:          nginxResolver,
		DefaultAllow:      ingressAllow,
	})
}
