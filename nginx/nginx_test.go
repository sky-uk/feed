package nginx

import (
	"testing"

	"io/ioutil"
	"os"
	"regexp"

	"os/exec"

	"github.com/sky-uk/feed/controller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	port         = 9090
	defaultAllow = "10.50.0.0/16"
	fakeNginx    = "./fake_nginx.sh"
)

type mockSignaller struct {
	mock.Mock
}

func (m *mockSignaller) sigquit(p *os.Process) error {
	m.Called(p)
	return nil
}

func (m *mockSignaller) sighup(p *os.Process) error {
	m.Called(p)
	return nil
}

func newConf(tmpDir string, binary string) Conf {
	return Conf{
		WorkingDir:      tmpDir,
		BinaryLocation:  binary,
		IngressPort:     port,
		WorkerProcesses: 1,
		DefaultAllow:    defaultAllow,
	}
}

func newLb(tmpDir string) (controller.Updater, *mockSignaller) {
	return newLbWithBinary(tmpDir, fakeNginx)
}

func newLbWithBinary(tmpDir string, binary string) (controller.Updater, *mockSignaller) {
	conf := newConf(tmpDir, binary)
	return newLbWithConf(conf)
}

func newLbWithConf(conf Conf) (controller.Updater, *mockSignaller) {
	lb := New(conf)
	signaller := &mockSignaller{}
	signaller.On("sigquit", mock.AnythingOfType("*os.Process")).Return(nil)
	lb.(*nginxLoadBalancer).signaller = signaller
	return lb, signaller
}

func TestCanStartThenStop(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb, mockSignaller := newLb(tmpDir)

	assert.NoError(t, lb.Start())
	assert.NoError(t, lb.Stop())
	mockSignaller.AssertExpectations(t)
}

func TestStopWaitsForGracefulShutdownOfNginx(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb, _ := newLbWithBinary(tmpDir, "./fake_graceful_nginx.py")
	lb.(*nginxLoadBalancer).signaller = &osSignaller{}

	assert.NoError(lb.Start())
	assert.NoError(lb.Stop())
	assert.Error(lb.Health(), "should have waited for nginx to gracefully stop")
}

func TestHealthyWhileRunning(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb, _ := newLb(tmpDir)

	assert.Error(lb.Health(), "should be unhealthy")
	assert.NoError(lb.Start())
	assert.NoError(lb.Health(), "should be healthy")
	assert.NoError(lb.Stop())
	assert.Error(lb.Health(), "should be unhealthy")
}

func TestFailsIfNginxDiesEarly(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb, _ := newLbWithBinary(tmpDir, "./fake_failing_nginx.sh")

	assert.Error(lb.Start())
	assert.Error(lb.Health())
}

func TestCanSetLogLevel(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	defaultLogLevel := newConf(tmpDir, fakeNginx)
	customLogLevel := newConf(tmpDir, fakeNginx)
	customLogLevel.LogLevel = "info"

	var tests = []struct {
		nginxConf Conf
		logLine   string
	}{
		{
			defaultLogLevel,
			"error_log stderr warn;",
		},
		{
			customLogLevel,
			"error_log stderr info;",
		},
	}

	for _, test := range tests {
		lb, _ := newLbWithConf(test.nginxConf)
		assert.NoError(lb.Start())

		confBytes, err := ioutil.ReadFile(tmpDir + "/nginx.conf")
		assert.NoError(err)
		conf := string(confBytes)

		assert.Contains(conf, test.logLine)
	}
}

func TestNginxConfigUpdates(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	defaultConf := newConf(tmpDir, fakeNginx)

	var tests = []struct {
		lbConf        Conf
		entries       []controller.IngressEntry
		configEntries []string
	}{
		// Check full ingress entry works.
		{
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "chris.com",
					Name:           "chris-ingress",
					Path:           "/path",
					ServiceAddress: "service",
					ServicePort:    9090,
					Allow:          []string{"10.82.0.0/16"},
				},
			},
			[]string{
				"   # chris-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name chris.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 127.0.0.1;\n" +
					"        allow 10.82.0.0/16;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location /path/ {\n" +
					"            proxy_pass http://service:9090/;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
			},
		},
		// Check empty allow skips the allow for the ingress in the output.
		{
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "foo.com",
					Name:           "foo-ingress",
					Path:           "/bar",
					ServiceAddress: "lala",
					ServicePort:    8080,
				},
			},
			[]string{
				"   # foo-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name foo.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 127.0.0.1;\n" +
					"        allow 10.50.0.0/16;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location /bar/ {\n" +
					"            proxy_pass http://lala:8080/;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
			},
		},
		// Check entries ordered by name.
		{
			defaultConf,
			[]controller.IngressEntry{
				{
					Name:           "2-last-ingress",
					Host:           "foo.com",
					Path:           "/",
					ServiceAddress: "foo",
					ServicePort:    8080,
				},
				{
					Name:           "0-first-ingress",
					Host:           "foo.com",
					Path:           "/",
					ServiceAddress: "foo",
					ServicePort:    8080,
				},
				{
					Name:           "1-next-ingress",
					Host:           "foo.com",
					Path:           "/",
					ServiceAddress: "foo",
					ServicePort:    8080,
				},
			},
			[]string{
				"   # 0-first-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name foo.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 127.0.0.1;\n" +
					"        allow 10.50.0.0/16;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location / {\n" +
					"            proxy_pass http://foo:8080/;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
				"   # 1-next-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name foo.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 127.0.0.1;\n" +
					"        allow 10.50.0.0/16;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location / {\n" +
					"            proxy_pass http://foo:8080/;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
				"   # 2-last-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name foo.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 127.0.0.1;\n" +
					"        allow 10.50.0.0/16;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location / {\n" +
					"            proxy_pass http://foo:8080/;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
			},
		},
		// Check path slashes are added correctly
		{
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "chris.com",
					Name:           "chris-ingress",
					Path:           "",
					ServiceAddress: "service",
					ServicePort:    9090,
					Allow:          []string{"10.82.0.0/16"},
				},
				{
					Host:           "chris.com",
					Name:           "chris-ingress",
					Path:           "/prefix-with-slash/",
					ServiceAddress: "service",
					ServicePort:    9090,
					Allow:          []string{"10.82.0.0/16"},
				},
				{
					Host:           "chris.com",
					Name:           "chris-ingress",
					Path:           "prefix-without-preslash/",
					ServiceAddress: "service",
					ServicePort:    9090,
					Allow:          []string{"10.82.0.0/16"},
				},
				{
					Host:           "chris.com",
					Name:           "chris-ingress",
					Path:           "/prefix-without-postslash",
					ServiceAddress: "service",
					ServicePort:    9090,
					Allow:          []string{"10.82.0.0/16"},
				},
				{
					Host:           "chris.com",
					Name:           "chris-ingress",
					Path:           "prefix-without-anyslash",
					ServiceAddress: "service",
					ServicePort:    9090,
					Allow:          []string{"10.82.0.0/16"},
				},
			},
			[]string{
				"   # chris-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name chris.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 127.0.0.1;\n" +
					"        allow 10.82.0.0/16;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location / {\n" +
					"            proxy_pass http://service:9090/;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
				"   # chris-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name chris.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 127.0.0.1;\n" +
					"        allow 10.82.0.0/16;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location /prefix-with-slash/ {\n" +
					"            proxy_pass http://service:9090/;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
				"   # chris-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name chris.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 127.0.0.1;\n" +
					"        allow 10.82.0.0/16;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location /prefix-without-preslash/ {\n" +
					"            proxy_pass http://service:9090/;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
				"   # chris-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name chris.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 127.0.0.1;\n" +
					"        allow 10.82.0.0/16;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location /prefix-without-postslash/ {\n" +
					"            proxy_pass http://service:9090/;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
				"   # chris-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name chris.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 127.0.0.1;\n" +
					"        allow 10.82.0.0/16;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location /prefix-without-anyslash/ {\n" +
					"            proxy_pass http://service:9090/;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
			},
		},
		// Check multiple allows work
		{
			defaultConf,
			[]controller.IngressEntry{
				{
					Host:           "chris.com",
					Name:           "chris-ingress",
					Path:           "",
					ServiceAddress: "service",
					ServicePort:    9090,
					Allow:          []string{"10.82.0.0/16", "10.99.0.0/16"},
				},
			},
			[]string{
				"   # chris-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name chris.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 127.0.0.1;\n" +
					"        allow 10.82.0.0/16;\n" +
					"        allow 10.99.0.0/16;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location / {\n" +
					"            proxy_pass http://service:9090/;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
			},
		},
		// Check no allows work
		{
			Conf{
				WorkingDir:      tmpDir,
				BinaryLocation:  fakeNginx,
				IngressPort:     port,
				WorkerProcesses: 1,
				DefaultAllow:    "",
			},
			[]controller.IngressEntry{
				{
					Host:           "chris.com",
					Name:           "chris-ingress",
					Path:           "",
					ServiceAddress: "service",
					ServicePort:    9090,
				},
			},
			[]string{
				"   # chris-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name chris.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 127.0.0.1;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location / {\n" +
					"            proxy_pass http://service:9090/;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
			},
		},
	}

	for _, test := range tests {
		lb, mockSignaller := newLbWithConf(test.lbConf)
		mockSignaller.On("sighup", mock.AnythingOfType("*os.Process")).Return(nil)

		assert.NoError(lb.Start())
		entries := test.entries
		err := lb.Update(controller.IngressUpdate{Entries: entries})
		assert.NoError(err)

		config, err := ioutil.ReadFile(tmpDir + "/nginx.conf")
		assert.NoError(err)
		configContents := string(config)

		r, err := regexp.Compile("(?s)# Start entry\\n (.*?)# End entry")
		assert.NoError(err)
		serverEntries := r.FindAllStringSubmatch(configContents, -1)

		assert.Equal(len(test.configEntries), len(serverEntries))
		for i := range test.configEntries {
			assert.Equal(test.configEntries[i], serverEntries[i][1])
		}

		assert.Nil(lb.Stop())
		mockSignaller.AssertExpectations(t)
	}
}

func TestDoesNotUpdateIfConfigurationHasNotChanged(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)
	lb, mockSignaller := newLb(tmpDir)
	mockSignaller.On("sighup", mock.AnythingOfType("*os.Process")).Return(nil).Once()

	lb.Start()

	entries := []controller.IngressEntry{
		{
			Host:           "chris.com",
			Path:           "/path",
			ServiceAddress: "service",
			ServicePort:    9090,
		},
	}

	err := lb.Update(controller.IngressUpdate{Entries: entries})
	assert.NoError(err)
	config1, err := ioutil.ReadFile(tmpDir + "/nginx.conf")
	assert.NoError(err)

	err = lb.Update(controller.IngressUpdate{Entries: entries})
	assert.NoError(err)
	config2, err := ioutil.ReadFile(tmpDir + "/nginx.conf")
	assert.NoError(err)

	lb.Stop()

	assert.Equal(string(config1), string(config2), "configs should be identical")
	mockSignaller.AssertExpectations(t)
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
