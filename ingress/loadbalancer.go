package ingress

import (
	"io/ioutil"
	"os"
	"os/exec"
	"syscall"
)

// LoadBalancer that the controller will modify.
type LoadBalancer interface {
	Start() error
	Stop() error
	// Update the loadbalancer configuration. Returns if the LB was required to reload
	// its configuration
	Update(LoadBalancerUpdate) (bool, error)
	Healthy() bool
}

// Signaller interface around signalling the loadbalancer process
type Signaller interface {
	Sigquit(*os.Process) error
	Sighup(*os.Process) error
}

// DefaultSignaller sends os signals
type DefaultSignaller struct {
}

// Sigquit sends a SIGQUIT to the process
func (s *DefaultSignaller) Sigquit(p *os.Process) error {
	return p.Signal(syscall.SIGQUIT)
}

// Sighup sends a SIGHUP to the process
func (s *DefaultSignaller) Sighup(p *os.Process) error {
	return p.Signal(syscall.SIGHUP)
}

func writeFile(location string, contents []byte) (bool, error) {
	err := ioutil.WriteFile(location, contents, 0644)
	if err != nil {
		return false, err
	}
	return true, nil
}

func diff(b1, b2 []byte) ([]byte, error) {
	f1, err := ioutil.TempFile("", "")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f1.Name())
	defer f1.Close()

	f2, err := ioutil.TempFile("", "")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f2.Name())
	defer f2.Close()

	f1.Write(b1)
	f2.Write(b2)

	data, err := exec.Command("diff", "-u", f1.Name(), f2.Name()).CombinedOutput()
	if len(data) > 0 {
		return data, nil
	}
	return data, err
}
