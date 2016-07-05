package nginx

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sky-uk/feed/util/metrics"
)

var connectionGauge, waitingConnectionsGauge, writingConnectionsGauge, readingConnectionsGauge,
	acceptsGauge, handledGauge, requestsGauge prometheus.Gauge

func initMetrics() {
	connectionGauge = prometheus.MustRegisterOrGet(prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace:   metrics.PrometheusNamespace,
			Subsystem:   metrics.PrometheusIngressSubsystem,
			Name:        "nginx_connections",
			Help:        "The active number of connections in use by nginx.",
			ConstLabels: metrics.ConstLabels,
		})).(prometheus.Gauge)

	waitingConnectionsGauge = prometheus.MustRegisterOrGet(prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace:   metrics.PrometheusNamespace,
			Subsystem:   metrics.PrometheusIngressSubsystem,
			Name:        "nginx_connections_waiting",
			Help:        "The number of idle connections.",
			ConstLabels: metrics.ConstLabels,
		})).(prometheus.Gauge)

	writingConnectionsGauge = prometheus.MustRegisterOrGet(prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace:   metrics.PrometheusNamespace,
			Subsystem:   metrics.PrometheusIngressSubsystem,
			Name:        "nginx_connections_writing",
			Help:        "The number of connections writing data.",
			ConstLabels: metrics.ConstLabels,
		})).(prometheus.Gauge)

	readingConnectionsGauge = prometheus.MustRegisterOrGet(prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace:   metrics.PrometheusNamespace,
			Subsystem:   metrics.PrometheusIngressSubsystem,
			Name:        "nginx_connections_reading",
			Help:        "The number of connections reading data.",
			ConstLabels: metrics.ConstLabels,
		})).(prometheus.Gauge)

	acceptsGauge = prometheus.MustRegisterOrGet(prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace:   metrics.PrometheusNamespace,
			Subsystem:   metrics.PrometheusIngressSubsystem,
			Name:        "nginx_accepts",
			Help:        "The number of client connections accepted by nginx.",
			ConstLabels: metrics.ConstLabels,
		})).(prometheus.Gauge)

	handledGauge = prometheus.MustRegisterOrGet(prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: metrics.PrometheusNamespace,
			Subsystem: metrics.PrometheusIngressSubsystem,
			Name:      "nginx_handled",
			Help: "The number of client connections handled by nginx. Can be less than accepts if connection limit " +
				"reached.",
			ConstLabels: metrics.ConstLabels,
		})).(prometheus.Gauge)

	requestsGauge = prometheus.MustRegisterOrGet(prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: metrics.PrometheusNamespace,
			Subsystem: metrics.PrometheusIngressSubsystem,
			Name:      "nginx_requests",
			Help: "The number of client requests served by nginx. Will be larger than handled if using persistent " +
				"connections.",
			ConstLabels: metrics.ConstLabels,
		})).(prometheus.Gauge)
}

type parsedMetrics struct {
	connections        int
	waitingConnections int
	writingConnections int
	readingConnections int
	accepts            int
	handled            int
	requests           int
}

func parseAndSetNginxMetrics(port int, statusPath string) error {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/%s", port, strings.TrimPrefix(statusPath, "/")))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	parsed, err := parseStatusBody(resp.Body)
	if err != nil {
		return err
	}

	connectionGauge.Set(float64(parsed.connections))
	waitingConnectionsGauge.Set(float64(parsed.waitingConnections))
	writingConnectionsGauge.Set(float64(parsed.writingConnections))
	readingConnectionsGauge.Set(float64(parsed.readingConnections))
	acceptsGauge.Set(float64(parsed.accepts))
	handledGauge.Set(float64(parsed.handled))
	requestsGauge.Set(float64(parsed.requests))

	return nil
}

func parseStatusBody(body io.Reader) (parsedMetrics, error) {
	text, err := ioutil.ReadAll(body)
	if err != nil {
		return parsedMetrics{}, err
	}

	fields := strings.Fields(string(text))

	var metrics parsedMetrics
	var lookups = []struct {
		metric *int
		idx    int
	}{
		{&metrics.connections, 2},
		{&metrics.accepts, 7},
		{&metrics.handled, 8},
		{&metrics.requests, 9},
		{&metrics.readingConnections, 11},
		{&metrics.writingConnections, 13},
		{&metrics.waitingConnections, 15},
	}

	for _, lookup := range lookups {
		*lookup.metric, err = strconv.Atoi(fields[lookup.idx])
		if err != nil {
			return parsedMetrics{}, fmt.Errorf("%v[%d] not an int: %v", fields, lookup.idx, err)
		}
	}

	return metrics, nil
}
