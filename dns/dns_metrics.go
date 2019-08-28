package dns

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sky-uk/feed/util/metrics"
)

var once sync.Once
var recordsGauge prometheus.Gauge
var updateCount, failedCount, skippedCount prometheus.Counter

func initMetrics() {
	once.Do(func() {
		recordsGauge = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace:   metrics.PrometheusNamespace,
				Subsystem:   metrics.PrometheusDNSSubsystem,
				Name:        "route53_records",
				Help:        "The current number of records.",
				ConstLabels: metrics.ConstLabels(),
			})
		prometheus.MustRegister(recordsGauge)

		updateCount = prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace:   metrics.PrometheusNamespace,
				Subsystem:   metrics.PrometheusDNSSubsystem,
				Name:        "route53_updates",
				Help:        "The number of record updates to Route53.",
				ConstLabels: metrics.ConstLabels(),
			})
		prometheus.MustRegister(updateCount)

		failedCount = prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace:   metrics.PrometheusNamespace,
				Subsystem:   metrics.PrometheusDNSSubsystem,
				Name:        "route53_failures",
				Help:        "The number of failed updates to route53.",
				ConstLabels: metrics.ConstLabels(),
			})
		prometheus.MustRegister(failedCount)

		skippedCount = prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: metrics.PrometheusNamespace,
				Subsystem: metrics.PrometheusDNSSubsystem,
				Name:      "skipped_ingress_entries",
				Help: "The number of ingress entries skipped by feed-dns, such as being outside of the" +
					" Route53 hosted zone.",
				ConstLabels: metrics.ConstLabels(),
			})
		prometheus.MustRegister(skippedCount)
	})
}
