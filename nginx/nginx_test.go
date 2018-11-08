package nginx

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/util/metrics"
	"github.com/stretchr/testify/assert"
)

func init() {
	metrics.SetConstLabels(make(prometheus.Labels))
}

const (
	port          = 9090
	fakeNginx     = "./fake_graceful_nginx.py"
	smallWaitTime = time.Millisecond * 20
)

func newConf(tmpDir string, binary string) Conf {
	return Conf{
		WorkingDir:                   tmpDir,
		BinaryLocation:               binary,
		Ports:                        []Port{{Name: "http", Port: port}},
		WorkerProcesses:              1,
		BackendKeepalives:            1024,
		BackendConnectTimeoutSeconds: 1,
		ServerNamesHashMaxSize:       -1,
		ServerNamesHashBucketSize:    -1,
		UpdatePeriod:                 time.Second,
		VhostStatsSharedMemory:       1,
		OpenTracingPlugin:            "",
		OpenTracingConfig:            "",
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

	assert.True(t, nginxStartedSuccessfully(tmpDir))
}

func nginxStartedSuccessfully(nginxDir string) bool {
	filename := fmt.Sprintf("%s/nginx-started", nginxDir)
	file, _ := os.Open(filename)
	defer file.Close()

	buf := make([]byte, 8)
	file.Read(buf)
	return string(buf) == "started!"
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
	conf := newConf(tmpDir, "./fake_graceful_nginx.py")
	conf.HealthPort = getPort(ts)
	lb := newNginxWithConf(conf)

	assert.EqualError(lb.Health(), "waiting for initial update")
	assert.NoError(lb.Start())

	time.Sleep(smallWaitTime)
	assert.EqualError(lb.Health(), "waiting for initial update")
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

	incorrectLargeClientHeaderBufferConf := defaultConf
	incorrectLargeClientHeaderBufferConf.LargeClientHeaderBufferBlocks = 4

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
			"PROXY protocol disabled uses X-Forwarded-For header for real_ip",
			defaultConf,
			[]string{
				"real_ip_header X-Forwarded-For;",
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
	}

	for _, test := range tests {
		fmt.Println(test.name)
		lb := newNginxWithConf(test.conf)

		assert.NoError(lb.Start())
		err := lb.Update(controller.IngressEntries{})
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
					Host:                  "chris.com",
					Namespace:             "core",
					Name:                  "chris-ingress",
					Path:                  "/path",
					ServiceAddress:        "service",
					ServicePort:           8080,
					Allow:                 []string{"10.82.0.0/16"},
					StripPaths:            true,
					BackendTimeoutSeconds: 1,
				},
				{
					Host:                  "chris.com",
					Namespace:             "core",
					Name:                  "chris-ingress-another",
					Path:                  "/anotherpath",
					ServiceAddress:        "anotherservice",
					ServicePort:           6060,
					Allow:                 []string{"10.86.0.0/16"},
					StripPaths:            false,
					BackendTimeoutSeconds: 10,
					BackendMaxConnections: 1024,
				},
			},
			[]string{
				"    upstream core.anotherservice.6060 {\n" +
					"        server anotherservice:6060 max_conns=1024;\n" +
					"        keepalive 1024;\n" +
					"    }",
				"    upstream core.service.8080 {\n" +
					"        server service:8080 max_conns=0;\n" +
					"        keepalive 1024;\n" +
					"    }",
			},
			[]string{
				"    server {\n" +
					"        listen 9090;\n" +
					"        server_name chris.com;\n" +
					"\n" +
					"        # disable any limits to avoid HTTP 413 for large uploads\n" +
					"        client_max_body_size 0;\n" +
					"\n" +
					"        location /anotherpath/ {\n" +
					"            # Keep original path when proxying.\n" +
					"            proxy_pass http://core.anotherservice.6060;\n" +
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
					"            proxy_pass http://core.service.8080/;\n" +
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
					"    }",
			},
		},
		{
			"Check no allows works",
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "chris.com",
					Namespace:      "core",
					Name:           "chris-ingress",
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
					Host:           "chris.com",
					Namespace:      "core",
					Name:           "chris-ingress",
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
			"Check path slashes are added correctly",
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "chris-0.com",
					Namespace:      "core",
					Name:           "chris-ingress",
					Path:           "",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
				{
					Host:           "chris-1.com",
					Namespace:      "core",
					Name:           "chris-ingress",
					Path:           "/prefix-with-slash/",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
				{
					Host:           "chris-2.com",
					Namespace:      "core",
					Name:           "chris-ingress",
					Path:           "prefix-without-preslash/",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
				{
					Host:           "chris-3.com",
					Namespace:      "core",
					Name:           "chris-ingress",
					Path:           "/prefix-without-postslash",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
				{
					Host:           "chris-4.com",
					Namespace:      "core",
					Name:           "chris-ingress",
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
			"Check multiple allows work",
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "chris.com",
					Namespace:      "core",
					Name:           "chris-ingress",
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
					Host:           "chris.com",
					Namespace:      "core",
					Name:           "chris-ingress",
					Path:           "/my-path",
					ServiceAddress: "service",
					ServicePort:    9090,
				},

				{
					Host:           "chris.com",
					Namespace:      "core",
					Name:           "chris-ingress",
					Path:           "/my-path2",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
			},

			[]string{
				"    upstream core.service.9090 {\n" +
					"        server service:9090 max_conns=0;\n" +
					"        keepalive 1024;\n" +
					"    }",
			},
			nil,
		},
		{
			"Ingress names are ordered in comment to prevent diff generation",
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "chris.com",
					Namespace:      "core",
					Name:           "02chris-ingress",
					Path:           "/my-path",
					ServiceAddress: "service",
					ServicePort:    9090,
				},

				{
					Host:           "chris.com",
					Namespace:      "core",
					Name:           "01chris-ingress",
					Path:           "/my-path2",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
			},
			nil,
			[]string{
				"# ingress: core/01chris-ingress core/02chris-ingress",
			},
		},
		{
			"Disabled path stripping should not put a trailing slash on proxy_pass",
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "chris.com",
					Namespace:      "core",
					Name:           "chris-ingress",
					Path:           "/path",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
			},
			nil,
			[]string{
				"    proxy_pass http://core.service.9090;\n",
			},
		},
		{
			"PROXY protocol enables proxy_protocol listeners",
			enableProxyProtocolConf,
			[]controller.IngressEntry{
				{
					Host:           "chris.com",
					Namespace:      "core",
					Name:           "chris-ingress",
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
					Host:                  "chris.com",
					Namespace:             "core",
					Name:                  "chris-ingress",
					Path:                  "",
					ServiceAddress:        "service",
					ServicePort:           9090,
					BackendTimeoutSeconds: 28,
				},
				{
					Host:                  "chris.com",
					Namespace:             "core",
					Name:                  "chris-ingress",
					Path:                  "/lala",
					ServiceAddress:        "service",
					ServicePort:           9090,
					BackendTimeoutSeconds: 28,
				},
				{
					Host:                  "chris.com",
					Namespace:             "core",
					Name:                  "chris-ingress",
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
					"            proxy_pass http://core.service.9090;\n" +
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
					"            proxy_pass http://core.service.9090;\n" +
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
					"            proxy_pass http://core.service.9090;\n" +
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
					"            proxy_pass http://core.service.9090;\n" +
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
	lb := newUpdaterWithBinary(tmpDir, "./fake_graceful_nginx.py")

	assert.NoError(lb.Start())

	entries := []controller.IngressEntry{
		{
			Host:           "chris.com",
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
	lb := newUpdaterWithBinary(tmpDir, "./fake_graceful_nginx.py")

	assert.NoError(lb.Start())

	entries := []controller.IngressEntry{
		{
			Host:           "chris.com",
			Path:           "/path",
			ServiceAddress: "service",
			ServicePort:    9090,
		},
	}

	updatedEntries := []controller.IngressEntry{
		{
			Host:           "chris.com",
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
}

func assertIngressRequestCounters(t *testing.T, host, path string, in, out, ones, twos, threes, fours, fives float64) {
	assert := assert.New(t)
	inBytes, _ := ingressBytes.GetMetricWithLabelValues(host, path, "in")
	assert.Equal(in, metricValue(inBytes), "in bytes for %s%s", host, path)
	outBytes, _ := ingressBytes.GetMetricWithLabelValues(host, path, "out")
	assert.Equal(out, metricValue(outBytes), "out bytes for %s%s", host, path)
	req1xx, _ := ingressRequests.GetMetricWithLabelValues(host, path, "1xx")
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
	assert.Equal(in, metricValue(inBytes), "in bytes for %s%s", name, endpoint)
	outBytes, _ := endpointBytes.GetMetricWithLabelValues(name, endpoint, "out")
	assert.Equal(out, metricValue(outBytes), "out bytes for %s%s", name, endpoint)
	req1xx, _ := endpointRequests.GetMetricWithLabelValues(name, endpoint, "1xx")
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

func TestFailsToUpdateIfConfigurationIsBroken(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)
	lb := newUpdaterWithBinary(tmpDir, "./fake_nginx_failing_reload.sh")

	assert.NoError(lb.Start())

	entries := []controller.IngressEntry{
		{
			Host:           "chris.com",
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
