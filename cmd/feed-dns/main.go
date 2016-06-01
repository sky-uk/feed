package main

import (
	"flag"
	"os"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/dns"
	"github.com/sky-uk/feed/util/cmd"
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
	cmd.ConfigureLogging(debug)

	client := cmd.CreateK8sClient(caCertFile, tokenFile, apiServer)
	controller := dns.New(client)

	cmd.ConfigureHealthPort(controller, healthPort)
	cmd.AddSignalHandler(controller)

	err := controller.Start()
	if err != nil {
		log.Error("Error while starting controller: ", err)
		os.Exit(-1)
	}

	select {}
}
