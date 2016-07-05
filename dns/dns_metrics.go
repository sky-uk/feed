package dns

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sky-uk/feed/util/metrics"
)

var recordsGauge prometheus.Gauge
var updateCount, failedCount, invalidIngressCount prometheus.Counter

func initMetrics() {
	recordsGauge = prometheus.MustRegisterOrGet(prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace:   metrics.PrometheusNamespace,
			Subsystem:   metrics.PrometheusDNSSubsystem,
			Name:        "route53_records",
			Help:        "The current number of records",
			ConstLabels: metrics.ConstLabels,
		})).(prometheus.Gauge)

	updateCount = prometheus.MustRegisterOrGet(prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace:   metrics.PrometheusNamespace,
			Subsystem:   metrics.PrometheusDNSSubsystem,
			Name:        "route53_updates",
			Help:        "The number of record updates to Route53",
			ConstLabels: metrics.ConstLabels,
		})).(prometheus.Counter)

	failedCount = prometheus.MustRegisterOrGet(prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace:   metrics.PrometheusNamespace,
			Subsystem:   metrics.PrometheusDNSSubsystem,
			Name:        "route53_failures",
			Help:        "The number of failed updates to route53",
			ConstLabels: metrics.ConstLabels,
		})).(prometheus.Counter)

	invalidIngressCount = prometheus.MustRegisterOrGet(prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace:   metrics.PrometheusNamespace,
			Subsystem:   metrics.PrometheusDNSSubsystem,
			Name:        "invalid_ingress_entries",
			Help:        "The number of invalid ingress entries",
			ConstLabels: metrics.ConstLabels,
		})).(prometheus.Counter)
}
