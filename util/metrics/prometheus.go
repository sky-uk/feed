package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

const (
	// PrometheusNamespace is the metric namespace for feed binaries.
	PrometheusNamespace = "feed"
	// PrometheusIngressSubsystem is the metric subsystem for feed-ingress.
	PrometheusIngressSubsystem = "ingress"
	// PrometheusDNSSubsystem is the metric subsystem for feed-dns.
	PrometheusDNSSubsystem = "dns"
)

var labelsLock sync.Mutex
var constLabels prometheus.Labels

// ConstLabels should be used when creating a prometheus metric, as a set of default labels.
// To ensure the correct const labels are used, make sure metrics are not created in init().
// SetConstLabels should have been called first.
func ConstLabels() prometheus.Labels {
	labelsLock.Lock()
	defer labelsLock.Unlock()

	if constLabels == nil {
		log.Panic("Bug: ConstLabels() hasn't been set yet. Are you initialising metrics too early?")
	}
	return constLabels
}

// SetConstLabels sets default labels for prometheus metrics, that can be retrieved using
// ConstLabels().
func SetConstLabels(l prometheus.Labels) {
	labelsLock.Lock()
	defer labelsLock.Unlock()

	if constLabels != nil {
		log.Panic("Bug: ConstLabels() were already set, tried to set them multiple times.")
	}
	constLabels = l
}

func newGaugeOpts(name string, help string) prometheus.GaugeOpts {
	return prometheus.GaugeOpts{
		Namespace:   PrometheusNamespace,
		Subsystem:   PrometheusDNSSubsystem,
		Name:        name,
		Help:        help,
		ConstLabels: ConstLabels(),
	}
}

func newCounterOpts(name string, help string) prometheus.CounterOpts {
	return prometheus.CounterOpts{
		Namespace:   PrometheusNamespace,
		Subsystem:   PrometheusDNSSubsystem,
		Name:        name,
		Help:        help,
		ConstLabels: ConstLabels(),
	}
}

// NewDefaultGauge creates a named Gauge with default options
func NewDefaultGauge(name string, help string) prometheus.Gauge {
	return prometheus.NewGauge(newGaugeOpts(name, help))
}

// NewDefaultGaugeVec creates a named GaugeVec with default options
func NewDefaultGaugeVec(name string, help string, labelNames []string) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(newGaugeOpts(name, help), labelNames)
}

// NewDefaultCounter creates a named Counter with default options
func NewDefaultCounter(name string, help string) prometheus.Counter {
	return prometheus.NewCounter(newCounterOpts(name, help))
}
