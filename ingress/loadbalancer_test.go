package ingress

import (
	"testing"

	"io/ioutil"
	"os"
	"regexp"

	"fmt"

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

func newLb(tmpDir string, mockSignaller Signaller) LoadBalancer {
	lb := NewNginxLB(NginxConf{
		BinaryLocation:  "./fake_nginx.sh",
		ConfigDir:       tmpDir,
		Port:            port,
		WorkerProcesses: 1,
	})
	lb.(*nginxLoadBalancer).signaller = mockSignaller
	return lb
}

func TestGracefulShutdown(t *testing.T) {
	tmpDir, err := ioutil.TempDir(os.TempDir(), "ingress_lb_test")
	assert.Nil(t, err)
	defer os.Remove(tmpDir)
	mockSignaller := &mockSignaller{}
	mockSignaller.On("Sigquit", mock.AnythingOfType("*os.Process")).Return(nil)

	lb := newLb(tmpDir, mockSignaller)

	assert.Nil(t, lb.Start())
	assert.Nil(t, lb.Stop())
	mockSignaller.AssertExpectations(t)
}

func TestReloadOfConfig(t *testing.T) {
	tmpDir, err := ioutil.TempDir(os.TempDir(), "ingress_lb_test")
	assert.Nil(t, err)
	defer os.Remove(tmpDir)
	mockSignaller := &mockSignaller{}
	mockSignaller.On("Sigquit", mock.AnythingOfType("*os.Process")).Return(nil)
	mockSignaller.On("Sighup", mock.AnythingOfType("*os.Process")).Return(nil)

	lb := newLb(tmpDir, mockSignaller)
	assert.Nil(t, lb.Start())

	entries := []LoadBalancerEntry{
		LoadBalancerEntry{
			Host:        "chris.com",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
	}
	updated, err := lb.Update(LoadBalancerUpdate{entries})
	assert.True(t, updated)

	config, err := ioutil.ReadFile(tmpDir + "/nginx.conf")
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
	tmpDir, _ := ioutil.TempDir(os.TempDir(), "ingress_lb_test")
	defer os.Remove(tmpDir)
	mockSignaller := &mockSignaller{}
	mockSignaller.On("Sigquit", mock.AnythingOfType("*os.Process")).Return(nil)
	mockSignaller.On("Sighup", mock.AnythingOfType("*os.Process")).Return(nil)
	lb := newLb(tmpDir, mockSignaller)
	lb.Start()

	entries := []LoadBalancerEntry{
		LoadBalancerEntry{
			Host:        "chris.com",
			Path:        "/path",
			ServiceName: "service",
			ServicePort: 9090,
		},
	}
	updated, _ := lb.Update(LoadBalancerUpdate{entries})
	assert.True(t, updated)

	updated, _ = lb.Update(LoadBalancerUpdate{entries})
	assert.False(t, updated)
}

func TestInvalidLoadBalancerEntryIsIgnored(t *testing.T) {
	tmpDir, _ := ioutil.TempDir(os.TempDir(), "ingress_lb_test")
	defer os.Remove(tmpDir)
	mockSignaller := &mockSignaller{}
	mockSignaller.On("Sigquit", mock.AnythingOfType("*os.Process")).Return(nil)
	mockSignaller.On("Sighup", mock.AnythingOfType("*os.Process")).Return(nil)
	lb := newLb(tmpDir, mockSignaller)
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
