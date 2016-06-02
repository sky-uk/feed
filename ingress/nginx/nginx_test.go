package nginx

import (
	"testing"

	"io/ioutil"
	"os"
	"regexp"

	"os/exec"

	"github.com/sky-uk/feed/ingress/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	port         = 9090
	defaultAllow = "10.50.0.0/16"
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

func newLb(tmpDir string) (types.LoadBalancer, *mockSignaller) {
	return newLbWithBinary(tmpDir, "./fake_nginx.sh")
}

func newLbWithBinary(tmpDir string, binary string) (types.LoadBalancer, *mockSignaller) {
	lb := NewNginxLB(Conf{
		BinaryLocation:  binary,
		WorkingDir:      tmpDir,
		IngressPort:     port,
		WorkerProcesses: 1,
		DefaultAllow:    defaultAllow,
	})
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

func TestReloadOfConfig(t *testing.T) {
	assert := assert.New(t)
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb, mockSignaller := newLb(tmpDir)
	mockSignaller.On("sighup", mock.AnythingOfType("*os.Process")).Return(nil)

	assert.NoError(lb.Start())

	var tests = []struct {
		entries       []types.LoadBalancerEntry
		configEntries []string
	}{
		// Check full ingress entry works.
		{
			[]types.LoadBalancerEntry{
				types.LoadBalancerEntry{
					Host:        "chris.com",
					Name:        "chris-ingress",
					Path:        "/path",
					ServiceName: "service",
					ServicePort: 9090,
					Allow:       "10.82.0.0/16",
				},
			},
			[]string{
				"   # chris-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name chris.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 10.50.0.0/16;\n" +
					"        allow 127.0.0.1;\n" +
					"        allow 10.82.0.0/16;\n" +
					"        deny all;\n" +
					"\n" +
					"        location /path {\n" +
					"            proxy_pass http://service:9090;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
			},
		},
		// Check empty allow skips the allow for the ingress in the output.
		{
			[]types.LoadBalancerEntry{
				types.LoadBalancerEntry{
					Host:        "foo.com",
					Name:        "foo-ingress",
					Path:        "/bar",
					ServiceName: "lala",
					ServicePort: 8080,
				},
			},
			[]string{
				"   # foo-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name foo.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 10.50.0.0/16;\n" +
					"        allow 127.0.0.1;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location /bar {\n" +
					"            proxy_pass http://lala:8080;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
			},
		},
		// Check entries ordered by name.
		{
			[]types.LoadBalancerEntry{
				types.LoadBalancerEntry{
					Name:        "2-last-ingress",
					Host:        "foo.com",
					Path:        "/",
					ServiceName: "foo",
					ServicePort: 8080,
				},
				types.LoadBalancerEntry{
					Name:        "0-first-ingress",
					Host:        "foo.com",
					Path:        "/",
					ServiceName: "foo",
					ServicePort: 8080,
				},
				types.LoadBalancerEntry{
					Name:        "1-next-ingress",
					Host:        "foo.com",
					Path:        "/",
					ServiceName: "foo",
					ServicePort: 8080,
				},
			},
			[]string{
				"   # 0-first-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name foo.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 10.50.0.0/16;\n" +
					"        allow 127.0.0.1;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location / {\n" +
					"            proxy_pass http://foo:8080;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
				"   # 1-next-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name foo.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 10.50.0.0/16;\n" +
					"        allow 127.0.0.1;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location / {\n" +
					"            proxy_pass http://foo:8080;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
				"   # 2-last-ingress\n" +
					"    server {\n" +
					"        listen 9090;\n" +
					"        server_name foo.com;\n" +
					"\n" +
					"        # Restrict clients\n" +
					"        allow 10.50.0.0/16;\n" +
					"        allow 127.0.0.1;\n" +
					"        \n" +
					"        deny all;\n" +
					"\n" +
					"        location / {\n" +
					"            proxy_pass http://foo:8080;\n" +
					"        }\n" +
					"    }\n" +
					"    ",
			},
		},
	}

	for _, test := range tests {
		entries := test.entries
		updated, err := lb.Update(types.LoadBalancerUpdate{Entries: entries})
		assert.NoError(err)
		assert.True(updated)

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
	}

	assert.Nil(lb.Stop())
	mockSignaller.AssertExpectations(t)
}

func TestResolverIsSpecifiedIfNotEmpty(t *testing.T) {
	assert := assert.New(t)

	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	resolver := "10.254.0.10:53"
	lb := NewNginxLB(Conf{
		BinaryLocation:  "./fake_nginx.sh",
		WorkingDir:      tmpDir,
		IngressPort:     port,
		WorkerProcesses: 1,
		Resolver:        resolver,
	})
	signaller := &mockSignaller{}
	signaller.On("sigquit", mock.AnythingOfType("*os.Process")).Return(nil)
	lb.(*nginxLoadBalancer).signaller = signaller

	assert.NoError(lb.Start())

	config, err := ioutil.ReadFile(tmpDir + "/nginx.conf")
	assert.NoError(err)
	configContents := string(config)

	assert.Contains(configContents, "resolver "+resolver+";")
}

func TestResolverIsNotSpecifiedIfEmpty(t *testing.T) {
	assert := assert.New(t)

	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb, _ := newLb(tmpDir)
	assert.NoError(lb.Start())

	config, err := ioutil.ReadFile(tmpDir + "/nginx.conf")
	assert.NoError(err)
	configContents := string(config)

	assert.NotContains(configContents, "resolver ")

}

func TestDoesNotUpdateIfConfigurationHasNotChanged(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)
	lb, mockSignaller := newLb(tmpDir)
	mockSignaller.On("sighup", mock.AnythingOfType("*os.Process")).Return(nil)

	lb.Start()

	entries := []types.LoadBalancerEntry{
		types.LoadBalancerEntry{
			Host:        "chris.com",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
	}
	updated, err := lb.Update(types.LoadBalancerUpdate{Entries: entries})
	assert.NoError(t, err)
	assert.True(t, updated)

	updated, err = lb.Update(types.LoadBalancerUpdate{Entries: entries})
	assert.NoError(t, err)
	assert.False(t, updated)
}

func TestInvalidLoadBalancerEntryIsIgnored(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)
	lb, mockSignaller := newLb(tmpDir)
	mockSignaller.On("sighup", mock.AnythingOfType("*os.Process")).Return(nil)

	lb.Start()
	entries := []types.LoadBalancerEntry{
		types.LoadBalancerEntry{
			Host:        "chris.com",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
	}
	updated, err := lb.Update(types.LoadBalancerUpdate{Entries: entries})
	assert.NoError(t, err)

	// Add an invalid entry
	entries = []types.LoadBalancerEntry{
		types.LoadBalancerEntry{ // Invalid due to blank host
			Host:        "",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
		types.LoadBalancerEntry{ // Same as the one before
			Host:        "chris.com",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
	}
	updated, err = lb.Update(types.LoadBalancerUpdate{Entries: entries})
	assert.NoError(t, err)
	assert.False(t, updated)
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
