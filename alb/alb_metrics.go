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
		attachedFrontendGauge = metrics.RegisterNewDefaultGauge("alb_frontends_attached",
			"The total number of ALB frontends attached to")
	})
}
