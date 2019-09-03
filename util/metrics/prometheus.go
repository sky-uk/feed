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

func gaugeOpts(name string, help string) prometheus.GaugeOpts {
	return prometheus.GaugeOpts{
		Namespace:   PrometheusNamespace,
		Subsystem:   PrometheusDNSSubsystem,
		Name:        name,
		Help:        help,
		ConstLabels: ConstLabels(),
	}
}

func counterOpts(name string, help string) prometheus.CounterOpts {
	return prometheus.CounterOpts{
		Namespace:   PrometheusNamespace,
		Subsystem:   PrometheusDNSSubsystem,
		Name:        name,
		Help:        help,
		ConstLabels: ConstLabels(),
	}
}

func register(collector prometheus.Collector, name string) prometheus.Collector {
	err := prometheus.Register(collector)
	if err != nil {
		log.Fatalf("Could not register collector %s: %v", name, err)
	}
	return collector
}

// RegisterNewDefaultGauge creates and registers a named Gauge with default options
func RegisterNewDefaultGauge(name string, help string) prometheus.Gauge {
	return register(prometheus.NewGauge(gaugeOpts(name, help)), name).(prometheus.Gauge)
}

// RegisterNewDefaultGaugeVec creates and registers a named GaugeVec with default options
func RegisterNewDefaultGaugeVec(name string, help string, labelNames []string) *prometheus.GaugeVec {
	return register(prometheus.NewGaugeVec(gaugeOpts(name, help), labelNames), name).(*prometheus.GaugeVec)
}

// RegisterNewDefaultCounter creates and registers a named Counter with default options
func RegisterNewDefaultCounter(name string, help string) prometheus.Counter {
	return register(prometheus.NewCounter(counterOpts(name, help)), name).(prometheus.Counter)
}
