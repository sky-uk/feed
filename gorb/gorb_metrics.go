package gorb

import (
	"sync"

	"github.com/sky-uk/feed/util/metrics"

	"github.com/prometheus/client_golang/prometheus"
)

var once sync.Once
var attachedFrontendGauge prometheus.Gauge

func initMetrics() {
	once.Do(func() {
		attachedFrontendGauge = metrics.RegisterNewDefaultGauge(metrics.PrometheusIngressSubsystem,
			"gorb_frontends_attached", "The total number of frontends attached to Gorb")
	})
}
