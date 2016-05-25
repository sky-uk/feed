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

// Nginx implementation
type nginxLoadBalancer struct {
	NginxConf
	cmd       *exec.Cmd
	signaller Signaller
	running   safeBool
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
		NginxConf: nginxConf,
		signaller: &DefaultSignaller{}}
}

func (lb *nginxLoadBalancer) Start() error {
	err := lb.initialiseNginxConf()
	if err != nil {
		return fmt.Errorf("unable to initialise nginx config: %v", err)
	}

	lb.cmd = exec.Command(lb.BinaryLocation, "-c", lb.nginxConfFile())

	lb.cmd.Stdout = log.StandardLogger().Writer()
	lb.cmd.Stderr = log.StandardLogger().Writer()
	lb.cmd.Stdin = os.Stdin

	log.Info("(Ignore errors about /var/log/nginx/error.log - they are expected)")
	err = lb.cmd.Start()
	if err != nil {
		return fmt.Errorf("unable to start nginx: %v", err)
	}

	lb.running.set(true)

	go func() {
		log.Info("Nginx has exited: ", lb.cmd.Wait())
		lb.running.set(false)
	}()

	time.Sleep(nginxStartDelay)
	if !lb.running.get() {
		return fmt.Errorf("nginx died shortly after starting")
	}

	log.Debugf("Nginx pid %d", lb.cmd.Process.Pid)
	return nil
}

func (lb *nginxLoadBalancer) initialiseNginxConf() error {
	err := os.Remove(lb.nginxConfFile())
	if err != nil {
		log.Debugf("Can't remove nginx.conf: %v", err)
	}
	_, err = lb.update(LoadBalancerUpdate{Entries: []LoadBalancerEntry{}})
	return err
}

func (lb *nginxLoadBalancer) Stop() error {
	log.Info("Shutting down nginx process")
	err := lb.signaller.Sigquit(lb.cmd.Process)
	if err != nil {
		return fmt.Errorf("error shutting down nginx: %v", err)
	}
	return nil
}

func (lb *nginxLoadBalancer) Update(entries LoadBalancerUpdate) (bool, error) {
	updated, err := lb.update(entries)
	if err != nil {
		return false, fmt.Errorf("unable to update nginx: %v", err)
	}
	if updated {
		err = lb.signaller.Sighup(lb.cmd.Process)
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
