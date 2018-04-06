package cmd

import (
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"fmt"

	"time"

	"github.com/onrik/logrus/filename"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/util/metrics"
)

// Pulse represents something alive whose health can be checked.
type Pulse interface {
	// Health returns the current health, nil if healthy.
	Health() error
	// Stop the thing that's alive.
	Stop() error
}

// AddHealthPort is used to expose the health over http.
func AddHealthPort(pulse Pulse, healthPort int) {
	http.HandleFunc("/health", healthHandler(pulse))
	http.Handle("/metrics", prometheus.Handler())
	http.HandleFunc("/alive", okHandler)

	go func() {
		log.Error(http.ListenAndServe(":"+strconv.Itoa(healthPort), nil))
		log.Info(pulse.Stop())
		os.Exit(-1)
	}()
}

func healthHandler(pulse Pulse) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := pulse.Health(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, fmt.Sprintf("%v\n", err))
			return
		}

		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok\n")
	}
}

func okHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "ok\n")
}

const pollInterval = time.Second

// AddUnhealthyLogger adds a periodic poller which reports an unhealthy status.
// The healthCounter is increased by pollInterval if unhealthy.
func addUnhealthyLogger(pulse Pulse, unhealthyCounter prometheus.Counter) {
	go func() {
		healthy := true
		tickCh := time.Tick(pollInterval)
		for range tickCh {
			if err := pulse.Health(); err != nil {
				unhealthyCounter.Add(pollInterval.Seconds())
				if healthy {
					log.Warnf("Unhealthy: %v", err)
					healthy = false
				}
			} else if !healthy {
				log.Info("Health restored")
				healthy = true
			}
		}
	}()
}

// AddSignalHandler allows the  controller to shutdown gracefully by respecting SIGTERM.
func AddSignalHandler(pulse Pulse) {
	c := make(chan os.Signal, 1)
	// SIGTERM is used by Kubernetes to gracefully stop pods.
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for sig := range c {
			log.Infof("Signalled %v, shutting down gracefully", sig)
			err := pulse.Stop()
			if err != nil {
				log.Errorf("Error while stopping: %v", err)
				os.Exit(-1)
			}
			os.Exit(0)
		}
	}()
}

// ConfigureLogging sets logging to Stdout and manages setting debug level
func ConfigureLogging(debug bool) {
	// logging is the main output, so write it all to stdout
	log.SetOutput(os.Stdout)
	if debug {
		log.SetLevel(log.DebugLevel)
	}
	filenameHook := filename.NewHook()
	filenameHook.Field = "source"
	log.AddHook(filenameHook)
}

// ConfigureMetrics sets up metrics pushing and default labels. This must be called before any metrics
// are defined.
func ConfigureMetrics(job string, prometheusLabels KeyValues, pushgatewayURL string, pushgatewayIntervalSeconds int) {
	labels := make(prometheus.Labels)
	for _, l := range prometheusLabels {
		labels[l.key] = l.value
	}
	metrics.SetConstLabels(labels)
	addMetricsPusher(job, pushgatewayURL, time.Second*time.Duration(pushgatewayIntervalSeconds))
}

// AddHealthMetrics adds a global health metric for the given pulse. This should only be called
// a single time per binary.
func AddHealthMetrics(pulse Pulse, prometheusSubsystem string) {
	unhealthyCounter := createUnhealthyCounter(prometheusSubsystem)
	addUnhealthyLogger(pulse, unhealthyCounter)
}

func createUnhealthyCounter(subsystem string) prometheus.Counter {
	unhealthyCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: metrics.PrometheusNamespace,
		Subsystem: subsystem,
		Name:      "unhealthy_time",
		Help: fmt.Sprintf("The number of seconds %s-%s has been unhealthy.",
			metrics.PrometheusNamespace, subsystem),
		ConstLabels: metrics.ConstLabels(),
	})
	prometheus.MustRegister(unhealthyCounter)
	return unhealthyCounter
}

// AddMetricsPusher starts a periodic push of metrics to a prometheus pushgateway.
func addMetricsPusher(job, pushgatewayURL string, interval time.Duration) {
	if pushgatewayURL == "" {
		return
	}

	go func() {
		tick := time.Tick(interval)
		for range tick {
			instance, err := os.Hostname()
			if err != nil {
				log.Warnf("Unable to lookup hostname for metrics: %v", err)
				continue
			}

			if err := prometheus.Push(job, instance, pushgatewayURL); err != nil {
				log.Warnf("Unable to push metrics: %v", err)
			}
		}
	}()
}
