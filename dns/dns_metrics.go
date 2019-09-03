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
		recordsGauge = metrics.NewDefaultGauge("route53_records",
			"The current number of records.")
		prometheus.MustRegister(recordsGauge)

		updateCount = metrics.NewDefaultCounter("route53_updates",
			"The number of record updates to Route53.")
		prometheus.MustRegister(updateCount)

		failedCount = metrics.NewDefaultCounter("route53_failures",
			"The number of failed updates to route53.")
		prometheus.MustRegister(failedCount)

		skippedCount = metrics.NewDefaultCounter("skipped_ingress_entries",
			"The number of ingress entries skipped by feed-dns, such as being outside of the Route53 hosted zone.")
		prometheus.MustRegister(skippedCount)
	})
}
