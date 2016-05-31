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

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/ingress"
	"github.com/sky-uk/feed/ingress/api"
	"github.com/sky-uk/feed/ingress/nginx"
	"github.com/sky-uk/feed/k8s"
)

var (
	nginxConfDir         string
	ingressPort          int
	nginxWorkerProcesses int
	apiServer            string
	caCertFile           string
	tokenFile            string
	debug                bool
	healthPort           int
)

func init() {
	const (
		defaultAPIServer       = "https://kubernetes:443"
		defaultCaCertFile      = "/run/secrets/kubernetes.io/serviceaccount/ca.crt"
		defaultTokenFile       = "/run/secrets/kubernetes.io/serviceaccount/token"
		defaultNginxWorkingDir = "/nginx"
		defaultIngressPort     = 80
		defaultNginxWorkers    = 1
		defaultHealthPort      = 12082
	)

	flag.StringVar(&apiServer, "apiserver", defaultAPIServer, "Kubernetes API server URL.")
	flag.StringVar(&caCertFile, "cacertfile", defaultCaCertFile, "File containing kubernetes ca certificate.")
	flag.StringVar(&tokenFile, "tokenfile", defaultTokenFile, "File containing kubernetes client authentication token.")
	flag.StringVar(&nginxConfDir, "nginx-workdir", defaultNginxWorkingDir, "Directory to store nginx files. Also the location of the nginx.tmpl file.")
	flag.IntVar(&ingressPort, "ingress-port", defaultIngressPort, "Port to server ingress traffic.")
	flag.IntVar(&nginxWorkerProcesses, "nginx-workers", defaultNginxWorkers, "Number of nginx worker processes.")
	flag.BoolVar(&debug, "debug", false, "Enable debug logging.")
	flag.IntVar(&healthPort, "health-port", defaultHealthPort, "Port for checking the health of the ingress controller.")
}

func main() {
	flag.Parse()
	configureLogging()

	lb := createLB()
	client := createK8sClient()
	controller := ingress.New(lb, client)

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
		BinaryLocation:  "/usr/sbin/nginx",
		WorkingDir:      nginxConfDir,
		WorkerProcesses: nginxWorkerProcesses,
		Port:            ingressPort,
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
		if !controller.Healthy() {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, "fail\n")
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
