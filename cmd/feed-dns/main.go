package main

import (
	"flag"

	"os"

	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/dns"
	"github.com/sky-uk/feed/elb"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/util/cmd"
	"github.com/sky-uk/feed/util/metrics"
)

var (
	debug                      bool
	kubeconfig                 string
	resyncPeriod               time.Duration
	healthPort                 int
	albNames                   cmd.CommaSeparatedValues
	elbLabelValue              string
	elbRegion                  string
	r53HostedZone              string
	pushgatewayURL             string
	pushgatewayIntervalSeconds int
	pushgatewayLabels          cmd.KeyValues
	awsAPIRetries              int
	r53DelAssocOnly            bool
)

func init() {
	const (
		defaultResyncPeriod               = time.Minute * 15
		defaultHealthPort                 = 12082
		defaultElbRegion                  = "eu-west-1"
		defaultElbLabelValue              = ""
		defaultHostedZone                 = ""
		defaultPushgatewayIntervalSeconds = 60
		defaultAwsAPIRetries              = 5
	)

	flag.BoolVar(&debug, "debug", false,
		"Enable debug logging.")
	flag.StringVar(&kubeconfig, "kubeconfig", "",
		"Path to kubeconfig for connecting to the apiserver. Leave blank to connect inside a cluster.")
	flag.DurationVar(&resyncPeriod, "resync-period", defaultResyncPeriod,
		"Resync with the apiserver periodically to handle missed updates.")
	flag.IntVar(&healthPort, "health-port", defaultHealthPort,
		"Port for checking the health of the ingress controller.")
	flag.Var(&albNames, "alb-names",
		"Comma delimited list of ALB names to use for Route53 updates. Should only include a single ALB name per LB scheme.")
	flag.StringVar(&elbRegion, "elb-region", defaultElbRegion,
		"AWS region for ELBs.")
	flag.StringVar(&elbLabelValue, "elb-label-value", defaultElbLabelValue,
		"Alias to ELBs tagged with "+elb.ElbTag+"=value. Route53 entries will be created to these,"+
			"depending on the scheme.")
	flag.StringVar(&r53HostedZone, "r53-hosted-zone", defaultHostedZone,
		"Route53 hosted zone id to manage.")
	flag.StringVar(&pushgatewayURL, "pushgateway", "",
		"Prometheus pushgateway URL for pushing metrics. Leave blank to not push metrics.")
	flag.IntVar(&pushgatewayIntervalSeconds, "pushgateway-interval", defaultPushgatewayIntervalSeconds,
		"Interval in seconds for pushing metrics.")
	flag.Var(&pushgatewayLabels, "pushgateway-label",
		"A label=value pair to attach to metrics pushed to prometheus. Specify multiple times for multiple labels.")
	flag.IntVar(&awsAPIRetries, "aws-api-retries", defaultAwsAPIRetries,
		"Number of times a request to the AWS API is retried.")
	flag.BoolVar(&r53DelAssocOnly, "del-assoc-only", false,
		"Only delete Route53 entries associated with ELBs/ALBs configured with this controller.")
}

func main() {
	flag.Parse()
	validateConfig()

	cmd.ConfigureLogging(debug)
	cmd.ConfigureMetrics("feed-dns", pushgatewayLabels, pushgatewayURL, pushgatewayIntervalSeconds)

	client, err := k8s.New(kubeconfig, resyncPeriod)
	if err != nil {
		log.Fatal("Unable to create k8s client: ", err)
	}

	dnsUpdater := dns.New(r53HostedZone, elbRegion, elbLabelValue, albNames, awsAPIRetries, r53DelAssocOnly)

	controller := controller.New(controller.Config{
		KubernetesClient: client,
		Updaters:         []controller.Updater{dnsUpdater},
	})

	cmd.AddHealthMetrics(controller, metrics.PrometheusDNSSubsystem)
	cmd.AddHealthPort(controller, healthPort)
	cmd.AddSignalHandler(controller)

	if err := controller.Start(); err != nil {
		log.Fatal("Error while starting controller: ", err)
	}

	select {}
}

func validateConfig() {
	if r53HostedZone == "" {
		log.Error("Must supply r53-hosted-zone")
		os.Exit(-1)
	}
}
