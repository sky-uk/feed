package nginx

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/util/metrics"
	"github.com/stretchr/testify/assert"
)

func init() {
	metrics.SetConstLabels(make(prometheus.Labels))

	fakeNginxBinary, err := os.Open(fakeNginx)
	if err != nil {
		if errors.IsNotFound(err) {
			panic(fmt.Sprintf("Fake nginx binary %s not found. Run 'make fakenginx' and re-run.", fakeNginx))
		}

		panic(fmt.Sprintf("Error locating fake ngninx binary %s: %v", fakeNginx, err))
	}
	fakeNginxBinary.Close()
}

const (
	port          = 9090
	fakeNginx     = "./fake/fake_graceful_nginx"
	smallWaitTime = time.Millisecond * 20
)

func newConf(tmpDir string, binary string) Conf {
	return Conf{
		WorkingDir:                   tmpDir,
		BinaryLocation:               binary,
		Ports:                        []Port{{Name: "http", Port: port}},
		WorkerProcesses:              1,
		WorkerShutdownTimeoutSeconds: 0,
		BackendKeepalives:            1024,
		BackendConnectTimeoutSeconds: 1,
		ServerNamesHashMaxSize:       -1,
		ServerNamesHashBucketSize:    -1,
		UpdatePeriod:                 time.Second,
		VhostStatsSharedMemory:       1,
		VhostStatsRequestBuckets:     []string{"0.005", "0.01", "0.05", "0.1", "0.5", "1", "10"},
		OpenTracingPlugin:            "",
		OpenTracingConfig:            "",
		HTTPConf:                     HTTPConf{NginxSetRealIPFromHeader: "Some-Header-Name-From-Flag"},
	}
}

func newUpdater(tmpDir string) controller.Updater {
	return newUpdaterWithBinary(tmpDir, fakeNginx)
}

func newUpdaterWithBinary(tmpDir string, binary string) controller.Updater {
	conf := newConf(tmpDir, binary)
	return newNginxWithConf(conf)
}

func newNginxWithConf(conf Conf) controller.Updater {
	lb := New(conf)
	return lb
}

func TestCanStartThenStop(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb := newUpdater(tmpDir)

	assert.NoError(t, lb.Start())
	assert.NoError(t, lb.Update([]controller.IngressEntry{{
		Host: "james.com",
	}}))
	assert.NoError(t, lb.Stop())
}

func TestStopDoesNothingIfNginxIsNotRunning(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb := newUpdater(tmpDir)

	assert.NoError(t, lb.Start())
	assert.NoError(t, lb.Stop())
}

func TestStopWaitsForGracefulShutdownOfNginx(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb := newUpdater(tmpDir)

	assert.NoError(lb.Start())
	assert.NoError(lb.Update([]controller.IngressEntry{{
		Host: "james.com",
	}}))
	assert.NoError(lb.Stop())
	assert.Error(lb.Health(), "should have waited for nginx to gracefully stop")
}

func TestNginxStartedAfterFirstUpdate(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)
	lb := newUpdater(tmpDir)

	lb.Start()
	err := lb.Update([]controller.IngressEntry{{
		Host: "james.com",
	}})
	assert.NoError(t, err)

	assert.True(t, nginxHasStarted(tmpDir))
}

func TestNginxDoesNotStartWithZeroIngresses(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)
	lb := newUpdater(tmpDir)

	lb.Start()

	assert.EqualError(t, lb.Update([]controller.IngressEntry{}), "nginx update has been called with 0 entries")
	assert.False(t, nginxHasStarted(tmpDir))
}

func TestNginxDoesNotReloadWithZeroIngresses(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)
	lb := newUpdater(tmpDir)

	lb.Start()
	err := lb.Update([]controller.IngressEntry{{
		Host: "james.com",
	}})
	assert.NoError(t, err)
	assert.True(t, nginxHasStarted(tmpDir))

	err = lb.Update([]controller.IngressEntry{})
	assert.EqualError(t, err, "nginx update has been called with 0 entries")
	time.Sleep(time.Second * 2)
	assert.False(t, nginxHasReloaded(tmpDir))
}

func TestReloadMetricIsIncremented(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	ts := stubHealthPort()
	defer ts.Close()

	conf := newConf(tmpDir, fakeNginx)
	conf.HealthPort = getPort(ts)
	lb := newNginxWithConf(conf)

	lb.Start()
	err := lb.Update([]controller.IngressEntry{{
		Host: "james.com",
	}})
	assert.NoError(err)
	assert.True(nginxHasStarted(tmpDir))

	err = lb.Update([]controller.IngressEntry{{
		Host: "bob.com",
	}})
	time.Sleep(time.Second * 2)

	assert.True(nginxHasReloaded(tmpDir))
	assert.Equal(float64(1), testutil.ToFloat64(reloads))
}

func nginxHasStarted(tmpDir string) bool {
	return nginxLogEquals(tmpDir, "started!")
}

func nginxHasReloaded(tmpDir string) bool {
	return nginxLogEquals(tmpDir, "reloaded!")
}

func nginxLogEquals(nginxDir string, message string) bool {
	filename := fmt.Sprintf("%s/nginx-log", nginxDir)
	file, _ := os.Open(filename)
	defer file.Close()

	buf := make([]byte, len(message))
	file.Read(buf)
	return string(buf) == message
}

func TestUnhealthyIfHealthPortIsNotUp(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb := newUpdater(tmpDir)

	assert.NoError(lb.Start())
	assert.NoError(lb.Update([]controller.IngressEntry{{
		Host: "james.com",
	}}))

	time.Sleep(smallWaitTime)
	assert.EqualError(lb.Health(), "nginx metrics are failing to update")
}

func TestUnhealthyUntilInitialUpdate(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	ts := stubHealthPort()
	defer ts.Close()
	conf := newConf(tmpDir, fakeNginx)
	conf.HealthPort = getPort(ts)
	lb := newNginxWithConf(conf)

	assert.EqualError(lb.Health(), "nginx is not running")
	assert.NoError(lb.Start())

	time.Sleep(smallWaitTime)
	assert.EqualError(lb.Health(), "nginx is not running")
	assert.NoError(lb.Update([]controller.IngressEntry{{
		Host: "james.com",
	}}))
	assert.NoError(lb.Health(), "should be healthy")

	assert.NoError(lb.Stop())
	assert.EqualError(lb.Health(), "nginx is not running")
}

func TestFailsIfNginxDiesEarly(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb := newUpdaterWithBinary(tmpDir, "./fake_failing_nginx.sh")

	assert.NoError(lb.Start())
	assert.Error(lb.Update([]controller.IngressEntry{{
		Host: "james.com",
	}}))
	assert.EqualError(lb.Health(), "nginx is not running")
}

func TestNginxConfig(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	defaultConf := newConf(tmpDir, fakeNginx)

	trustedFrontends := defaultConf
	trustedFrontends.TrustedFrontends = []string{"10.50.185.0/24", "10.82.0.0/16"}

	customLogLevel := defaultConf
	customLogLevel.LogLevel = "info"

	serverNameHashes := defaultConf
	serverNameHashes.ServerNamesHashMaxSize = 128
	serverNameHashes.ServerNamesHashBucketSize = 58

	proxyProtocol := defaultConf
	proxyProtocol.ProxyProtocol = true

	connectTimeout := defaultConf
	connectTimeout.BackendConnectTimeoutSeconds = 3

	enabledAccessLogConf := defaultConf
	enabledAccessLogConf.AccessLog = true
	enabledAccessLogConf.AccessLogDir = "/nginx-access-log"

	sslEndpointConf := defaultConf
	sslEndpointConf.Ports = []Port{{Name: "https", Port: 443}}

	logHeadersConf := defaultConf
	logHeadersConf.LogHeaders = []string{"Content-Type", "Authorization"}

	opentracingConf := defaultConf
	opentracingConf.OpenTracingPlugin = "/my/plugin.so"
	opentracingConf.OpenTracingConfig = "/etc/my/config.json"

	httpConf := defaultConf
	httpConf.ClientHeaderBufferSize = 16
	httpConf.ClientBodyBufferSize = 16
	httpConf.LargeClientHeaderBufferBlocks = 4
	httpConf.NginxSetRealIPFromHeader = "Some-Header-Name-From-Flag"

	incorrectLargeClientHeaderBufferConf := defaultConf
	incorrectLargeClientHeaderBufferConf.LargeClientHeaderBufferBlocks = 4

	workerShutdowntimeoutConf := defaultConf
	workerShutdowntimeoutConf.WorkerShutdownTimeoutSeconds = 10

	noVhostStatsRequestBucketsConf := defaultConf
	noVhostStatsRequestBucketsConf.VhostStatsRequestBuckets = nil

	websocketProxyConf := defaultConf
	websocketProxyConf.AllowWebsocketUpgrade = true

	var tests = []struct {
		name             string
		conf             Conf
		expectedSettings []string
	}{
		{
			"can set trusted frontends for real_ip",
			trustedFrontends,
			[]string{
				"set_real_ip_from 10.50.185.0/24;",
				"set_real_ip_from 10.82.0.0/16;",
				"real_ip_recursive on;",
			},
		},
		{
			"can exclude trusted frontends for real_ip",
			defaultConf,
			[]string{
				"!set_real_ip_from",
			},
		},
		{
			"can set log level",
			customLogLevel,
			[]string{
				"error_log stderr info;",
			},
		},
		{
			"default log level is warn",
			defaultConf,
			[]string{
				"error_log stderr warn;",
			},
		},
		{
			"server names hashes are set",
			serverNameHashes,
			[]string{
				"server_names_hash_max_size 128;",
				"server_names_hash_bucket_size 58;",
			},
		},
		{
			"server names hashes not set by default",
			defaultConf,
			[]string{
				"!server_names_hash_max_size",
				"!server_names_hash_bucket_size",
			},
		},
		{
			"PROXY protocol sets real_ip",
			proxyProtocol,
			[]string{
				"real_ip_header proxy_protocol;",
			},
		},
		{
			"PROXY protocol disabled uses the header name passed in the flags for real_ip",
			defaultConf,
			[]string{
				"real_ip_header Some-Header-Name-From-Flag;",
			},
		},
		{
			"Proxy connect timeout can be changed",
			connectTimeout,
			[]string{
				"proxy_connect_timeout 3s;",
			},
		},
		{
			"Custom log format is used for access logs",
			defaultConf,
			[]string{
				"log_format upstream_info",
			},
		},
		{
			"Access logs are turned off by default",
			defaultConf,
			[]string{
				"access_log off;",
			},
		},
		{
			"Access logs use custom format when enabled",
			enabledAccessLogConf,
			[]string{
				"access_log /nginx-access-log/access.log upstream_info buffer=32k flush=1m;",
			},
		},
		{
			"Access logs use custom headers when enabled",
			logHeadersConf,
			[]string{
				"\"$request\" $status Content-Type=$http_Content_Type Authorization=$http_Authorization $body_bytes_sent",
			},
		},
		{
			"Ssl Endpoint should be created",
			sslEndpointConf,
			[]string{
				"listen 443 ssl default_server;",
			},
		},
		{
			"Vhost stats module has 1 MiB of shared memory",
			defaultConf,
			[]string{
				"vhost_traffic_status_zone shared:vhost_traffic_status:1m",
			},
		},
		{
			"OpenTracing loads the vendor tracer",
			opentracingConf,
			[]string{
				"opentracing_load_tracer /my/plugin.so /etc/my/config.json;",
			},
		},
		{
			"OpenTracing is enabled",
			opentracingConf,
			[]string{
				"opentracing on;",
			},
		},
		{
			"OpenTracing propagates context",
			opentracingConf,
			[]string{
				"opentracing_propagate_context;",
			},
		},
		{
			"Adds client headers buffer size attribute",
			httpConf,
			[]string{
				"client_header_buffer_size 16k;",
			},
		},
		{
			"Adds client body buffer size attribute",
			httpConf,
			[]string{
				"client_body_buffer_size 16k;",
			},
		},
		{
			"Adds large client header buffer attribute",
			httpConf,
			[]string{
				"large_client_header_buffers 4 16k;",
			},
		},
		{
			"client headers buffer size attribute not present if not passed in",
			defaultConf,
			[]string{
				"!client_header_buffer_size",
			},
		},
		{
			"client body buffer size attribute not present if not passed in",
			defaultConf,
			[]string{
				"!client_body_buffer_size",
			},
		},
		{
			"large client header buffer attribute not present if not passed in",
			defaultConf,
			[]string{
				"!large_client_header_buffers",
			},
		},
		{
			"Adds large client header buffer attribute only if the client buffer size is also set",
			incorrectLargeClientHeaderBufferConf,
			[]string{
				"!large_client_header_buffers",
			},
		},
		{
			"Worker shutdown timeout setting not present if default",
			defaultConf,
			[]string{
				"!worker_shutdown_timeout",
			},
		},
		{
			"Worker shutdown timeout is present if not set to default",
			workerShutdowntimeoutConf,
			[]string{
				"worker_shutdown_timeout 10;",
			},
		},
		{
			"Vhost stats request buckets set if provided",
			defaultConf,
			[]string{"vhost_traffic_status_histogram_buckets 0.005 0.01 0.05 0.1 0.5 1 10;"},
		},
		{
			"Vhost stats request buckets not set if not provided",
			noVhostStatsRequestBucketsConf,
			[]string{
				"!vhost_traffic_status_histogram_buckets",
			},
		},
		{
			"WebSocket proxy headers present if enabled",
			websocketProxyConf,
			[]string{
				"# Support WebSocket upgrade, allow keepalive",
				"# Upgrade logic from http://nginx.org/en/docs/http/websocket.html",
				"map $http_upgrade $connection_upgrade {",
				"    default upgrade;",
				"    ''      '';",
				"}",
				"proxy_http_version 1.1;",
				"proxy_set_header Upgrade $http_upgrade;",
				"proxy_set_header Connection $connection_upgrade;",
			},
		},
	}

	for _, test := range tests {
		fmt.Println(test.name)
		lb := newNginxWithConf(test.conf)

		assert.NoError(lb.Start())
		err := lb.Update([]controller.IngressEntry{{
			Host: "james.com",
		}})
		assert.NoError(err)

		config, err := ioutil.ReadFile(tmpDir + "/nginx.conf")
		assert.NoError(err)
		configContents := string(config)

		for _, expected := range test.expectedSettings {
			if strings.HasPrefix(expected, "!") {
				assert.NotContains(configContents, expected[1:], "%s\nshould not contain setting", test.name)
			} else {
				assert.Contains(configContents, expected, "%sshould contain setting", test.name)
			}
		}
	}
}

func TestNginxIngressEntries(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	defaultConf := newConf(tmpDir, fakeNginx)
	enableProxyProtocolConf := defaultConf
	enableProxyProtocolConf.ProxyProtocol = true

	sslEndpointConf := defaultConf
	sslEndpointConf.Ports = []Port{{Name: "https", Port: 443}}

	var tests = []struct {
		name            string
		config          Conf
		entries         []controller.IngressEntry
		upstreamEntries []string
		serverEntries   []string
	}{
		{
			"Check full ingress entry works",
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:                            "foo.com",
					Namespace:                       "core",
					Name:                            "foo-ingress",
					Path:                            "/path",
					ServiceAddress:                  "service",
					ServicePort:                     8080,
					Allow:                           []string{"10.82.0.0/16"},
					StripPaths:                      true,
					ExactPath:                       false,
					BackendTimeoutSeconds:           1,
					BackendKeepaliveTimeout:         2 * time.Minute,
					BackendMaxConnections:           300,
					BackendMaxRequestsPerConnection: 2000,
				},
				{
					Host:                  "foo.com",
					Namespace:             "core",
					Name:                  "foo-ingress-different-path",
					Path:                  "/same-service-different-path",
					ServiceAddress:        "service",
					ServicePort:           8080,
					Allow:                 []string{"10.82.0.0/16"},
					StripPaths:            true,
					ExactPath:             false,
					BackendTimeoutSeconds: 1,
				},
				{
					Host:                            "foo.com",
					Namespace:                       "core",
					Name:                            "foo-ingress-another",
					Path:                            "/anotherpath",
					ServiceAddress:                  "anotherservice",
					ServicePort:                     6060,
					Allow:                           []string{"10.86.0.0/16"},
					StripPaths:                      false,
					ExactPath:                       false,
					BackendTimeoutSeconds:           10,
					BackendMaxRequestsPerConnection: 100,
					BackendKeepaliveTimeout:         5 * time.Minute,
					BackendMaxConnections:           1024,
				},
			},
			[]string{
				"    upstream core.foo-ingress-another.anotherservice.6060 {\n" +
					"        server anotherservice:6060 max_conns=1024;\n" +
					"        keepalive 1024;\n" +
					"        keepalive_requests 100;\n" +
					"        keepalive_timeout 300s;\n" +
					"    }",
				"    upstream core.foo-ingress-different-path.service.8080 {\n" +
					"        server service:8080 max_conns=0;\n" +
					"        keepalive 1024;\n" +
					"        keepalive_requests 1024;\n" +
					"    }",
				"    upstream core.foo-ingress.service.8080 {\n" +
					"        server service:8080 max_conns=300;\n" +
					"        keepalive 1024;\n" +
					"        keepalive_requests 2000;\n" +
					"        keepalive_timeout 120s;\n" +
					"    }",
			},
			[]string{
				"    server {\n" +
					"        listen 9090;\n" +
					"        server_name foo.com;\n" +
					"\n" +
					"        # disable any limits to avoid HTTP 413 for large uploads\n" +
					"        client_max_body_size 0;\n" +
					"\n" +
					"        location /anotherpath/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.foo-ingress-another.anotherservice.6060;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /anotherpath/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 10s;\n" +
					"            proxy_send_timeout 10s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            allow 10.86.0.0/16;\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"\n" +
					"        location /path/ {\n" +
					"            # Strip location path when proxying.\n" +
					"            # Beware this can cause issues with url encoded characters.\n" +
					"            proxy_pass http://core.foo-ingress.service.8080/;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /path/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 1s;\n" +
					"            proxy_send_timeout 1s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            allow 10.82.0.0/16;\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"\n" +
					"        location /same-service-different-path/ {\n" +
					"            # Strip location path when proxying.\n" +
					"            # Beware this can cause issues with url encoded characters.\n" +
					"            proxy_pass http://core.foo-ingress-different-path.service.8080/;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /same-service-different-path/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 1s;\n" +
					"            proxy_send_timeout 1s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            allow 10.82.0.0/16;\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"        location / {\n" +
					"            return 404;\n" +
					"        }\n" +
					"    }",
			},
		},
		{
			"Check no allows works",
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "foo.com",
					Namespace:      "core",
					Name:           "foo-ingress",
					Path:           "/path",
					ServiceAddress: "service",
					ServicePort:    9090,
					Allow:          []string{},
				},
			},
			nil,
			[]string{
				"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n",
			},
		},
		{
			"Check nil allow works",
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "foo.com",
					Namespace:      "core",
					Name:           "foo-ingress",
					Path:           "/path",
					ServiceAddress: "service",
					ServicePort:    9090,
					Allow:          nil,
				},
			},
			nil,
			[]string{
				"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n",
			},
		},
		{
			"Check servers ordered by hostname",
			defaultConf,
			[]controller.IngressEntry{
				{
					Namespace:      "core",
					Name:           "2-last-ingress",
					Host:           "foo-2.com",
					Path:           "/",
					ServiceAddress: "foo",
					ServicePort:    8080,
					Allow:          []string{"10.82.0.0/16"},
				},
				{
					Namespace:      "core",
					Name:           "0-first-ingress",
					Host:           "foo-0.com",
					Path:           "/",
					ServiceAddress: "foo",
					ServicePort:    8080,
					Allow:          []string{"10.82.0.0/16"},
				},
				{
					Namespace:      "core",
					Name:           "1-next-ingress",
					Host:           "foo-1.com",
					Path:           "/",
					ServiceAddress: "foo",
					ServicePort:    8080,
					Allow:          []string{"10.82.0.0/16"},
				},
			},
			nil,
			[]string{
				"        server_name foo-0.com;",
				"        server_name foo-1.com;",
				"        server_name foo-2.com;",
			},
		},
		{
			"Duplicate host/path entries are ignored, the first one is kept order by Namepace,Name,Host,Path",
			defaultConf,
			[]controller.IngressEntry{
				{
					Namespace:         "core",
					Name:              "2-last-ingress",
					Host:              "foo-0.com",
					Path:              "/",
					ServiceAddress:    "foo",
					ServicePort:       8080,
					Allow:             []string{"10.82.0.0/16"},
					CreationTimestamp: time.Now(),
				},
				{
					Namespace:         "core",
					Name:              "0-first-ingress",
					Host:              "foo-0.com",
					Path:              "/",
					ServiceAddress:    "foo",
					ServicePort:       8080,
					Allow:             []string{"10.82.0.0/16"},
					CreationTimestamp: time.Now().Add(-1 * time.Minute),
				},
				{
					Namespace:         "core",
					Name:              "1-next-ingress",
					Host:              "foo-0.com",
					Path:              "/",
					ServiceAddress:    "foo",
					ServicePort:       8080,
					Allow:             []string{"10.82.0.0/16"},
					CreationTimestamp: time.Now(),
				},
			},
			nil,
			[]string{"# ingress: core/0-first-ingress"},
		},
		{
			"Duplicate host/path entries are ignored, even if path is not specified, the first one is kept order by Namepace,Name,Host,Path",
			defaultConf,
			[]controller.IngressEntry{
				{
					Namespace:         "core",
					Name:              "1-last-ingress",
					Host:              "foo-0.com",
					Path:              "/",
					ServiceAddress:    "foo",
					ServicePort:       8080,
					Allow:             []string{"10.82.0.0/16"},
					CreationTimestamp: time.Now(),
				},
				{
					Namespace:         "core",
					Name:              "0-first-ingress",
					Host:              "foo-0.com",
					Path:              "",
					ServiceAddress:    "foo",
					ServicePort:       8080,
					Allow:             []string{"10.82.0.0/16"},
					CreationTimestamp: time.Now().Add(-1 * time.Minute),
				},
			},
			nil,
			[]string{"# ingress: core/0-first-ingress\n" +
				"    server {\n" +
				"        listen 9090;\n" +
				"        server_name foo-0.com;\n" +
				"\n" +
				"        # disable any limits to avoid HTTP 413 for large uploads\n" +
				"        client_max_body_size 0;\n" +
				"\n" +
				"        location / {\n" +
				"            # Keep original path when proxying.\n" +
				"            proxy_pass http://core.0-first-ingress.foo.8080;\n" +
				"\n" +
				"            # Set display name for vhost stats.\n" +
				"            vhost_traffic_status_filter_by_set_key /::$proxy_host $server_name;\n" +
				"\n" +
				"            # Close proxy connections after backend keepalive time.\n" +
				"            proxy_read_timeout 0s;\n" +
				"            proxy_send_timeout 0s;\n" +
				"            proxy_buffer_size 0k;\n" +
				"            proxy_buffers 0 0k;\n" +
				"\n" +
				"            # Allow localhost for debugging\n" +
				"            allow 127.0.0.1;\n" +
				"\n" +
				"            # Restrict clients\n" +
				"            allow 10.82.0.0/16;\n" +
				"            \n" +
				"            deny all;\n" +
				"        }\n" +
				"    }",
			},
		},
		{
			"Duplicate host/path entries are ignored, the first one is kept order by Service",
			defaultConf,
			[]controller.IngressEntry{
				{
					Namespace:         "core",
					Name:              "ingress",
					Host:              "foo-0.com",
					Path:              "/",
					ServiceAddress:    "service-2",
					ServicePort:       8080,
					Allow:             []string{"10.82.0.0/16"},
					CreationTimestamp: time.Now().Add(-1 * time.Minute),
				},
				{
					Namespace:         "core",
					Name:              "ingress",
					Host:              "foo-0.com",
					Path:              "/",
					ServiceAddress:    "service-1",
					ServicePort:       8080,
					Allow:             []string{"10.82.0.0/16"},
					CreationTimestamp: time.Now(),
				},
			},
			nil,
			[]string{"proxy_pass http://core.ingress.service-1.8080"},
		},
		{
			"Duplicate host/path entries are ignored, the first one is kept order by Port",
			defaultConf,
			[]controller.IngressEntry{
				{
					Namespace:         "core",
					Name:              "ingress",
					Host:              "foo-0.com",
					Path:              "/",
					ServiceAddress:    "service",
					ServicePort:       2,
					Allow:             []string{"10.82.0.0/16"},
					CreationTimestamp: time.Now().Add(-1 * time.Minute),
				},
				{
					Namespace:         "core",
					Name:              "ingress",
					Host:              "foo-0.com",
					Path:              "/",
					ServiceAddress:    "service",
					ServicePort:       1,
					Allow:             []string{"10.82.0.0/16"},
					CreationTimestamp: time.Now(),
				},
			},
			nil,
			[]string{"proxy_pass http://core.ingress.service.1"},
		},
		{
			"Check path slashes are added correctly",
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "foo-0.com",
					Namespace:      "core",
					Name:           "foo-ingress",
					Path:           "",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
				{
					Host:           "foo-1.com",
					Namespace:      "core",
					Name:           "foo-ingress",
					Path:           "/prefix-with-slash/",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
				{
					Host:           "foo-2.com",
					Namespace:      "core",
					Name:           "foo-ingress",
					Path:           "prefix-without-preslash/",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
				{
					Host:           "foo-3.com",
					Namespace:      "core",
					Name:           "foo-ingress",
					Path:           "/prefix-without-postslash",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
				{
					Host:           "foo-4.com",
					Namespace:      "core",
					Name:           "foo-ingress",
					Path:           "prefix-without-anyslash",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
			},
			nil,
			[]string{
				"        location / {\n",
				"        location /prefix-with-slash/ {\n",
				"        location /prefix-without-preslash/ {\n",
				"        location /prefix-without-postslash/ {\n",
				"        location /prefix-without-anyslash/ {\n",
			},
		},
		{
			"Check exact paths include equals",
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "foo-0.com",
					Namespace:      "core",
					Name:           "foo-ingress",
					Path:           "/a/test/path",
					ServiceAddress: "service",
					ServicePort:    9090,
					ExactPath:      true,
				},
			},
			nil,
			[]string{
				"        location = /a/test/path {\n",
			},
		},
		{
			"Check multiple allows work",
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "foo.com",
					Namespace:      "core",
					Name:           "foo-ingress",
					Path:           "",
					ServiceAddress: "service",
					ServicePort:    9090,
					Allow:          []string{"10.82.0.0/16", "10.99.0.0/16"},
				},
			},
			nil,
			[]string{
				"            # Restrict clients\n" +
					"            allow 10.82.0.0/16;\n" +
					"            allow 10.99.0.0/16;\n" +
					"            \n" +
					"            deny all;\n",
			},
		},
		{
			"Only a single upstream per ingress",
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "foo.com",
					Namespace:      "core",
					Name:           "foo-ingress",
					Path:           "/my-path",
					ServiceAddress: "service",
					ServicePort:    9090,
				},

				{
					Host:           "foo.com",
					Namespace:      "core",
					Name:           "foo-ingress",
					Path:           "/my-path2",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
			},

			[]string{
				"    upstream core.foo-ingress.service.9090 {\n" +
					"        server service:9090 max_conns=0;\n" +
					"        keepalive 1024;\n" +
					"        keepalive_requests 1024;\n" +
					"    }",
			},
			nil,
		},
		{
			"Ingress names are ordered in comment to prevent diff generation",
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "foo.com",
					Namespace:      "core",
					Name:           "02foo-ingress",
					Path:           "/my-path",
					ServiceAddress: "service",
					ServicePort:    9090,
				},

				{
					Host:           "foo.com",
					Namespace:      "core",
					Name:           "01foo-ingress",
					Path:           "/my-path2",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
			},
			nil,
			[]string{
				"# ingress: core/01foo-ingress core/02foo-ingress",
			},
		},
		{
			"Disabled path stripping should not put a trailing slash on proxy_pass",
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "foo.com",
					Namespace:      "core",
					Name:           "foo-ingress",
					Path:           "/path",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
			},
			nil,
			[]string{
				"    proxy_pass http://core.foo-ingress.service.9090;\n",
			},
		},
		{
			"PROXY protocol enables proxy_protocol listeners",
			enableProxyProtocolConf,
			[]controller.IngressEntry{
				{
					Host:           "foo.com",
					Namespace:      "core",
					Name:           "foo-ingress",
					Path:           "/path",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
			},
			nil,
			[]string{
				"listen 9090 proxy_protocol;",
			},
		},
		{
			"Locations should be ordered by path",
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:                  "foo.com",
					Namespace:             "core",
					Name:                  "foo-ingress",
					Path:                  "",
					ServiceAddress:        "service",
					ServicePort:           9090,
					BackendTimeoutSeconds: 28,
				},
				{
					Host:                  "foo.com",
					Namespace:             "core",
					Name:                  "foo-ingress",
					Path:                  "/lala",
					ServiceAddress:        "service",
					ServicePort:           9090,
					BackendTimeoutSeconds: 28,
				},
				{
					Host:                  "foo.com",
					Namespace:             "core",
					Name:                  "foo-ingress",
					Path:                  "/01234-hi",
					ServiceAddress:        "service",
					ServicePort:           9090,
					BackendTimeoutSeconds: 28,
				},
			},
			nil,
			[]string{
				"        location / {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.foo-ingress.service.9090;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 28s;\n" +
					"            proxy_send_timeout 28s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"\n" +
					"        location /01234-hi/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.foo-ingress.service.9090;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /01234-hi/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 28s;\n" +
					"            proxy_send_timeout 28s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"\n" +
					"        location /lala/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.foo-ingress.service.9090;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /lala/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 28s;\n" +
					"            proxy_send_timeout 28s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n",
			},
		},
		{
			"SSL endpoint defined",
			sslEndpointConf,
			[]controller.IngressEntry{
				{
					Host:           "endpoint-ssl.com",
					Namespace:      "core",
					Name:           "endpoint-ssl-ingress",
					Path:           "/path",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
			},
			nil,
			[]string{
				"ssl_protocols TLSv1.2;",
			},
		},
		{
			"Proxy and buffer size is configurable",
			sslEndpointConf,
			[]controller.IngressEntry{
				{
					Host:              "default-proxy-buffer.com",
					Namespace:         "core",
					Name:              "some-ingress",
					Path:              "/some-path",
					ServiceAddress:    "service",
					ServicePort:       9090,
					ProxyBufferSize:   8,
					ProxyBufferBlocks: 8,
				},
			},
			nil,
			[]string{
				"        location /some-path/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.some-ingress.service.9090;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /some-path/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 8k;\n" +
					"            proxy_buffers 8 8k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n",
			},
		},
	}

	for _, test := range tests {
		fmt.Printf("\n=== test: %s\n", test.name)

		lb := newNginxWithConf(test.config)

		assert.NoError(lb.Start())
		entries := test.entries
		err := lb.Update(entries)
		assert.NoError(err)

		config, err := ioutil.ReadFile(tmpDir + "/nginx.conf")
		assert.NoError(err)
		configContents := string(config)

		if test.upstreamEntries != nil {
			assertConfigEntries(t, test.name, "upstream", `(?sU)(    upstream.+\n    })`, test.upstreamEntries, configContents)
		}
		if test.serverEntries != nil {
			assertConfigEntries(t, test.name, "server", `(?sU)(# ingress:.+server.+\n    })`, test.serverEntries, configContents)
		}
		assert.Nil(lb.Stop())

		if t.Failed() {
			t.FailNow()
		}
	}
}

func TestNginxRootPathLocations(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	config := newConf(tmpDir, fakeNginx)

	var tests = []struct {
		name          string
		entries       []controller.IngressEntry
		serverEntries []string
	}{
		{
			"Generate the root location returning '404 Not Found` code for the server without root path ingress",
			[]controller.IngressEntry{
				{
					Host:           "no-root-location.com",
					Namespace:      "core",
					Name:           "no-root-location-ingress",
					Path:           "/path",
					ServiceAddress: "service",
					ServicePort:    8080,
				},
				{
					Host:           "no-root-location.com",
					Namespace:      "core",
					Name:           "no-root-location-ingress-another",
					Path:           "/anotherpath",
					ServiceAddress: "anotherservice",
					ServicePort:    6060,
				},
			},
			[]string{
				"    server {\n" +
					"        listen 9090;\n" +
					"        server_name no-root-location.com;\n" +
					"\n" +
					"        # disable any limits to avoid HTTP 413 for large uploads\n" +
					"        client_max_body_size 0;\n" +
					"\n" +
					"        location /anotherpath/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.no-root-location-ingress-another.anotherservice.6060;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /anotherpath/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"\n" +
					"        location /path/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.no-root-location-ingress.service.8080;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /path/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"        location / {\n" +
					"            return 404;\n" +
					"        }\n" +
					"    }",
			},
		}, {
			"Generate the root location according tp the root path ingress for the server with root path ingress",
			[]controller.IngressEntry{
				{
					Host:           "root-location.com",
					Namespace:      "core",
					Name:           "root-location-ingress",
					Path:           "/",
					ServiceAddress: "service",
					ServicePort:    7123,
				},
				{
					Host:           "root-location.com",
					Namespace:      "core",
					Name:           "some-root-location-ingress",
					Path:           "/somepath",
					ServiceAddress: "someservice",
					ServicePort:    7124,
				},
			},
			[]string{
				"    server {\n" +
					"        listen 9090;\n" +
					"        server_name root-location.com;\n" +
					"\n" +
					"        # disable any limits to avoid HTTP 413 for large uploads\n" +
					"        client_max_body_size 0;\n" +
					"\n" +
					"        location / {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.root-location-ingress.service.7123;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"\n" +
					"        location /somepath/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.some-root-location-ingress.someservice.7124;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /somepath/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"    }",
			},
		}, {
			"Generate the root location according to the root path ingress for the server with the exact root path ingress",
			[]controller.IngressEntry{
				{
					Host:           "root-location.com",
					Namespace:      "core",
					Name:           "root-location-ingress",
					Path:           "/",
					ServiceAddress: "service",
					ServicePort:    7123,
					ExactPath:      true,
				},
				{
					Host:           "root-location.com",
					Namespace:      "core",
					Name:           "some-root-location-ingress",
					Path:           "/somepath",
					ServiceAddress: "someservice",
					ServicePort:    7124,
				},
			},
			[]string{
				"    server {\n" +
					"        listen 9090;\n" +
					"        server_name root-location.com;\n" +
					"\n" +
					"        # disable any limits to avoid HTTP 413 for large uploads\n" +
					"        client_max_body_size 0;\n" +
					"\n" +
					"        location = / {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.root-location-ingress.service.7123;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"\n" +
					"        location /somepath/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.some-root-location-ingress.someservice.7124;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /somepath/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"    }",
			},
		},
	}

	for _, test := range tests {
		fmt.Printf("\n=== test: %s\n", test.name)

		lb := newNginxWithConf(config)

		assert.NoError(lb.Start())
		entries := test.entries
		err := lb.Update(entries)
		assert.NoError(err)

		config, err := ioutil.ReadFile(tmpDir + "/nginx.conf")
		assert.NoError(err)
		configContents := string(config)

		if test.serverEntries != nil {
			assertConfigEntries(t, test.name, "server", `(?sU)(# ingress:.+server.+\n    })`, test.serverEntries, configContents)
		}
		assert.Nil(lb.Stop())

		if t.Failed() {
			t.FailNow()
		}
	}
}
func TestNginxRootPathLocationsUpdates(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	ts := stubHealthPort()
	defer ts.Close()

	config := newConf(tmpDir, fakeNginx)
	config.HealthPort = getPort(ts)

	var tests = []struct {
		name                 string
		initialEntries       []controller.IngressEntry
		updatedEntries       []controller.IngressEntry
		initialServerEntries []string
		updatedServerEntries []string
	}{
		{
			"Update the root location according to the root path ingress when the root path ingress is added to the server ingresses",
			[]controller.IngressEntry{
				{
					Host:           "root-path-will-be-added.com",
					Namespace:      "core",
					Name:           "an-ingress",
					Path:           "/path",
					ServiceAddress: "service",
					ServicePort:    8080,
				},
				{
					Host:           "root-path-will-be-added.com",
					Namespace:      "core",
					Name:           "another-ingress",
					Path:           "/anotherpath",
					ServiceAddress: "anotherservice",
					ServicePort:    6060,
				},
			},
			[]controller.IngressEntry{
				{
					Host:           "root-path-will-be-added.com",
					Namespace:      "core",
					Name:           "an-ingress",
					Path:           "/",
					ServiceAddress: "service",
					ServicePort:    8080,
				},
				{
					Host:           "root-path-will-be-added.com",
					Namespace:      "core",
					Name:           "an-ingress",
					Path:           "/path",
					ServiceAddress: "service",
					ServicePort:    8080,
				},
				{
					Host:           "root-path-will-be-added.com",
					Namespace:      "core",
					Name:           "another-ingress",
					Path:           "/anotherpath",
					ServiceAddress: "anotherservice",
					ServicePort:    6060,
				},
			},
			[]string{
				"    server {\n" +
					"        listen 9090;\n" +
					"        server_name root-path-will-be-added.com;\n" +
					"\n" +
					"        # disable any limits to avoid HTTP 413 for large uploads\n" +
					"        client_max_body_size 0;\n" +
					"\n" +
					"        location /anotherpath/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.another-ingress.anotherservice.6060;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /anotherpath/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"\n" +
					"        location /path/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.an-ingress.service.8080;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /path/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"        location / {\n" +
					"            return 404;\n" +
					"        }\n" +
					"    }",
			},
			[]string{
				"    server {\n" +
					"        listen 9090;\n" +
					"        server_name root-path-will-be-added.com;\n" +
					"\n" +
					"        # disable any limits to avoid HTTP 413 for large uploads\n" +
					"        client_max_body_size 0;\n" +
					"\n" +
					"        location / {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.an-ingress.service.8080;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"\n" +
					"        location /anotherpath/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.another-ingress.anotherservice.6060;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /anotherpath/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"\n" +
					"        location /path/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.an-ingress.service.8080;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /path/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"    }",
			},
		},
		{
			"Update the root location to return '404 Not Found` code when the root path ingress is removed from the server ingresses",

			[]controller.IngressEntry{
				{
					Host:           "root-path-will-be-added.com",
					Namespace:      "core",
					Name:           "an-ingress",
					Path:           "/",
					ServiceAddress: "service",
					ServicePort:    8080,
				},
				{
					Host:           "root-path-will-be-added.com",
					Namespace:      "core",
					Name:           "an-ingress",
					Path:           "/path",
					ServiceAddress: "service",
					ServicePort:    8080,
				},
				{
					Host:           "root-path-will-be-added.com",
					Namespace:      "core",
					Name:           "another-ingress",
					Path:           "/anotherpath",
					ServiceAddress: "anotherservice",
					ServicePort:    6060,
				},
			},
			[]controller.IngressEntry{
				{
					Host:           "root-path-will-be-added.com",
					Namespace:      "core",
					Name:           "an-ingress",
					Path:           "/path",
					ServiceAddress: "service",
					ServicePort:    8080,
				},
				{
					Host:           "root-path-will-be-added.com",
					Namespace:      "core",
					Name:           "another-ingress",
					Path:           "/anotherpath",
					ServiceAddress: "anotherservice",
					ServicePort:    6060,
				},
			},
			[]string{
				"    server {\n" +
					"        listen 9090;\n" +
					"        server_name root-path-will-be-added.com;\n" +
					"\n" +
					"        # disable any limits to avoid HTTP 413 for large uploads\n" +
					"        client_max_body_size 0;\n" +
					"\n" +
					"        location / {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.an-ingress.service.8080;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"\n" +
					"        location /anotherpath/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.another-ingress.anotherservice.6060;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /anotherpath/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"\n" +
					"        location /path/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.an-ingress.service.8080;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /path/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"    }",
			},
			[]string{
				"    server {\n" +
					"        listen 9090;\n" +
					"        server_name root-path-will-be-added.com;\n" +
					"\n" +
					"        # disable any limits to avoid HTTP 413 for large uploads\n" +
					"        client_max_body_size 0;\n" +
					"\n" +
					"        location /anotherpath/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.another-ingress.anotherservice.6060;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /anotherpath/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"\n" +
					"        location /path/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.an-ingress.service.8080;\n" +
					"\n" +
					"            # Set display name for vhost stats.\n" +
					"            vhost_traffic_status_filter_by_set_key /path/::$proxy_host $server_name;\n" +
					"\n" +
					"            # Close proxy connections after backend keepalive time.\n" +
					"            proxy_read_timeout 0s;\n" +
					"            proxy_send_timeout 0s;\n" +
					"            proxy_buffer_size 0k;\n" +
					"            proxy_buffers 0 0k;\n" +
					"\n" +
					"            # Allow localhost for debugging\n" +
					"            allow 127.0.0.1;\n" +
					"\n" +
					"            # Restrict clients\n" +
					"            \n" +
					"            deny all;\n" +
					"        }\n" +
					"        location / {\n" +
					"            return 404;\n" +
					"        }\n" +
					"    }",
			},
		},
	}

	for _, test := range tests {
		fmt.Printf("\n=== test: %s\n", test.name)

		lb := newNginxWithConf(config)
		assert.NoError(lb.Start())

		initialEntries := test.initialEntries
		err := lb.Update(initialEntries)
		assert.NoError(err)

		initialConfig, err := ioutil.ReadFile(tmpDir + "/nginx.conf")
		assert.NoError(err)

		updatedEntries := test.updatedEntries
		err = lb.Update(updatedEntries)
		assert.NoError(err)

		updatedConfig, err := ioutil.ReadFile(tmpDir + "/nginx.conf")
		assert.NoError(err)

		assertConfigEntries(t, test.name, "server", `(?sU)(# ingress:.+server.+\n    })`, test.initialServerEntries, string(initialConfig))
		assertConfigEntries(t, test.name, "server", `(?sU)(# ingress:.+server.+\n    })`, test.updatedServerEntries, string(updatedConfig))

		assert.Nil(lb.Stop())

		if t.Failed() {
			t.FailNow()
		}
	}
}

func assertConfigEntries(t *testing.T, testName, entryName, entryRegex string, expectedEntries []string, configContents string) {
	r := regexp.MustCompile(entryRegex)

	actualEntries := r.FindAllStringSubmatch(configContents, -1)
	if len(expectedEntries) != len(actualEntries) {
		assert.FailNow(t, "wrong number of entries", "%s: Expected %d %s entries but found %d: %v\n", testName,
			len(expectedEntries), entryName, len(actualEntries), actualEntries)
	}

	for i := range expectedEntries {
		expected := expectedEntries[i]
		actualSubgroups := actualEntries[i]
		if len(actualSubgroups) != 2 {
			assert.FailNow(t, "wrong number of subgroups", "%s: Expected exactly one subgroup, but %s had %d", testName,
				entryRegex, len(actualSubgroups))
		}
		actual := actualSubgroups[1]

		diff := calculateDiff(actual, expected)
		assert.Contains(t, actual, expected, "%s: %s entry doesn't match, diff(actual, expected):\n%s", testName,
			entryName, diff)
	}
}

func calculateDiff(actual, expected string) string {
	diff, err := diff([]byte(actual), []byte(expected))
	if err != nil {
		panic(err)
	}
	return string(diff)
}

func TestDoesNotUpdateIfConfigurationHasNotChanged(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)
	lb := newUpdaterWithBinary(tmpDir, fakeNginx)

	assert.NoError(lb.Start())

	entries := []controller.IngressEntry{
		{
			Host:           "foo.com",
			Path:           "/path",
			ServiceAddress: "service",
			ServicePort:    9090,
		},
	}

	assert.NoError(lb.Update(entries))

	config1, err := ioutil.ReadFile(tmpDir + "/nginx.conf")
	assert.NoError(err)

	assert.NoError(lb.Update(entries))
	config2, err := ioutil.ReadFile(tmpDir + "/nginx.conf")
	assert.NoError(err)

	assert.NoError(lb.Stop())

	assert.Equal(string(config1), string(config2), "configs should be identical")
	time.Sleep(time.Duration(1) * time.Second)
}

func TestRateLimitedForUpdates(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)
	lb := newUpdaterWithBinary(tmpDir, fakeNginx)

	assert.NoError(lb.Start())

	entries := []controller.IngressEntry{
		{
			Host:           "foo.com",
			Path:           "/path",
			ServiceAddress: "service",
			ServicePort:    9090,
		},
	}

	updatedEntries := []controller.IngressEntry{
		{
			Host:           "foo.com",
			Path:           "/path",
			ServiceAddress: "somethingdifferent",
			ServicePort:    9090,
		},
	}

	// initial one should go through synchronously
	assert.NoError(lb.Update(entries))

	// these two should be merged into one
	assert.NoError(lb.Update(updatedEntries))
	assert.NoError(lb.Update(updatedEntries))
	time.Sleep(1 * time.Second)

	assert.NoError(lb.Stop())
}

func TestUpdatesMetricsFromNginxStatusPage(t *testing.T) {
	// given
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	ts := stubHealthPort()
	defer ts.Close()

	conf := newConf(tmpDir, fakeNginx)
	conf.HealthPort = getPort(ts)
	lb := newNginxWithConf(conf)

	// when
	assert.NoError(lb.Start())
	time.Sleep(time.Millisecond * 50)

	// then
	assert.Equal(2.0, metricValue(connections))
	assert.Equal(0.0, metricValue(readingConnections))
	assert.Equal(1.0, metricValue(writingConnections))
	assert.Equal(1.0, metricValue(waitingConnections))
	assert.Equal(68942.0, metricValue(totalAccepts))
	assert.Equal(68942.0, metricValue(totalHandled))
	assert.Equal(76622.0, metricValue(totalRequests))

	// and
	assertIngressRequestCounters(t,
		"heapster-external.sandbox.cosmic.sky", "/stuff/",
		898.0, 471.0, 6.0, 3.0, 2.0, 1.0, 7.0)
	assertIngressRequestCounters(t,
		"heapster.sandbox.cosmic.sky", "/",
		2012.0, 1099.0, 0.0, 7.0, 0.0, 0.0, 0.0)
	assertEndpointRequestCounters(t,
		"kube-system.10.254.201.199.80", "10.254.201.199:80",
		2910.0, 1570.0, 1.0, 10.0, 9.0, 2.0, 3.0)

	// Assert that hosts with both valid and invalid entries for the same path generate metrics for the correct, valid VTS entry
	assertIngressRequestCounters(t,
		"ingress-with-valid-duplicate-path.sandbox.cosmic.sky", "/path/",
		5000.0, 2000.0, 0.0, 5.0, 0.0, 0.0, 0.0)
	// Assert that invalid paths do not generate metrics, even if the VTS data shows hits (e.g. 3xx's)
	assertIngressRequestCounters(t,
		"ingress-with-invalid-path.sandbox.cosmic.sky", "/bad/",
		0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0)
}

func assertIngressRequestCounters(t *testing.T, host, path string, in, out, ones, twos, threes, fours, fives float64) {
	assert := assert.New(t)

	inBytes, _ := ingressBytes.GetMetricWithLabelValues(host, path, "in")
	assert.Equal("feed_ingress_ingress_bytes", metricName(ingressBytes))
	assert.Equal(in, metricValue(inBytes), "in bytes for %s%s", host, path)
	outBytes, _ := ingressBytes.GetMetricWithLabelValues(host, path, "out")
	assert.Equal(out, metricValue(outBytes), "out bytes for %s%s", host, path)

	req1xx, _ := ingressRequests.GetMetricWithLabelValues(host, path, "1xx")
	assert.Equal("feed_ingress_ingress_requests", metricName(req1xx))
	assert.Equal(ones, metricValue(req1xx), "1xx for %s%s", host, path)
	req2xx, _ := ingressRequests.GetMetricWithLabelValues(host, path, "2xx")
	assert.Equal(twos, metricValue(req2xx), "2xx for %s%s", host, path)
	req3xx, _ := ingressRequests.GetMetricWithLabelValues(host, path, "3xx")
	assert.Equal(threes, metricValue(req3xx), "3xx for %s%s", host, path)
	req4xx, _ := ingressRequests.GetMetricWithLabelValues(host, path, "4xx")
	assert.Equal(fours, metricValue(req4xx), "4xx for %s%s", host, path)
	req5xx, _ := ingressRequests.GetMetricWithLabelValues(host, path, "5xx")
	assert.Equal(fives, metricValue(req5xx), "5xx for %s%s", host, path)
}

func assertEndpointRequestCounters(t *testing.T, name, endpoint string, in, out, ones, twos, threes, fours, fives float64) {
	assert := assert.New(t)

	inBytes, _ := endpointBytes.GetMetricWithLabelValues(name, endpoint, "in")
	assert.Equal("feed_ingress_endpoint_bytes", metricName(inBytes))
	assert.Equal(in, metricValue(inBytes), "in bytes for %s%s", name, endpoint)
	outBytes, _ := endpointBytes.GetMetricWithLabelValues(name, endpoint, "out")
	assert.Equal(out, metricValue(outBytes), "out bytes for %s%s", name, endpoint)

	req1xx, _ := endpointRequests.GetMetricWithLabelValues(name, endpoint, "1xx")
	assert.Equal("feed_ingress_endpoint_requests", metricName(req1xx))
	assert.Equal(ones, metricValue(req1xx), "1xx for %s%s", name, endpoint)
	req2xx, _ := endpointRequests.GetMetricWithLabelValues(name, endpoint, "2xx")
	assert.Equal(twos, metricValue(req2xx), "2xx for %s%s", name, endpoint)
	req3xx, _ := endpointRequests.GetMetricWithLabelValues(name, endpoint, "3xx")
	assert.Equal(threes, metricValue(req3xx), "3xx for %s%s", name, endpoint)
	req4xx, _ := endpointRequests.GetMetricWithLabelValues(name, endpoint, "4xx")
	assert.Equal(fours, metricValue(req4xx), "4xx for %s%s", name, endpoint)
	req5xx, _ := endpointRequests.GetMetricWithLabelValues(name, endpoint, "5xx")
	assert.Equal(fives, metricValue(req5xx), "5xx for %s%s", name, endpoint)
}

func stubHealthPort() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status/format/json" {
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write(statusResponseBody); err != nil {
				fmt.Printf("Unable to write health port stub data: %v\n", err)
			}
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func getPort(ts *httptest.Server) int {
	_, port, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		panic(err)
	}
	intPort, err := strconv.Atoi(port)
	if err != nil {
		panic(err)
	}
	return intPort
}

func metricValue(c prometheus.Collector) float64 {
	metricCh := make(chan prometheus.Metric, 1)
	c.Collect(metricCh)
	metric := <-metricCh
	var metricVal dto.Metric
	metric.Write(&metricVal)
	if metricVal.Gauge != nil {
		return *metricVal.Gauge.Value
	}
	if metricVal.Counter != nil {
		return *metricVal.Counter.Value
	}
	return -1.0
}

func metricName(c prometheus.Collector) string {
	descriptionCh := make(chan *prometheus.Desc, 1)
	c.Describe(descriptionCh)
	desc := <-descriptionCh
	descReflect := reflect.ValueOf(*desc)
	fqNameField := descReflect.FieldByName("fqName")
	return fqNameField.String()
}

func TestFailsToUpdateIfConfigurationIsBroken(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)
	lb := newUpdaterWithBinary(tmpDir, "./fake_nginx_failing_reload.sh")

	assert.NoError(lb.Start())

	entries := []controller.IngressEntry{
		{
			Host:           "foo.com",
			Path:           "/path",
			ServiceAddress: "service",
			ServicePort:    9090,
		},
	}

	err := lb.Update(entries)
	assert.Contains(err.Error(), "Config check failed")
	assert.Contains(err.Error(), "./fake_nginx_failing_reload.sh -t")
}

func setupWorkDir(t *testing.T) string {
	tmpDir, err := ioutil.TempDir(os.TempDir(), "ingress_lb_test")
	assert.NoError(t, err)
	copyNginxTemplate(t, tmpDir)
	return tmpDir
}

func copyNginxTemplate(t *testing.T, tmpDir string) {
	assert.NoError(t, exec.Command("cp", "nginx.tmpl", tmpDir+"/").Run())
}

var statusResponseBody = []byte(`{
  "nginxVersion": "1.10.2",
  "loadMsec": 1486665540251,
  "nowMsec": 1486742278375,
  "connections": {
    "active": 2,
    "reading": 0,
    "writing": 1,
    "waiting": 1,
    "accepted": 68942,
    "handled": 68942,
    "requests": 76622
  },
  "serverZones": {
    "heapster.sandbox.cosmic.sky": {
      "requestCounter": 7,
      "inBytes": 2012,
      "outBytes": 1099,
      "responses": {
        "1xx": 0,
        "2xx": 7,
        "3xx": 0,
        "4xx": 0,
        "5xx": 0,
        "miss": 0,
        "bypass": 0,
        "expired": 0,
        "stale": 0,
        "updating": 0,
        "revalidated": 0,
        "hit": 0,
        "scarce": 0
      },
      "overCounts": {
        "maxIntegerSize": 18446744073709551615,
        "requestCounter": 0,
        "inBytes": 0,
        "outBytes": 0,
        "1xx": 0,
        "2xx": 0,
        "3xx": 0,
        "4xx": 0,
        "5xx": 0,
        "miss": 0,
        "bypass": 0,
        "expired": 0,
        "stale": 0,
        "updating": 0,
        "revalidated": 0,
        "hit": 0,
        "scarce": 0
      }
    },
    "heapster-external.sandbox.cosmic.sky": {
      "requestCounter": 3,
      "inBytes": 898,
      "outBytes": 471,
      "responses": {
        "1xx": 0,
        "2xx": 3,
        "3xx": 0,
        "4xx": 0,
        "5xx": 0,
        "miss": 0,
        "bypass": 0,
        "expired": 0,
        "stale": 0,
        "updating": 0,
        "revalidated": 0,
        "hit": 0,
        "scarce": 0
      },
      "overCounts": {
        "maxIntegerSize": 18446744073709551615,
        "requestCounter": 0,
        "inBytes": 0,
        "outBytes": 0,
        "1xx": 0,
        "2xx": 0,
        "3xx": 0,
        "4xx": 0,
        "5xx": 0,
        "miss": 0,
        "bypass": 0,
        "expired": 0,
        "stale": 0,
        "updating": 0,
        "revalidated": 0,
        "hit": 0,
        "scarce": 0
      }
    },
    "ingress-with-valid-duplicate-path.sandbox.cosmic.sky": {
      "requestCounter": 5,
      "inBytes": 5000,
      "outBytes": 2000,
      "responses": {
        "1xx": 0,
        "2xx": 5,
        "3xx": 0,
        "4xx": 0,
        "5xx": 0,
        "miss": 0,
        "bypass": 0,
        "expired": 0,
        "stale": 0,
        "updating": 0,
        "revalidated": 0,
        "hit": 0,
        "scarce": 0
      },
      "overCounts": {
        "maxIntegerSize": 18446744073709551615,
        "requestCounter": 0,
        "inBytes": 0,
        "outBytes": 0,
        "1xx": 0,
        "2xx": 0,
        "3xx": 0,
        "4xx": 0,
        "5xx": 0,
        "miss": 0,
        "bypass": 0,
        "expired": 0,
        "stale": 0,
        "updating": 0,
        "revalidated": 0,
        "hit": 0,
        "scarce": 0
      }
    },
    "ingress-with-invalid-path.sandbox.cosmic.sky": {
      "requestCounter": 10,
      "inBytes": 10,
      "outBytes": 5,
      "responses": {
        "1xx": 0,
        "2xx": 0,
        "3xx": 10,
        "4xx": 0,
        "5xx": 0,
        "miss": 0,
        "bypass": 0,
        "expired": 0,
        "stale": 0,
        "updating": 0,
        "revalidated": 0,
        "hit": 0,
        "scarce": 0
      },
      "overCounts": {
        "maxIntegerSize": 18446744073709551615,
        "requestCounter": 0,
        "inBytes": 0,
        "outBytes": 0,
        "1xx": 0,
        "2xx": 0,
        "3xx": 0,
        "4xx": 0,
        "5xx": 0,
        "miss": 0,
        "bypass": 0,
        "expired": 0,
        "stale": 0,
        "updating": 0,
        "revalidated": 0,
        "hit": 0,
        "scarce": 0
      }
    },
    "*": {
      "requestCounter": 10,
      "inBytes": 2910,
      "outBytes": 1570,
      "responses": {
        "1xx": 0,
        "2xx": 10,
        "3xx": 0,
        "4xx": 0,
        "5xx": 0,
        "miss": 0,
        "bypass": 0,
        "expired": 0,
        "stale": 0,
        "updating": 0,
        "revalidated": 0,
        "hit": 0,
        "scarce": 0
      },
      "overCounts": {
        "maxIntegerSize": 18446744073709551615,
        "requestCounter": 0,
        "inBytes": 0,
        "outBytes": 0,
        "1xx": 0,
        "2xx": 0,
        "3xx": 0,
        "4xx": 0,
        "5xx": 0,
        "miss": 0,
        "bypass": 0,
        "expired": 0,
        "stale": 0,
        "updating": 0,
        "revalidated": 0,
        "hit": 0,
        "scarce": 0
      }
    }
  },
  "filterZones": {
    "heapster-external.sandbox.cosmic.sky": {
      "/stuff/::kube-system.10.254.201.199.80": {
        "requestCounter": 19,
        "inBytes": 898,
        "outBytes": 471,
        "responses": {
          "1xx": 6,
          "2xx": 3,
          "3xx": 2,
          "4xx": 1,
          "5xx": 7,
          "miss": 0,
          "bypass": 0,
          "expired": 0,
          "stale": 0,
          "updating": 0,
          "revalidated": 0,
          "hit": 0,
          "scarce": 0
        },
        "overCounts": {
          "maxIntegerSize": 18446744073709551615,
          "requestCounter": 0,
          "inBytes": 0,
          "outBytes": 0,
          "1xx": 0,
          "2xx": 0,
          "3xx": 0,
          "4xx": 0,
          "5xx": 0,
          "miss": 0,
          "bypass": 0,
          "expired": 0,
          "stale": 0,
          "updating": 0,
          "revalidated": 0,
          "hit": 0,
          "scarce": 0
        }
      }
    },
    "heapster.sandbox.cosmic.sky": {
      "/::kube-system.10.254.201.199.80": {
        "requestCounter": 7,
        "inBytes": 2012,
        "outBytes": 1099,
        "responses": {
          "1xx": 0,
          "2xx": 7,
          "3xx": 0,
          "4xx": 0,
          "5xx": 0,
          "miss": 0,
          "bypass": 0,
          "expired": 0,
          "stale": 0,
          "updating": 0,
          "revalidated": 0,
          "hit": 0,
          "scarce": 0
        },
        "overCounts": {
          "maxIntegerSize": 18446744073709551615,
          "requestCounter": 0,
          "inBytes": 0,
          "outBytes": 0,
          "1xx": 0,
          "2xx": 0,
          "3xx": 0,
          "4xx": 0,
          "5xx": 0,
          "miss": 0,
          "bypass": 0,
          "expired": 0,
          "stale": 0,
          "updating": 0,
          "revalidated": 0,
          "hit": 0,
          "scarce": 0
        }
      }
    },
    "ingress-with-valid-duplicate-path.sandbox.cosmic.sky": {
      "/path/::some-app.10.254.204.100.8080": {
        "requestCounter": 10,
        "inBytes": 5000,
        "outBytes": 2000,
        "responses": {
          "1xx": 0,
          "2xx": 5,
          "3xx": 0,
          "4xx": 0,
          "5xx": 0,
          "miss": 0,
          "bypass": 0,
          "expired": 0,
          "stale": 0,
          "updating": 0,
          "revalidated": 0,
          "hit": 0,
          "scarce": 0
        },
        "overCounts": {
          "maxIntegerSize": 18446744073709551615,
          "requestCounter": 0,
          "inBytes": 0,
          "outBytes": 0,
          "1xx": 0,
          "2xx": 0,
          "3xx": 0,
          "4xx": 0,
          "5xx": 0,
          "miss": 0,
          "bypass": 0,
          "expired": 0,
          "stale": 0,
          "updating": 0,
          "revalidated": 0,
          "hit": 0,
          "scarce": 0
        }
      },
      "/path/::": {
        "requestCounter": 50,
        "inBytes": 50,
        "outBytes": 50,
        "responses": {
          "1xx": 50,
          "2xx": 50,
          "3xx": 50,
          "4xx": 50,
          "5xx": 50,
          "miss": 50,
          "bypass": 0,
          "expired": 0,
          "stale": 0,
          "updating": 0,
          "revalidated": 0,
          "hit": 0,
          "scarce": 0
        },
        "overCounts": {
          "maxIntegerSize": 18446744073709551615,
          "requestCounter": 0,
          "inBytes": 0,
          "outBytes": 0,
          "1xx": 0,
          "2xx": 0,
          "3xx": 0,
          "4xx": 0,
          "5xx": 0,
          "miss": 0,
          "bypass": 0,
          "expired": 0,
          "stale": 0,
          "updating": 0,
          "revalidated": 0,
          "hit": 0,
          "scarce": 0
        }
      }
    },
    "ingress-with-invalid-path.sandbox.cosmic.sky": {
      "/bad::": {
        "requestCounter": 10,
        "inBytes": 10,
        "outBytes": 5,
        "responses": {
          "1xx": 0,
          "2xx": 10,
          "3xx": 0,
          "4xx": 0,
          "5xx": 0,
          "miss": 0,
          "bypass": 0,
          "expired": 0,
          "stale": 0,
          "updating": 0,
          "revalidated": 0,
          "hit": 0,
          "scarce": 0
        },
        "overCounts": {
          "maxIntegerSize": 18446744073709551615,
          "requestCounter": 0,
          "inBytes": 0,
          "outBytes": 0,
          "1xx": 0,
          "2xx": 0,
          "3xx": 0,
          "4xx": 0,
          "5xx": 0,
          "miss": 0,
          "bypass": 0,
          "expired": 0,
          "stale": 0,
          "updating": 0,
          "revalidated": 0,
          "hit": 0,
          "scarce": 0
        }
      }
    }
  },
  "upstreamZones": {
    "kube-system.10.254.201.199.80": [
      {
        "server": "10.254.201.199:80",
        "requestCounter": 10,
        "inBytes": 2910,
        "outBytes": 1570,
        "responses": {
          "1xx": 1,
          "2xx": 10,
          "3xx": 9,
          "4xx": 2,
          "5xx": 3
        },
        "responseMsec": 1,
        "weight": 1,
        "maxFails": 1,
        "failTimeout": 10,
        "backup": false,
        "down": false,
        "overCounts": {
          "maxIntegerSize": 18446744073709551615,
          "requestCounter": 0,
          "inBytes": 0,
          "outBytes": 0,
          "1xx": 0,
          "2xx": 0,
          "3xx": 0,
          "4xx": 0,
          "5xx": 0
        }
      }
    ]
  }
}
`)
