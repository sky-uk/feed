package ingress

import (
	"io/ioutil"
	"os"
	"os/exec"

	"bytes"
	"fmt"
	"text/template"

	"strings"

	"time"

	"syscall"

	log "github.com/Sirupsen/logrus"
)

const nginxStartDelay = time.Millisecond * 100

// NginxConf configuration for nginx
type NginxConf struct {
	BinaryLocation  string
	WorkingDir      string
	WorkerProcesses int
	Port            int
}

// Signaller interface around signalling the loadbalancer process
type signaller interface {
	sigquit(*os.Process) error
	sighup(*os.Process) error
}

type osSignaller struct {
}

// Sigquit sends a SIGQUIT to the process
func (s *osSignaller) sigquit(p *os.Process) error {
	log.Debugf("Sending SIGQUIT to %d", p.Pid)
	return p.Signal(syscall.SIGQUIT)
}

// Sighup sends a SIGHUP to the process
func (s *osSignaller) sighup(p *os.Process) error {
	log.Debugf("Sending SIGHUP to %d", p.Pid)
	return p.Signal(syscall.SIGHUP)
}

// Nginx implementation
type nginxLoadBalancer struct {
	NginxConf
	cmd        *exec.Cmd
	signaller  signaller
	running    safeBool
	finishedCh chan error
}

// Used for generating nginx cofnig
type loadBalancerTemplate struct {
	Config  NginxConf
	Entries []LoadBalancerEntry
}

func (lb *nginxLoadBalancer) nginxConfFile() string {
	return lb.WorkingDir + "/nginx.conf"
}

// NewNginxLB creates a new LoadBalancer
func NewNginxLB(nginxConf NginxConf) LoadBalancer {
	nginxConf.WorkingDir = strings.TrimSuffix(nginxConf.WorkingDir, "/")
	return &nginxLoadBalancer{
		NginxConf:  nginxConf,
		signaller:  &osSignaller{},
		finishedCh: make(chan error),
	}
}

func (lb *nginxLoadBalancer) Start() error {
	if err := lb.logNginxVersion(); err != nil {
		return err
	}

	if err := lb.initialiseNginxConf(); err != nil {
		return fmt.Errorf("unable to initialise nginx config: %v", err)
	}

	lb.cmd = exec.Command(lb.BinaryLocation, "-c", lb.nginxConfFile())

	lb.cmd.Stdout = log.StandardLogger().Writer()
	lb.cmd.Stderr = log.StandardLogger().Writer()
	lb.cmd.Stdin = os.Stdin

	log.Info("(Ignore errors about /var/log/nginx/error.log - they are expected)")
	if err := lb.cmd.Start(); err != nil {
		return fmt.Errorf("unable to start nginx: %v", err)
	}

	lb.running.set(true)
	go lb.waitForNginxToFinish()

	time.Sleep(nginxStartDelay)
	if !lb.running.get() {
		return fmt.Errorf("nginx died shortly after starting")
	}

	log.Debugf("Nginx pid %d", lb.cmd.Process.Pid)
	return nil
}

func (lb *nginxLoadBalancer) logNginxVersion() error {
	cmd := exec.Command(lb.BinaryLocation, "-v")
	cmd.Stdout = log.StandardLogger().Writer()
	cmd.Stderr = log.StandardLogger().Writer()
	return cmd.Run()
}

func (lb *nginxLoadBalancer) initialiseNginxConf() error {
	err := os.Remove(lb.nginxConfFile())
	if err != nil {
		log.Debugf("Can't remove nginx.conf: %v", err)
	}
	_, err = lb.update(LoadBalancerUpdate{Entries: []LoadBalancerEntry{}})
	return err
}

func (lb *nginxLoadBalancer) waitForNginxToFinish() {
	err := lb.cmd.Wait()
	if err != nil {
		log.Error("Nginx has exited with an error: ", err)
	} else {
		log.Info("Nginx has shutdown successfully")
	}
	lb.running.set(false)
	lb.finishedCh <- err
}

func (lb *nginxLoadBalancer) Stop() error {
	log.Info("Shutting down nginx process")
	lb.cmd.Process.Signal(syscall.SIGQUIT)
	if err := lb.signaller.sigquit(lb.cmd.Process); err != nil {
		return fmt.Errorf("error shutting down nginx: %v", err)
	}
	err := <-lb.finishedCh
	return err
}

func (lb *nginxLoadBalancer) Update(entries LoadBalancerUpdate) (bool, error) {
	updated, err := lb.update(entries)
	if err != nil {
		return false, fmt.Errorf("unable to update nginx: %v", err)
	}
	if updated {
		err = lb.signaller.sighup(lb.cmd.Process)
		if err != nil {
			return false, fmt.Errorf("unable to signal nginx to reload: %v", err)
		}
	}
	return updated, err
}

func (lb *nginxLoadBalancer) update(entries LoadBalancerUpdate) (bool, error) {
	log.Debugf("Updating loadbalancer %s", entries)
	file, err := lb.createConfig(entries)
	if err != nil {
		return false, err
	}
	existing, err := ioutil.ReadFile(lb.nginxConfFile())
	if err != nil {
		log.Debugf("Error trying to read nginx.conf: %v", err)
		log.Info("Creating nginx.conf for the first time")
		return writeFile(lb.nginxConfFile(), file)

	}
	diffOutput, err := diff(existing, file)
	if err != nil {
		log.Warn("Unable to diff nginx files", err)
		return false, err
	}

	if len(diffOutput) == 0 {
		log.Info("Configuration has not changed")
		return false, nil
	}
	log.Infof("Diff output %s", string(diffOutput))

	log.Info("Configuration is different. Updating configuration.")
	_, err = writeFile(lb.nginxConfFile(), file)

	if err != nil {
		log.Error("Unable to write new nginx configuration", err)
		return false, err
	}
	return true, nil
}

func (lb *nginxLoadBalancer) createConfig(update LoadBalancerUpdate) ([]byte, error) {
	tmpl, err := template.New("nginx.tmpl").ParseFiles(lb.WorkingDir + "/nginx.tmpl")
	if err != nil {
		return nil, err
	}

	var output bytes.Buffer
	validEntries := filterInvalidEntries(update.Entries)
	err = tmpl.Execute(&output, loadBalancerTemplate{Config: lb.NginxConf, Entries: validEntries})

	if err != nil {
		return []byte{}, fmt.Errorf("Unable to execute nginx config duration. It will be out of date: %v", err)
	}

	return output.Bytes(), nil
}

func (lb *nginxLoadBalancer) Healthy() bool {
	return lb.running.get()
}

func (lb *nginxLoadBalancer) String() string {
	return "[nginx lb]"
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
