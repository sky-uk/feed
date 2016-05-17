package main

import (
	"os"
	"os/signal"
	//
	"flag"

	"io/ioutil"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/ingress"
	"github.com/sky-uk/feed/k8s"
)

var (
	apiServer  string
	caCertFile string
	tokenFile  string
	debug      bool
)

func init() {
	const (
		defaultAPIServer  = "https://kubernetes:443"
		defaultCaCertFile = "/run/secrets/kubernetes.io/serviceaccount/ca.crt"
		defaultTokenFile  = "/run/secrets/kubernetes.io/serviceaccount/token"
	)

	flag.StringVar(&apiServer, "apiserver", defaultAPIServer, "Kubernetes API server URL")
	flag.StringVar(&caCertFile, "cacertfile", defaultCaCertFile, "file containing kubernetes ca certificate")
	flag.StringVar(&tokenFile, "tokenfile", defaultTokenFile, "file containing kubernetes client authentication token")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
}

func main() {
	flag.Parse()

	configureLogging()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	lb := ingress.NewLB()
	client := createK8sClient()

	controller := ingress.New(lb, client)

	go func() {
		for sig := range c {
			log.Infof("Signalled %v, shutting down.", sig)
			controller.Stop()
		}
	}()

	controller.Run()
}

func configureLogging() {
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
