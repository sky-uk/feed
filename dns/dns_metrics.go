package dns

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sky-uk/feed/util"
)

var recordsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
	Namespace: util.PrometheusNamespace,
	Subsystem: util.PrometheusDNSSubsystem,
	Name:      "route53_records",
	Help:      "The current number of records",
})

var updateCount = prometheus.NewCounter(prometheus.CounterOpts{
	Namespace: util.PrometheusNamespace,
	Subsystem: util.PrometheusDNSSubsystem,
	Name:      "route53_updates",
	Help:      "The number of record updates to Route53",
})

var failedCount = prometheus.NewCounter(prometheus.CounterOpts{
	Namespace: util.PrometheusNamespace,
	Subsystem: util.PrometheusDNSSubsystem,
	Name:      "route53_failures",
	Help:      "The number of failed updates to route53",
})

var invalidIngressCount = prometheus.NewCounter(prometheus.CounterOpts{
	Namespace: util.PrometheusNamespace,
	Subsystem: util.PrometheusDNSSubsystem,
	Name:      "invalid_ingress_entries",
	Help:      "The number of invalid ingress entries",
})

func init() {
	prometheus.MustRegister(recordsGauge)
	prometheus.MustRegister(updateCount)
	prometheus.MustRegister(failedCount)
	prometheus.MustRegister(invalidIngressCount)
}
