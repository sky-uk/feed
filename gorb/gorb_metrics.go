package gorb

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sky-uk/feed/util/metrics"
)

var once sync.Once
var attachedFrontendGauge prometheus.Gauge

func initMetrics() {
	once.Do(func() {
		attachedFrontendGauge = prometheus.MustRegisterOrGet(prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace:   metrics.PrometheusNamespace,
				Subsystem:   metrics.PrometheusIngressSubsystem,
				Name:        "gorb_frontennds_attached",
				Help:        "The total number of frontends attached to Gorb",
				ConstLabels: metrics.ConstLabels(),
			})).(prometheus.Gauge)
	})
}
