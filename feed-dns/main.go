package main

import (
	"flag"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/dns"
	"github.com/sky-uk/feed/dns/adapter"
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
	internalHostname           string
	externalHostname           string
	cnameTimeToLive            time.Duration
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
		defaultCnameTTL                   = 5 * time.Minute
	)

	flag.BoolVar(&debug, "debug", false,
		"Enable debug logging.")
	flag.StringVar(&kubeconfig, "kubeconfig", "",
		"Path to kubeconfig for connecting to the API server. Leave blank to connect inside a cluster.")
	flag.DurationVar(&resyncPeriod, "resync-period", defaultResyncPeriod,
		"Resync with the API server periodically to handle missed updates.")
	flag.IntVar(&healthPort, "health-port", defaultHealthPort,
		"Port for checking the health of the ingress controller.")
	flag.Var(&albNames, "alb-names",
		"Comma delimited list of ALB names to use for Route53 updates. Should only include a single ALB name per LB scheme.")
	flag.StringVar(&elbRegion, "elb-region", defaultElbRegion,
		"AWS region for ELBs.")
	flag.StringVar(&elbLabelValue, "elb-label-value", defaultElbLabelValue,
		"Alias to ELBs tagged with "+elb.FrontendTag+"=value. Route53 entries will be created to these,"+
			"depending on the scheme.")
	flag.StringVar(&r53HostedZone, "r53-hosted-zone", defaultHostedZone,
		"Route53 hosted zone id to manage.")
	flag.StringVar(&pushgatewayURL, "pushgateway", "",
		"Prometheus Pushgateway URL for pushing metrics. Leave blank to not push metrics.")
	flag.IntVar(&pushgatewayIntervalSeconds, "pushgateway-interval", defaultPushgatewayIntervalSeconds,
		"Interval in seconds for pushing metrics.")
	flag.Var(&pushgatewayLabels, "pushgateway-label",
		"A label=value pair to attach to metrics pushed to prometheus. Specify multiple times for multiple labels.")
	flag.IntVar(&awsAPIRetries, "aws-api-retries", defaultAwsAPIRetries,
		"Number of times a request to the AWS API is retried.")
	flag.StringVar(&internalHostname, "internal-hostname", "",
		"Hostname of the internal facing load-balancer. If specified, external-hostname must also be given.")
	flag.StringVar(&externalHostname, "external-hostname", "",
		"Hostname of the internet facing load-balancer. If specified, internal-hostname must also be given.")
	flag.DurationVar(&cnameTimeToLive, "cname-ttl", defaultCnameTTL,
		"Time-to-live of CNAME records")
}

func main() {
	flag.Parse()
	validateConfig()

	cmd.ConfigureLogging(debug)
	cmd.ConfigureMetrics("feed-dns", pushgatewayLabels, pushgatewayURL, pushgatewayIntervalSeconds)

	stopCh := make(chan struct{})
	client, err := k8s.New(kubeconfig, resyncPeriod, stopCh)
	if err != nil {
		log.Fatal("Unable to create k8s client: ", err)
	}

	var lbAdapter, lbErr = createFrontendAdapter()
	if lbErr != nil {
		log.Fatal("Error during initialisation: ", lbErr)
	}
	dnsUpdater := dns.New(r53HostedZone, lbAdapter, awsAPIRetries)

	feedController := controller.New(controller.Config{
		KubernetesClient: client,
		Updaters:         []controller.Updater{dnsUpdater},
	}, stopCh)

	cmd.AddHealthMetrics(feedController, metrics.PrometheusDNSSubsystem)
	cmd.AddHealthPort(feedController, healthPort)
	cmd.AddSignalHandler(feedController)

	if err := feedController.Start(); err != nil {
		log.Fatal("Error while starting controller: ", err)
	}

	select {}
}

func createFrontendAdapter() (adapter.FrontendAdapter, error) {
	if internalHostname != "" || externalHostname != "" {
		addressesWithScheme := make(map[string]string)
		if internalHostname != "" {
			addressesWithScheme["internal"] = internalHostname
		}

		if externalHostname != "" {
			addressesWithScheme["internet-facing"] = externalHostname
		}

		return adapter.NewStaticHostnameAdapter(addressesWithScheme, cnameTimeToLive), nil
	}

	config := adapter.AWSAdapterConfig{
		Region:        elbRegion,
		HostedZoneID:  r53HostedZone,
		ELBLabelValue: elbLabelValue,
		ALBNames:      albNames,
	}
	return adapter.NewAWSAdapter(&config)
}

func validateConfig() {
	if r53HostedZone == "" {
		log.Error("Must supply r53-hosted-zone")
		os.Exit(-1)
	}

	if elbLabelValue == "" && len(albNames) == 0 && internalHostname == "" && externalHostname == "" {
		log.Error("Must specify at least one of alb-names, elb-label-value, internal-hostname or external-hostname")
		os.Exit(-1)
	}

	if (internalHostname != "" || externalHostname != "") && (elbLabelValue != "" || len(albNames) > 0) {
		log.Error("Can't supply both ELB/ALB and non-ALB/ELB hostname. Choose one or the other.")
		os.Exit(-1)
	}
}
