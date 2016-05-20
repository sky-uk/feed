package ingress

import (
	"io/ioutil"
	"os"
	"os/exec"

	"bytes"
	"fmt"
	"text/template"

	log "github.com/Sirupsen/logrus"
)

// NginxConf configuration for nginx
type NginxConf struct {
	BinaryLocation  string
	ConfigDir       string
	WorkerProcesses int
	Port            int
}

// Nginx implementation
type nginxLoadBalancer struct {
	conf      NginxConf
	cmd       *exec.Cmd
	signaller Signaller
}

// Used for generating nginx cofnig
type loadBalancerTemplate struct {
	Config  NginxConf
	Entries []LoadBalancerEntry
}

func (lb *nginxLoadBalancer) configLocation() string {
	return lb.conf.ConfigDir + "/nginx.conf"
}

// NewNginxLB creates a new LoadBalancer
func NewNginxLB(nginxConf NginxConf) LoadBalancer {
	return &nginxLoadBalancer{
		conf:      nginxConf,
		signaller: &DefaultSignaller{}}
}

func (lb *nginxLoadBalancer) Start() error {
	// Write out initial configuration
	lb.update(LoadBalancerUpdate{Entries: []LoadBalancerEntry{}})

	cmd := exec.Command(lb.conf.BinaryLocation, "-c", lb.configLocation())

	cmd.Stdout = log.StandardLogger().Writer()
	cmd.Stderr = log.StandardLogger().Writer()
	cmd.Stdin = os.Stdin

	lb.cmd = cmd
	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("Inable to start nginx %v", err)
	}

	go func() {
		log.Info("Nginx has exited: ", cmd.Wait())
	}()

	log.Debugf("Nginx pid %d", cmd.Process.Pid)
	return nil
}

func (lb *nginxLoadBalancer) Stop() error {
	log.Info("Shutting down process %d", lb.cmd.Process.Pid)
	err := lb.signaller.Sigquit(lb.cmd.Process)
	if err != nil {
		return fmt.Errorf("Error shutting down nginx %v", err)
	}
	return nil
}

func (lb *nginxLoadBalancer) Update(entries LoadBalancerUpdate) (bool, error) {
	updated, err := lb.update(entries)
	if err != nil {
		return false, fmt.Errorf("Unable to update nginx %v", err)
	}
	if updated {
		err = lb.signaller.Sighup(lb.cmd.Process)
		if err != nil {
			return false, fmt.Errorf("Configuration was wirtten but unable to signal nginx to reload configuration %v", err)
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
	existing, err := ioutil.ReadFile(lb.configLocation())
	if err != nil {
		log.Info("Unable to read existing nginx config file. Assuming it needs creating for the first time.", err)
		return writeFile(lb.configLocation(), file)

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
	_, err = writeFile(lb.configLocation(), file)

	if err != nil {
		log.Error("Unable to write new nginx configuration", err)
		return false, err
	}
	return true, nil
}

func (lb *nginxLoadBalancer) createConfig(update LoadBalancerUpdate) ([]byte, error) {
	tmpl, err := template.New("nginx.tmpl").ParseFiles("./nginx.tmpl")
	if err != nil {
		return nil, err
	}

	var output bytes.Buffer
	validEntries := filterInvalidEntries(update.Entries)
	err = tmpl.Execute(&output, loadBalancerTemplate{Config: lb.conf, Entries: validEntries})

	if err != nil {
		return []byte{}, fmt.Errorf("Unable to execute nginx config duration. It will be out of date: %v", err)
	}

	return output.Bytes(), nil
}

func (lb *nginxLoadBalancer) String() string {
	return "[nginx lb]"
}
