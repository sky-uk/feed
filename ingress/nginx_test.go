package ingress

import (
	"testing"

	"io/ioutil"
	"os"
	"regexp"

	"fmt"

	"os/exec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	port = 9090
)

type mockSignaller struct {
	mock.Mock
}

func (m *mockSignaller) Sigquit(p *os.Process) error {
	m.Called(p)
	return nil
}

func (m *mockSignaller) Sighup(p *os.Process) error {
	m.Called(p)
	return nil
}

func newLb(tmpDir string) (LoadBalancer, *mockSignaller) {
	return newLbWithBinary(tmpDir, "./fake_nginx.sh")
}

func newLbWithBinary(tmpDir string, binary string) (LoadBalancer, *mockSignaller) {
	lb := NewNginxLB(NginxConf{
		BinaryLocation:  binary,
		WorkingDir:      tmpDir,
		Port:            port,
		WorkerProcesses: 1,
	})
	signaller := &mockSignaller{}
	signaller.On("Sigquit", mock.AnythingOfType("*os.Process")).Return(nil)
	lb.(*nginxLoadBalancer).signaller = signaller
	return lb, signaller
}

func TestGracefulShutdown(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb, mockSignaller := newLb(tmpDir)

	assert.NoError(t, lb.Start())
	assert.NoError(t, lb.Stop())
	mockSignaller.AssertExpectations(t)
}

func TestHealthyWhileRunning(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)

	lb, _ := newLb(tmpDir)

	assert.False(t, lb.Healthy(), "should be unhealthy")
	lb.Start()
	assert.True(t, lb.Healthy(), "should be healthy")
	lb.Stop()
	assert.True(t, lb.Healthy(), "should be unhealthy")
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
	mockSignaller.On("Sighup", mock.AnythingOfType("*os.Process")).Return(nil)

	assert.NoError(t, lb.Start())

	entries := []LoadBalancerEntry{
		LoadBalancerEntry{
			Host:        "chris.com",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
	}
	updated, err := lb.Update(LoadBalancerUpdate{entries})
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
	mockSignaller.On("Sighup", mock.AnythingOfType("*os.Process")).Return(nil)

	lb.Start()

	entries := []LoadBalancerEntry{
		LoadBalancerEntry{
			Host:        "chris.com",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
	}
	updated, err := lb.Update(LoadBalancerUpdate{entries})
	assert.NoError(t, err)
	assert.True(t, updated)

	updated, err = lb.Update(LoadBalancerUpdate{entries})
	assert.NoError(t, err)
	assert.False(t, updated)
}

func TestInvalidLoadBalancerEntryIsIgnored(t *testing.T) {
	tmpDir := setupWorkDir(t)
	defer os.Remove(tmpDir)
	lb, mockSignaller := newLb(tmpDir)
	mockSignaller.On("Sighup", mock.AnythingOfType("*os.Process")).Return(nil)

	lb.Start()
	entries := []LoadBalancerEntry{
		LoadBalancerEntry{
			Host:        "chris.com",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
	}
	updated, err := lb.Update(LoadBalancerUpdate{entries})
	assert.NoError(t, err)

	// Add an invalid entry
	entries = []LoadBalancerEntry{
		LoadBalancerEntry{ // Invalid due to blank host
			Host:        "",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
		LoadBalancerEntry{ // Same as the one before
			Host:        "chris.com",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
	}
	updated, err = lb.Update(LoadBalancerUpdate{entries})
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
