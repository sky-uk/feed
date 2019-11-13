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
		recordsGauge = metrics.RegisterNewDefaultGauge(metrics.PrometheusDNSSubsystem,
			"route53_records", "The current number of records.")
		updateCount = metrics.RegisterNewDefaultCounter(metrics.PrometheusDNSSubsystem,
			"route53_updates", "The number of record updates to Route53.")
		failedCount = metrics.RegisterNewDefaultCounter(metrics.PrometheusDNSSubsystem,
			"route53_failures", "The number of failed updates to route53.")
		skippedCount = metrics.RegisterNewDefaultCounter(metrics.PrometheusDNSSubsystem,
			"skipped_ingress_entries",
			"The number of ingress entries skipped by feed-dns, such as being outside of the Route53 hosted zone.")
	})
}
