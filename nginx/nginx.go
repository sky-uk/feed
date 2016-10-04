package nginx

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

	"errors"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/util"
)

const (
	nginxStartDelay       = time.Millisecond * 100
	metricsUpdateInterval = time.Second * 10
)

// Conf configuration for nginx
type Conf struct {
	BinaryLocation               string
	WorkingDir                   string
	WorkerProcesses              int
	WorkerConnections            int
	KeepaliveSeconds             int
	BackendKeepalives            int
	BackendConnectTimeoutSeconds int
	ServerNamesHashBucketSize    int
	ServerNamesHashMaxSize       int
	HealthPort                   int
	TrustedFrontends             []string
	IngressPort                  int
	LogLevel                     string
	ProxyProtocol                bool
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
	Conf
	cmd              *exec.Cmd
	signaller        signaller
	running          util.SafeBool
	lastErr          util.SafeError
	metricsUnhealthy util.SafeBool
	doneCh           chan struct{}
}

// Used for generating nginx config
type loadBalancerTemplate struct {
	Conf
	Entries []*nginxEntry
}

type nginxEntry struct {
	Name       string
	ServerName string
	Upstreams  []upstream
	Locations  []location
}

type upstream struct {
	ID     string
	Server string
}

type location struct {
	Path                    string
	UpstreamID              string
	Allow                   []string
	StripPath               bool
	BackendKeepaliveSeconds int
}

func (lb *nginxLoadBalancer) nginxConfFile() string {
	return lb.WorkingDir + "/nginx.conf"
}

// New creates an nginx proxy.
func New(nginxConf Conf) controller.Updater {
	initMetrics()

	nginxConf.WorkingDir = strings.TrimSuffix(nginxConf.WorkingDir, "/")
	if nginxConf.LogLevel == "" {
		nginxConf.LogLevel = "warn"
	}

	return &nginxLoadBalancer{
		Conf:      nginxConf,
		signaller: &osSignaller{},
		doneCh:    make(chan struct{}),
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

	if err := lb.cmd.Start(); err != nil {
		return fmt.Errorf("unable to start nginx: %v", err)
	}

	lb.running.Set(true)
	go lb.waitForNginxToFinish()

	time.Sleep(nginxStartDelay)
	if !lb.running.Get() {
		return errors.New("nginx died shortly after starting")
	}

	go lb.periodicallyUpdateMetrics()

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
	_, err = lb.update(controller.IngressUpdate{Entries: []controller.IngressEntry{}})
	return err
}

func (lb *nginxLoadBalancer) waitForNginxToFinish() {
	err := lb.cmd.Wait()
	if err != nil {
		log.Error("Nginx has exited with an error: ", err)
	} else {
		log.Info("Nginx has shutdown successfully")
	}
	lb.running.Set(false)
	lb.lastErr.Set(err)
	close(lb.doneCh)
}

func (lb *nginxLoadBalancer) periodicallyUpdateMetrics() {
	lb.updateMetrics()
	ticker := time.NewTicker(metricsUpdateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-lb.doneCh:
			return
		case <-ticker.C:
			lb.updateMetrics()
		}
	}
}

func (lb *nginxLoadBalancer) updateMetrics() {
	if err := parseAndSetNginxMetrics(lb.HealthPort, "/basic_status"); err != nil {
		log.Warnf("Unable to update nginx metrics: %v", err)
		lb.metricsUnhealthy.Set(true)
	} else {
		lb.metricsUnhealthy.Set(false)
	}
}

func (lb *nginxLoadBalancer) Stop() error {
	log.Info("Shutting down nginx process")
	lb.cmd.Process.Signal(syscall.SIGQUIT)
	if err := lb.signaller.sigquit(lb.cmd.Process); err != nil {
		return fmt.Errorf("error shutting down nginx: %v", err)
	}
	<-lb.doneCh
	return lb.lastErr.Get()
}

func (lb *nginxLoadBalancer) Update(entries controller.IngressUpdate) error {
	updated, err := lb.update(entries)
	if err != nil {
		return fmt.Errorf("unable to update nginx: %v", err)
	}

	if updated {
		err = lb.signaller.sighup(lb.cmd.Process)
		if err != nil {
			return fmt.Errorf("unable to signal nginx to reload: %v", err)
		}
		log.Info("Nginx updated")
	}

	return nil
}

func (lb *nginxLoadBalancer) update(entries controller.IngressUpdate) (bool, error) {
	log.Debugf("Updating loadbalancer %s", entries)
	updatedConfig, err := lb.createConfig(entries)
	if err != nil {
		return false, err
	}

	existingConfig, err := ioutil.ReadFile(lb.nginxConfFile())
	if err != nil {
		log.Debugf("Error trying to read nginx.conf: %v", err)
		log.Info("Creating nginx.conf for the first time")
		return writeFile(lb.nginxConfFile(), updatedConfig)
	}

	return lb.diffAndUpdate(existingConfig, updatedConfig)
}

func (lb *nginxLoadBalancer) diffAndUpdate(existing, updated []byte) (bool, error) {
	diffOutput, err := diff(existing, updated)
	if err != nil {
		log.Warnf("Unable to diff nginx files: %v", err)
		return false, err
	}

	if len(diffOutput) == 0 {
		log.Info("Configuration has not changed")
		return false, nil
	}

	log.Infof("Updating nginx config: %s", string(diffOutput))
	_, err = writeFile(lb.nginxConfFile(), updated)

	if err != nil {
		log.Errorf("Unable to write nginx configuration: %v", err)
		return false, err
	}

	err = lb.checkNginxConfig()
	if err != nil {
		return false, err
	}

	return true, nil
}

func (lb *nginxLoadBalancer) checkNginxConfig() error {
	cmd := exec.Command(lb.BinaryLocation, "-t", "-c", lb.nginxConfFile())
	var out bytes.Buffer
	cmd.Stderr = &out
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("invalid config: %v: %s", err, out.String())
	}
	return nil
}

func (lb *nginxLoadBalancer) createConfig(update controller.IngressUpdate) ([]byte, error) {
	tmpl, err := template.New("nginx.tmpl").ParseFiles(lb.WorkingDir + "/nginx.tmpl")
	if err != nil {
		return nil, err
	}

	entries := createNginxEntries(update)

	var output bytes.Buffer
	err = tmpl.Execute(&output, loadBalancerTemplate{Conf: lb.Conf, Entries: entries})

	if err != nil {
		return []byte{}, fmt.Errorf("unable to create nginx config from template: %v", err)
	}

	return output.Bytes(), nil
}

type pathSet map[string]struct{}

func createNginxEntries(update controller.IngressUpdate) []*nginxEntry {
	sortedIngressEntries := update.SortedByName().Entries
	hostToNginxEntry := make(map[string]*nginxEntry)
	hostToPaths := make(map[string]pathSet)
	var nginxEntries []*nginxEntry
	var upstreamIndex int

	for _, ingressEntry := range sortedIngressEntries {
		nginxPath := createNginxPath(ingressEntry.Path)
		upstream := upstream{
			ID:     fmt.Sprintf("upstream%03d", upstreamIndex),
			Server: fmt.Sprintf("%s:%d", ingressEntry.ServiceAddress, ingressEntry.ServicePort),
		}
		location := location{
			Path:                    nginxPath,
			UpstreamID:              upstream.ID,
			Allow:                   ingressEntry.Allow,
			StripPath:               ingressEntry.StripPaths,
			BackendKeepaliveSeconds: ingressEntry.BackendKeepAliveSeconds,
		}

		entry, exists := hostToNginxEntry[ingressEntry.Host]
		if !exists {
			entry = &nginxEntry{ServerName: ingressEntry.Host}
			hostToNginxEntry[ingressEntry.Host] = entry
			nginxEntries = append(nginxEntries, entry)
			hostToPaths[ingressEntry.Host] = make(map[string]struct{})
		}

		paths := hostToPaths[ingressEntry.Host]
		if _, exists := paths[location.Path]; exists {
			log.Infof("Ignoring '%s' because it duplicates the host/path of a previous entry", ingressEntry.Name)
			continue
		}
		paths[location.Path] = struct{}{}

		entry.Name += " " + ingressEntry.Name
		entry.Upstreams = append(entry.Upstreams, upstream)
		entry.Locations = append(entry.Locations, location)
		upstreamIndex++
	}

	return nginxEntries
}

func createNginxPath(rawPath string) string {
	nginxPath := strings.TrimSuffix(strings.TrimPrefix(rawPath, "/"), "/")
	if len(nginxPath) == 0 {
		nginxPath = "/"
	} else {
		nginxPath = fmt.Sprintf("/%s/", nginxPath)
	}
	return nginxPath
}

func (lb *nginxLoadBalancer) Health() error {
	if !lb.running.Get() {
		return errors.New("nginx is not running")
	}
	if lb.metricsUnhealthy.Get() {
		return errors.New("nginx metrics are failing to update")
	}
	return nil
}

func (lb *nginxLoadBalancer) String() string {
	return "nginx proxy"
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
