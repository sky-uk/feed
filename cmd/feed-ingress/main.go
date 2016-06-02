package main

import (
	"flag"
	"os"
	"os/signal"

	"io/ioutil"
	"net/http"
	_ "net/http/pprof"

	"strconv"

	"io"

	"syscall"

	"fmt"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/ingress"
	"github.com/sky-uk/feed/ingress/api"
	"github.com/sky-uk/feed/ingress/nginx"
	"github.com/sky-uk/feed/k8s"
)

var (
	debug                  bool
	apiServer              string
	caCertFile             string
	tokenFile              string
	ingressPort            int
	ingressAllow           string
	healthPort             int
	nginxBinary            string
	nginxWorkDir           string
	nginxResolver          string
	serviceDomain          string
	nginxWorkerProcesses   int
	nginxWorkerConnections int
	nginxKeepAliveSeconds  int
	nginxStatusPort        int
)

func init() {
	const (
		defaultAPIServer              = "https://kubernetes:443"
		defaultCaCertFile             = "/run/secrets/kubernetes.io/serviceaccount/ca.crt"
		defaultTokenFile              = "/run/secrets/kubernetes.io/serviceaccount/token"
		defaultIngressPort            = 8080
		defaultIngressAllow           = ""
		defaultHealthPort             = 12082
		defaultServiceDomain          = "svc.cluster"
		defaultNginxBinary            = "/usr/sbin/nginx"
		defaultNginxWorkingDir        = "/nginx"
		defaultNginxWorkers           = 1
		defaultNginxWorkerConnections = 1024
		defaultNginxKeepAliveSeconds  = 65
		defaultNginxStatusPort        = 8081
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
	flag.IntVar(&nginxStatusPort, "nginx-status-port", defaultNginxStatusPort,
		"Port for nginx /health and /status pages. Should be used by frontends to determine if nginx is available.")
}

func main() {
	flag.Parse()
	configureLogging()

	lb := createLB()
	client := createK8sClient()
	controller := ingress.New(ingress.Config{
		LoadBalancer:     lb,
		KubernetesClient: client,
		ServiceDomain:    serviceDomain,
	})

	configureHealthPort(controller)
	addSignalHandler(controller)

	err := controller.Start()
	if err != nil {
		log.Error("Error while starting controller: ", err)
		os.Exit(-1)
	}

	select {}
}

func configureLogging() {
	// logging is the main output, so write it all to stdout
	log.SetOutput(os.Stdout)
	if debug {
		log.SetLevel(log.DebugLevel)
	}
}

func createLB() api.LoadBalancer {
	return nginx.NewNginxLB(nginx.Conf{
		BinaryLocation:    nginxBinary,
		IngressPort:       ingressPort,
		WorkingDir:        nginxWorkDir,
		WorkerProcesses:   nginxWorkerProcesses,
		WorkerConnections: nginxWorkerConnections,
		KeepAliveSeconds:  nginxKeepAliveSeconds,
		StatusPort:        nginxStatusPort,
		Resolver:          nginxResolver,
		DefaultAllow:      ingressAllow,
	})
}

func createK8sClient() k8s.Client {
	caCert := readFile(caCertFile)
	token := string(readFile(tokenFile))

	client, err := k8s.New(apiServer, caCert, token)
	if err != nil {
		log.Errorf("Unable to create Kubernetes client: %v", err)
		os.Exit(-1)
	}

	return client
}

func readFile(path string) []byte {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Errorf("Unable to read %s: %v", path, err)
		os.Exit(-1)
	}
	return data
}

func configureHealthPort(controller ingress.Controller) {
	http.HandleFunc("/health", checkHealth(controller))

	go func() {
		log.Error(http.ListenAndServe(":"+strconv.Itoa(healthPort), nil))
		log.Info(controller.Stop())
		os.Exit(-1)
	}()
}

func checkHealth(controller ingress.Controller) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := controller.Health(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, fmt.Sprintf("%v\n", err))
			return
		}

		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok\n")
	}
}

func addSignalHandler(controller ingress.Controller) {
	c := make(chan os.Signal, 1)
	// SIGTERM is used by Kubernetes to gracefully stop pods.
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for sig := range c {
			log.Infof("Signalled %v, shutting down gracefully", sig)
			err := controller.Stop()
			if err != nil {
				log.Error("Error while stopping controller: ", err)
				os.Exit(-1)
			}
			os.Exit(0)
		}
	}()
}
