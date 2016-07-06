package metrics

import "github.com/prometheus/client_golang/prometheus"

const (
	// PrometheusNamespace is the metric namespace for feed binaries.
	PrometheusNamespace = "feed"
	// PrometheusIngressSubsystem is the metric subsystem for feed-ingress.
	PrometheusIngressSubsystem = "ingress"
	// PrometheusDNSSubsystem is the metric subsystem for feed-dns.
	PrometheusDNSSubsystem = "dns"
)

// ConstLabels should be used when creating a prometheus metric as a set of default labels.
// To ensure the correct const labels are used, make sure metrics are not created in init().
var ConstLabels prometheus.Labels
