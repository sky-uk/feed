package alb

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sky-uk/feed/util/metrics"
)

var once sync.Once
var attachedFrontendGauge prometheus.Gauge

func initMetrics() {
	once.Do(func() {
		attachedFrontendGauge = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace:   metrics.PrometheusNamespace,
				Subsystem:   metrics.PrometheusIngressSubsystem,
				Name:        "alb_frontends_attached",
				Help:        "The total number of ALB frontends attached to",
				ConstLabels: metrics.ConstLabels(),
			})
		prometheus.MustRegister(attachedFrontendGauge)
	})
}
