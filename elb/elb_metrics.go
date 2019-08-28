package elb

import (
	"sync"

	"github.com/sky-uk/feed/util/metrics"

	"github.com/prometheus/client_golang/prometheus"
)

var once sync.Once
var attachedFrontendGauge prometheus.Gauge

func initMetrics() {
	once.Do(func() {
		attachedFrontendGauge = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace:   metrics.PrometheusNamespace,
				Subsystem:   metrics.PrometheusIngressSubsystem,
				Name:        "frontends_attached",
				Help:        "The total number of frontends attached",
				ConstLabels: metrics.ConstLabels(),
			})
		prometheus.MustRegister(attachedFrontendGauge)
	})
}
