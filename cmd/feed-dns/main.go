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
	"github.com/sky-uk/feed/dns"
	"github.com/sky-uk/feed/k8s"
)

var (
	apiServer  string
	caCertFile string
	tokenFile  string
	debug      bool
	healthPort int
)

func init() {
	const (
		defaultAPIServer  = "https://kubernetes:443"
		defaultCaCertFile = "/run/secrets/kubernetes.io/serviceaccount/ca.crt"
		defaultTokenFile  = "/run/secrets/kubernetes.io/serviceaccount/token"
		defaultHealthPort = 12082
	)

	flag.StringVar(&apiServer, "apiserver", defaultAPIServer, "Kubernetes API server URL.")
	flag.StringVar(&caCertFile, "cacertfile", defaultCaCertFile, "File containing kubernetes ca certificate.")
	flag.StringVar(&tokenFile, "tokenfile", defaultTokenFile, "File containing kubernetes client authentication token.")
	flag.BoolVar(&debug, "debug", false, "Enable debug logging.")
	flag.IntVar(&healthPort, "health-port", defaultHealthPort, "Port for checking the health of the ingress controller.")
}

func main() {
	flag.Parse()
	configureLogging()

	controller := dns.New(createK8sClient())

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

func configureHealthPort(controller dns.Controller) {
	http.HandleFunc("/health", checkHealth(controller))

	go func() {
		log.Error(http.ListenAndServe(":"+strconv.Itoa(healthPort), nil))
		log.Info(controller.Stop())
		os.Exit(-1)
	}()
}

func checkHealth(controller dns.Controller) func(w http.ResponseWriter, r *http.Request) {
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

func addSignalHandler(controller dns.Controller) {
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
