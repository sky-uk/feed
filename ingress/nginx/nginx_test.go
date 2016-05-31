package nginx

import (
	"testing"

	"io/ioutil"
	"os"
	"regexp"

	"fmt"

	"os/exec"

	"github.com/sky-uk/feed/ingress/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	port = 9090
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

func newLb(tmpDir string) (api.LoadBalancer, *mockSignaller) {
	return newLbWithBinary(tmpDir, "./fake_nginx.sh")
}

func newLbWithBinary(tmpDir string, binary string) (api.LoadBalancer, *mockSignaller) {
	lb := NewNginxLB(Conf{
		BinaryLocation:  binary,
		WorkingDir:      tmpDir,
		Port:            port,
		WorkerProcesses: 1,
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
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb, _ := newLbWithBinary(tmpDir, "./fake_graceful_nginx.py")
	lb.(*nginxLoadBalancer).signaller = &osSignaller{}

	assert.NoError(t, lb.Start())
	assert.NoError(t, lb.Stop())
	assert.False(t, lb.Healthy(), "should have waited for nginx to gracefully stop")
}

func TestHealthyWhileRunning(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb, _ := newLb(tmpDir)

	assert.False(t, lb.Healthy(), "should be unhealthy")
	assert.NoError(t, lb.Start())
	assert.True(t, lb.Healthy(), "should be healthy")
	assert.NoError(t, lb.Stop())
	assert.False(t, lb.Healthy(), "should be unhealthy")
}

func TestFailsIfNginxDiesEarly(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb, _ := newLbWithBinary(tmpDir, "./fake_failing_nginx.sh")

	assert.Error(t, lb.Start())
	assert.False(t, lb.Healthy())
}

func TestReloadOfConfig(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb, mockSignaller := newLb(tmpDir)
	mockSignaller.On("sighup", mock.AnythingOfType("*os.Process")).Return(nil)

	assert.NoError(t, lb.Start())

	entries := []api.LoadBalancerEntry{
		api.LoadBalancerEntry{
			Host:        "chris.com",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
	}
	updated, err := lb.Update(api.LoadBalancerUpdate{entries})
	assert.NoError(t, err)
	assert.True(t, updated)

	config, err := ioutil.ReadFile(tmpDir + "/nginx.conf")
	assert.NoError(t, err)
	configContents := string(config)

	r, err := regexp.Compile("(?s)# Start entry\\n (.*?)# End entry")
	assert.NoError(t, err)
	serverEntries := r.FindAllStringSubmatch(configContents, -1)

	assert.Equal(t, 1, len(serverEntries))
	assert.Equal(t, fmt.Sprintf("   server {\n        listen %d;\n        server_name chris.com;\n        location /path {\n            proxy_pass http://service:9090;\n        }\n    }\n    ", port), serverEntries[0][1])

	assert.Nil(t, lb.Stop())
	mockSignaller.AssertExpectations(t)
}

func TestDoesNotUpdateIfConfigurationHasNotChanged(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)
	lb, mockSignaller := newLb(tmpDir)
	mockSignaller.On("sighup", mock.AnythingOfType("*os.Process")).Return(nil)

	lb.Start()

	entries := []api.LoadBalancerEntry{
		api.LoadBalancerEntry{
			Host:        "chris.com",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
	}
	updated, err := lb.Update(api.LoadBalancerUpdate{entries})
	assert.NoError(t, err)
	assert.True(t, updated)

	updated, err = lb.Update(api.LoadBalancerUpdate{entries})
	assert.NoError(t, err)
	assert.False(t, updated)
}

func TestInvalidLoadBalancerEntryIsIgnored(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)
	lb, mockSignaller := newLb(tmpDir)
	mockSignaller.On("sighup", mock.AnythingOfType("*os.Process")).Return(nil)

	lb.Start()
	entries := []api.LoadBalancerEntry{
		api.LoadBalancerEntry{
			Host:        "chris.com",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
	}
	updated, err := lb.Update(api.LoadBalancerUpdate{entries})
	assert.NoError(t, err)

	// Add an invalid entry
	entries = []api.LoadBalancerEntry{
		api.LoadBalancerEntry{ // Invalid due to blank host
			Host:        "",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
		api.LoadBalancerEntry{ // Same as the one before
			Host:        "chris.com",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
	}
	updated, err = lb.Update(api.LoadBalancerUpdate{entries})
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
