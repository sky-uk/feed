package cmd

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sky-uk/feed/nginx"
)

func TestCreatePortsConfigWithoutPorts(t *testing.T) {
	if os.Getenv("BE_CRASHER") == "1" {
		createPortsConfig(unset, unset)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestCreatePortsConfigWithoutPorts")
	cmd.Env = append(os.Environ(), "BE_CRASHER=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("process ran with err %v, want exit status 1", err)
}

func TestCreatePortsConfigWithOnePort(t *testing.T) {
	ports := createPortsConfig(1, unset)
	expectedPorts := []nginx.Port{nginx.Port{Name: "http", Port: 1}}
	portsHTTPS := createPortsConfig(unset, 2)
	expectedPortsHTTPS := []nginx.Port{nginx.Port{Name: "https", Port: 2}}

	assert.Equal(t, expectedPorts, ports, "they should be equal")
	assert.Equal(t, expectedPortsHTTPS, portsHTTPS, "they should be equal")
}

func TestCreatePortsConfigWithPorts(t *testing.T) {
	ports := createPortsConfig(1, 2)
	expectedPorts := []nginx.Port{nginx.Port{Name: "http", Port: 1}, nginx.Port{Name: "https", Port: 2}}

	assert.Equal(t, expectedPorts, ports, "they should be equal")
}
