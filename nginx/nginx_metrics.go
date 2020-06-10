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
var totalAccepts, totalHandled, totalRequests prometheus.Gauge
var ingressRequests, endpointRequests, ingressBytes, endpointBytes *prometheus.GaugeVec
var reloads prometheus.Counter
var ingressRequestsLabelNames = []string{"host", "path", "code"}
var endpointRequestsLabelNames = []string{"name", "endpoint", "code"}
var ingressBytesLabelNames = []string{"host", "path", "direction"}
var endpointBytesLabelNames = []string{"name", "endpoint", "direction"}

func initMetrics() {
	once.Do(func() {
		connections = metrics.RegisterNewDefaultGauge(metrics.PrometheusIngressSubsystem, "nginx_connections",
			"The active number of connections in use by NGINX.")
		waitingConnections = metrics.RegisterNewDefaultGauge(metrics.PrometheusIngressSubsystem, "nginx_connections_waiting",
			"The number of idle connections.")
		writingConnections = metrics.RegisterNewDefaultGauge(metrics.PrometheusIngressSubsystem, "nginx_connections_writing",
			"The number of connections writing data.")
		readingConnections = metrics.RegisterNewDefaultGauge(metrics.PrometheusIngressSubsystem, "nginx_connections_reading",
			"The number of connections reading data.")
		totalAccepts = metrics.RegisterNewDefaultGauge(metrics.PrometheusIngressSubsystem, "nginx_accepts",
			"The number of client connections accepted by NGINX. "+
				"For implementation reasons, this counter is a gauge.")
		totalHandled = metrics.RegisterNewDefaultGauge(metrics.PrometheusIngressSubsystem, "nginx_handled",
			"The number of client connections handled by NGINX. "+
				"Can be less than accepts if connection limit reached. "+
				"For implementation reasons, this counter is a gauge.")
		totalRequests = metrics.RegisterNewDefaultGauge(metrics.PrometheusIngressSubsystem, "nginx_requests",
			"The number of client requests served by NGINX. "+
				"Will be larger than handled if using persistent connections. "+
				"For implementation reasons, this counter is a gauge.")
		ingressRequests = metrics.RegisterNewDefaultGaugeVec(metrics.PrometheusIngressSubsystem, "ingress_requests",
			"The number of requests proxied by NGINX per ingress. "+
				"For implementation reasons, this counter is a gauge.",
			ingressRequestsLabelNames)
		endpointRequests = metrics.RegisterNewDefaultGaugeVec(metrics.PrometheusIngressSubsystem, "endpoint_requests",
			"The number of requests proxied by NGINX per endpoint. "+
				"For implementation reasons, this counter is a gauge.",
			endpointRequestsLabelNames)
		ingressBytes = metrics.RegisterNewDefaultGaugeVec(metrics.PrometheusIngressSubsystem, "ingress_bytes",
			"The number of bytes sent or received by a client to this ingress. "+
				"Direction is 'in' for bytes received from a client, 'out' for bytes sent to a client. "+
				"For implementation reasons, this counter is a gauge.",
			ingressBytesLabelNames)
		endpointBytes = metrics.RegisterNewDefaultGaugeVec(metrics.PrometheusIngressSubsystem, "endpoint_bytes",
			"The number of bytes sent or received by this endpoint. "+
				"Direction is 'in' for bytes received from the endpoint, 'out' for bytes sent to the endpoint. "+
				"For implementation reasons, this counter is a gauge.",
			endpointBytesLabelNames)
		reloads = metrics.RegisterNewDefaultCounter(metrics.PrometheusIngressSubsystem, "reloads",
			"Count of Nginx configuration reloads")
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

// VTSMetrics represents the json returned by the VTS NGINX plugin.
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
	vtsMetrics, err := parseStatusBody(resp.Body)
	if err != nil {
		return err
	}

	updateNginxMetrics(vtsMetrics)
	updateIngressMetrics(vtsMetrics)
	updateEndpointMetrics(vtsMetrics)

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
			if len(strs) != 2 || strs[1] == "" {
				log.Warnf("filter name not formatted as expected for %s, got %s without a valid '::' split", host, zone)
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
	var vtsMetrics VTSMetrics
	if err := dec.Decode(&vtsMetrics); err != nil {
		return VTSMetrics{}, err
	}
	return vtsMetrics, nil
}

func incrementReloadMetric() {
	reloads.Inc()
}
