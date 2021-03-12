package cmd

import (
	"strings"

	"github.com/sky-uk/feed/nginx"

	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/k8s"
	"github.com/sky-uk/feed/util/metrics"

	cmdutil "github.com/sky-uk/feed/util/cmd"
)

type appendIngressUpdaters = func(kubernetesClient k8s.Client, updaters []controller.Updater) ([]controller.Updater, error)

func runCmd(appender appendIngressUpdaters) {
	if ingressClassName == defaultIngressClassName {
		log.Fatalf("The argument --%s is required", ingressClassFlag)
	}
	controllerConfig.Name = ingressClassName
	controllerConfig.IncludeClasslessIngresses = includeUnnamedIngresses

	cmdutil.ConfigureLogging(debug)
	cmdutil.ConfigureMetrics("feed-ingress", pushgatewayLabels, pushgatewayURL, pushgatewayIntervalSeconds)

	stopCh := make(chan struct{})
	client, err := k8s.New(kubeconfig, resyncPeriod, stopCh)
	if err != nil {
		log.Fatal("Unable to create k8s client: ", err)
	}
	controllerConfig.KubernetesClient = client

	controllerConfig.Updaters, err = createIngressUpdaters(client, appender)
	if err != nil {
		log.Fatal("Unable to create ingress updaters: ", err)
	}

	controllerConfig.NamespaceSelectors, err = parseNamespaceSelector(namespaceSelectors)
	if err != nil {
		log.Fatalf("invalid format for --%s (%s)", ingressControllerNamespaceSelectorsFlag, namespaceSelectors)
	}
	controllerConfig.MatchAllNamespaceSelectors = matchAllNamespaceSelectors

	feedController := controller.New(controllerConfig, stopCh)

	cmdutil.AddHealthMetrics(feedController, metrics.PrometheusIngressSubsystem)
	cmdutil.AddHealthPort(feedController, healthPort)
	cmdutil.AddSignalHandler(feedController)

	if err = feedController.Start(); err != nil {
		log.Fatal("Error while starting controller: ", err)
	}
	log.Info("Controller started")

	select {}
}

func createIngressUpdaters(kubernetesClient k8s.Client, appender appendIngressUpdaters) ([]controller.Updater, error) {
	nginxConfig.Ports = createPortsConfig(ingressPort, ingressHTTPSPort)

	nginxConfig.HealthPort = ingressHealthPort
	nginxConfig.SSLPath = nginxSSLPath
	nginxConfig.TrustedFrontends = nginxTrustedFrontends
	nginxConfig.LogHeaders = nginxLogHeaders
	nginxConfig.VhostStatsSharedMemory = nginxVhostStatsSharedMemory
	nginxConfig.OpenTracingPlugin = nginxOpenTracingPluginPath
	nginxConfig.OpenTracingConfig = nginxOpenTracingConfigPath
	nginxUpdater := nginx.New(nginxConfig)

	updaters := []controller.Updater{nginxUpdater}
	updaters, err := appender(kubernetesClient, updaters)
	if err != nil {
		return nil, err
	}
	return updaters, nil
}

func createPortsConfig(ingressPort int, ingressHTTPSPort int) []nginx.Port {
	var ports = []nginx.Port{}
	if ingressPort != unset {
		ports = append(ports, nginx.Port{Name: "http", Port: ingressPort})
	}
	if ingressHTTPSPort != unset {
		ports = append(ports, nginx.Port{Name: "https", Port: ingressHTTPSPort})
	}

	if len(ports) == 0 {
		log.Fatal("Error http or https port must be provided,(--ingress-port=XXXX or --ingress-https-port=XXXX) exiting")
	}
	return ports
}

func parseNamespaceSelector(nameValueStringSlice []string) ([]*k8s.NamespaceSelector, error) {
	if len(nameValueStringSlice) == 0 {
		return nil, nil
	}

	var namespaceSelectors []*k8s.NamespaceSelector
	for _, nameValueStr := range nameValueStringSlice {
		nameValue := strings.SplitN(nameValueStr, "=", 2)
		namespaceSelectors = append(namespaceSelectors, &k8s.NamespaceSelector{LabelName: nameValue[0], LabelValue: nameValue[1]})
		if len(nameValue) != 2 {
			log.Errorf("expecting name=value but was (%s)", nameValueStringSlice)
		}
	}

	return namespaceSelectors, nil
}
