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
	if ingressControllerName == defaultIngressControllerName {
		log.Fatalf("The argument %s is required", ingressControllerNameFlag)
	}
	controllerConfig.Name = ingressControllerName

	cmdutil.ConfigureLogging(debug)
	cmdutil.ConfigureMetrics("feed-ingress", pushgatewayLabels, pushgatewayURL, pushgatewayIntervalSeconds)

	client, err := k8s.New(kubeconfig, resyncPeriod)
	if err != nil {
		log.Fatal("Unable to create k8s client: ", err)
	}
	controllerConfig.KubernetesClient = client

	controllerConfig.Updaters, err = createIngressUpdaters(client, appender)
	if err != nil {
		log.Fatal("Unable to create ingress updaters: ", err)
	}

	// If the legacy setting is set, use it instead to preserve backwards compatibility.
	if legacyBackendKeepaliveSeconds != unset {
		controllerConfig.DefaultBackendTimeoutSeconds = legacyBackendKeepaliveSeconds
	}

	controllerConfig.NamespaceSelector, err = parseNamespaceSelector(namespaceSelector)
	if err != nil {
		log.Fatalf("invalid format for -%s (%s)", ingressControllerNamespaceSelectorFlag, namespaceSelector)
	}

	feedController := controller.New(controllerConfig)

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
	nginxConfig.Ports = []nginx.Port{{Name: "http", Port: ingressPort}}
	if ingressHTTPSPort != unset {
		nginxConfig.Ports = append(nginxConfig.Ports, nginx.Port{Name: "https", Port: ingressHTTPSPort})
	}

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

func parseNamespaceSelector(nameValueStr string) (*k8s.NamespaceSelector, error) {
	if len(nameValueStr) == 0 {
		return nil, nil
	}

	nameValue := strings.SplitN(nameValueStr, "=", 2)
	if len(nameValue) != 2 {
		log.Errorf("expecting name=value but was (%s)", nameValueStr)
	}
	return &k8s.NamespaceSelector{LabelName: nameValue[0], LabelValue: nameValue[1]}, nil
}
