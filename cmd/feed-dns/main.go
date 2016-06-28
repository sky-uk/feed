package main

import (
	"flag"

	"os"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/dns"
	"github.com/sky-uk/feed/elb"
	"github.com/sky-uk/feed/util/cmd"
)

var (
	apiServer      string
	caCertFile     string
	tokenFile      string
	clientCertFile string
	clientKeyFile  string
	debug          bool
	healthPort     int
	elbLabelValue  string
	elbRegion      string
	r53HostedZone  string
)

func init() {
	const (
		defaultAPIServer      = "https://kubernetes:443"
		defaultCaCertFile     = "/run/secrets/kubernetes.io/serviceaccount/ca.crt"
		defaultTokenFile      = "/run/secrets/kubernetes.io/serviceaccount/token"
		defaultClientCertFile = ""
		defaultClientKeyFile  = ""
		defaultHealthPort     = 12082
		defaultElbRegion      = "eu-west-1"
		defaultElbLabelValue  = ""
		defaultHostedZone     = ""
	)

	flag.StringVar(&apiServer, "apiserver", defaultAPIServer,
		"Kubernetes API server URL.")
	flag.StringVar(&caCertFile, "cacertfile", defaultCaCertFile,
		"File containing kubernetes ca certificate.")
	flag.StringVar(&tokenFile, "tokenfile", defaultTokenFile,
		"File containing kubernetes client authentication token.")
	flag.StringVar(&clientCertFile, "client-certfile", defaultClientCertFile,
		"File containing client certificate. Leave empty to not use a client certificate.")
	flag.StringVar(&clientKeyFile, "client-keyfile", defaultClientKeyFile,
		"File containing client key. Leave empty to not use a client certificate.")
	flag.BoolVar(&debug, "debug", false,
		"Enable debug logging.")
	flag.IntVar(&healthPort, "health-port", defaultHealthPort,
		"Port for checking the health of the ingress controller.")
	flag.StringVar(&elbRegion, "elb-region", defaultElbRegion,
		"AWS region for ELBs.")
	flag.StringVar(&elbLabelValue, "elb-label-value", defaultElbLabelValue,
		"Alias to ELBs tagged with "+elb.ElbTag+"=value. Leave empty to not attach.")
	flag.StringVar(&r53HostedZone, "r53-hosted-zone", defaultHostedZone,
		"Route53 Hosted zone to manage.")
}

func main() {
	flag.Parse()
	cmd.ConfigureLogging(debug)
	validateConfig()

	client := cmd.CreateK8sClient(caCertFile, tokenFile, apiServer, clientCertFile, clientKeyFile)
	dnsUpdater := dns.New(r53HostedZone, elbRegion, elbLabelValue)

	controller := controller.New(controller.Config{
		KubernetesClient: client,
		Updaters:         []controller.Updater{dnsUpdater},
	})

	cmd.AddHealthPort(dnsUpdater, healthPort)
	cmd.AddSignalHandler(dnsUpdater)

	err := controller.Start()
	if err != nil {
		log.Error("Error while starting controller: ", err)
		os.Exit(-1)
	}

	err = dnsUpdater.Start()
	if err != nil {
		log.Error("Error while starting updater: ", err)
		os.Exit(-1)
	}

	select {}
}

func validateConfig() {
	if r53HostedZone == "" {
		log.Error("Must supply r53-hosted-zone")
		os.Exit(-1)
	}
}
