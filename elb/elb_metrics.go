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
		attachedFrontendGauge = metrics.NewDefaultGauge("frontends_attached",
			"The total number of frontends attached")
		prometheus.MustRegister(attachedFrontendGauge)
	})
}
