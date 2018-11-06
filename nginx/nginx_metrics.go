package nginx

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/util/metrics"
)

const statusPath string = "status/format/json"

var once sync.Once
var connections, waitingConnections, writingConnections, readingConnections prometheus.Gauge
var totalAccepts, totalHandled, totalRequests prometheus.Counter
var ingressRequests, endpointRequests, ingressBytes, endpointBytes *prometheus.CounterVec
var ingressRequestsLabelNames = []string{"host", "path", "code"}
var endpointRequestsLabelNames = []string{"name", "endpoint", "code"}
var ingressBytesLabelNames = []string{"host", "path", "direction"}
var endpointBytesLabelNames = []string{"name", "endpoint", "direction"}

func initMetrics() {
	once.Do(func() {
		connections = prometheus.MustRegisterOrGet(prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace:   metrics.PrometheusNamespace,
				Subsystem:   metrics.PrometheusIngressSubsystem,
				Name:        "nginx_connections",
				Help:        "The active number of connections in use by nginx.",
				ConstLabels: metrics.ConstLabels(),
			})).(prometheus.Gauge)

		waitingConnections = prometheus.MustRegisterOrGet(prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace:   metrics.PrometheusNamespace,
				Subsystem:   metrics.PrometheusIngressSubsystem,
				Name:        "nginx_connections_waiting",
				Help:        "The number of idle connections.",
				ConstLabels: metrics.ConstLabels(),
			})).(prometheus.Gauge)

		writingConnections = prometheus.MustRegisterOrGet(prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace:   metrics.PrometheusNamespace,
				Subsystem:   metrics.PrometheusIngressSubsystem,
				Name:        "nginx_connections_writing",
				Help:        "The number of connections writing data.",
				ConstLabels: metrics.ConstLabels(),
			})).(prometheus.Gauge)

		readingConnections = prometheus.MustRegisterOrGet(prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace:   metrics.PrometheusNamespace,
				Subsystem:   metrics.PrometheusIngressSubsystem,
				Name:        "nginx_connections_reading",
				Help:        "The number of connections reading data.",
				ConstLabels: metrics.ConstLabels(),
			})).(prometheus.Gauge)

		totalAccepts = prometheus.MustRegisterOrGet(prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace:   metrics.PrometheusNamespace,
				Subsystem:   metrics.PrometheusIngressSubsystem,
				Name:        "nginx_accepts",
				Help:        "The number of client connections accepted by nginx.",
				ConstLabels: metrics.ConstLabels(),
			})).(prometheus.Gauge)

		totalHandled = prometheus.MustRegisterOrGet(prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: metrics.PrometheusNamespace,
				Subsystem: metrics.PrometheusIngressSubsystem,
				Name:      "nginx_handled",
				Help: "The number of client connections handled by nginx. Can be less than accepts if connection limit " +
					"reached.",
				ConstLabels: metrics.ConstLabels(),
			})).(prometheus.Gauge)

		totalRequests = prometheus.MustRegisterOrGet(prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: metrics.PrometheusNamespace,
				Subsystem: metrics.PrometheusIngressSubsystem,
				Name:      "nginx_requests",
				Help: "The number of client requests served by nginx. Will be larger than handled if using persistent " +
					"connections.",
				ConstLabels: metrics.ConstLabels(),
			})).(prometheus.Gauge)

		ingressRequests = prometheus.MustRegisterOrGet(prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   metrics.PrometheusNamespace,
				Subsystem:   metrics.PrometheusIngressSubsystem,
				Name:        "ingress_requests",
				Help:        "The number of requests proxied by nginx per ingress.",
				ConstLabels: metrics.ConstLabels(),
			},
			ingressRequestsLabelNames,
		)).(*prometheus.CounterVec)

		endpointRequests = prometheus.MustRegisterOrGet(prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   metrics.PrometheusNamespace,
				Subsystem:   metrics.PrometheusIngressSubsystem,
				Name:        "endpoint_requests",
				Help:        "The number of requests proxied by nginx per endpoint.",
				ConstLabels: metrics.ConstLabels(),
			},
			endpointRequestsLabelNames,
		)).(*prometheus.CounterVec)

		ingressBytes = prometheus.MustRegisterOrGet(prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: metrics.PrometheusNamespace,
				Subsystem: metrics.PrometheusIngressSubsystem,
				Name:      "ingress_bytes",
				Help: "The number of bytes sent or received by a client to this ingress. Direction is " +
					"'in' for bytes received from a client, 'out' for bytes sent to a client.",
				ConstLabels: metrics.ConstLabels(),
			},
			ingressBytesLabelNames,
		)).(*prometheus.CounterVec)

		endpointBytes = prometheus.MustRegisterOrGet(prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: metrics.PrometheusNamespace,
				Subsystem: metrics.PrometheusIngressSubsystem,
				Name:      "endpoint_bytes",
				Help: "The number of bytes sent or received by this endpoint. Direction is " +
					"'in' for bytes received from the endpoint, 'out' for bytes sent to the endpoint.",
				ConstLabels: metrics.ConstLabels(),
			},
			endpointBytesLabelNames,
		)).(*prometheus.CounterVec)
	})
}

// VTSConnections represents json of the connections.
type VTSConnections struct {
	Active   float64 `json:"active"`
	Reading  float64 `json:"reading"`
	Writing  float64 `json:"writing"`
	Waiting  float64 `json:"waiting"`
	Accepted float64 `json:"accepted"`
	Handled  float64 `json:"handled"`
	Requests float64 `json:"requests"`
}

// VTSRequestData contains request details.
type VTSRequestData struct {
	Server    string        `json:"server"`
	InBytes   float64       `json:"inBytes"`
	OutBytes  float64       `json:"outBytes"`
	Responses *VTSResponses `json:"responses"`
}

// VTSResponses contains response details.
type VTSResponses struct {
	OneXX   float64 `json:"1xx"`
	TwoXX   float64 `json:"2xx"`
	ThreeXX float64 `json:"3xx"`
	FourXX  float64 `json:"4xx"`
	FiveXX  float64 `json:"5xx"`
}

// VTSMetrics represents the json returned by the vts nginx plugin.
type VTSMetrics struct {
	Connections   *VTSConnections                      `json:"connections"`
	FilterZones   map[string]map[string]VTSRequestData `json:"filterZones"`
	UpstreamZones map[string][]VTSRequestData          `json:"upstreamZones"`
}

func parseAndSetNginxMetrics(statusPort int) error {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/%s", statusPort, statusPath))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	metrics, err := parseStatusBody(resp.Body)
	if err != nil {
		return err
	}

	updateNginxMetrics(metrics)
	updateIngressMetrics(metrics)
	updateEndpointMetrics(metrics)

	return nil
}

func updateNginxMetrics(metrics VTSMetrics) {
	connections.Set(metrics.Connections.Active)
	waitingConnections.Set(metrics.Connections.Waiting)
	writingConnections.Set(metrics.Connections.Writing)
	readingConnections.Set(metrics.Connections.Reading)
	totalAccepts.Set(metrics.Connections.Accepted)
	totalHandled.Set(metrics.Connections.Handled)
	totalRequests.Set(metrics.Connections.Requests)
}

func updateIngressMetrics(metrics VTSMetrics) {
	for host, zoneDetails := range metrics.FilterZones {
		for zone, requestData := range zoneDetails {
			responses := requestData.Responses
			if responses == nil {
				log.Warnf("response was nil when trying to parse filter zones for %s", host)
				continue
			}

			strs := strings.Split(zone, "::")
			if len(strs) != 2 {
				log.Warnf("filter name not formatted as expected for %s, got %s without a '::' split", host, zone)
				continue
			}
			path := strs[0]

			ingressBytes.WithLabelValues(host, path, "in").Set(requestData.InBytes)
			ingressBytes.WithLabelValues(host, path, "out").Set(requestData.OutBytes)
			ingressRequests.WithLabelValues(host, path, "1xx").Set(responses.OneXX)
			ingressRequests.WithLabelValues(host, path, "2xx").Set(responses.TwoXX)
			ingressRequests.WithLabelValues(host, path, "3xx").Set(responses.ThreeXX)
			ingressRequests.WithLabelValues(host, path, "4xx").Set(responses.FourXX)
			ingressRequests.WithLabelValues(host, path, "5xx").Set(responses.FiveXX)
		}
	}
}

func updateEndpointMetrics(metrics VTSMetrics) {
	for name, zones := range metrics.UpstreamZones {
		for _, zone := range zones {
			responses := zone.Responses
			if responses == nil || zone.Server == "" {
				log.Warnf("response or server was nil when trying to parse upstream zone for %s", name)
				continue
			}

			endpointBytes.WithLabelValues(name, zone.Server, "in").Set(zone.InBytes)
			endpointBytes.WithLabelValues(name, zone.Server, "out").Set(zone.OutBytes)
			endpointRequests.WithLabelValues(name, zone.Server, "1xx").Set(responses.OneXX)
			endpointRequests.WithLabelValues(name, zone.Server, "2xx").Set(responses.TwoXX)
			endpointRequests.WithLabelValues(name, zone.Server, "3xx").Set(responses.ThreeXX)
			endpointRequests.WithLabelValues(name, zone.Server, "4xx").Set(responses.FourXX)
			endpointRequests.WithLabelValues(name, zone.Server, "5xx").Set(responses.FiveXX)
		}
	}
}

func parseStatusBody(body io.Reader) (VTSMetrics, error) {
	dec := json.NewDecoder(body)
	var metrics VTSMetrics
	if err := dec.Decode(&metrics); err != nil {
		return VTSMetrics{}, err
	}
	return metrics, nil
}
