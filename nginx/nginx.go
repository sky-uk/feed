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
	AccessLog                    bool
	AccessLogDir                 string
	LogHeaders                   []string
	AccessLogHeaders             string
	UpdatePeriod                 time.Duration
}

type nginx struct {
	*exec.Cmd
}

// Sigquit sends a SIGQUIT to the process
func (n *nginx) sigquit() error {
	p := n.Process
	log.Debugf("Sending SIGQUIT to %d", p.Pid)
	return p.Signal(syscall.SIGQUIT)
}

// Sighup sends a SIGHUP to the process
func (n *nginx) sighup() error {
	p := n.Process
	log.Debugf("Sending SIGHUP to %d", p.Pid)
	return p.Signal(syscall.SIGHUP)
}

func (n *nginxUpdater) signalRequired() {
	n.updateRequired.Set(true)
}

func (n *nginxUpdater) signalIfRequired() {
	if n.updateRequired.Get() {
		log.Info("Signalling Nginx to reload configuration")
		n.nginx.sighup()
		n.updateRequired.Set(false)
	}
}

// Nginx implementation
type nginxUpdater struct {
	Conf
	running              util.SafeBool
	lastErr              util.SafeError
	metricsUnhealthy     util.SafeBool
	initialUpdateApplied util.SafeBool
	doneCh               chan struct{}
	nginx                *nginx
	updateRequired       util.SafeBool
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

func (c *Conf) nginxConfFile() string {
	return c.WorkingDir + "/nginx.conf"
}

// New creates an nginx updater.
func New(nginxConf Conf) controller.Updater {
	initMetrics()

	nginxConf.WorkingDir = strings.TrimSuffix(nginxConf.WorkingDir, "/")
	if nginxConf.LogLevel == "" {
		nginxConf.LogLevel = "warn"
	}

	cmd := exec.Command(nginxConf.BinaryLocation, "-c", nginxConf.nginxConfFile())
	cmd.Stdout = log.StandardLogger().Writer()
	cmd.Stderr = log.StandardLogger().Writer()
	cmd.Stdin = os.Stdin

	updater := &nginxUpdater{
		Conf:   nginxConf,
		doneCh: make(chan struct{}),
		nginx:  &nginx{Cmd: cmd},
	}

	return updater
}

func (n *nginxUpdater) Start() error {
	if err := n.logNginxVersion(); err != nil {
		return err
	}

	if err := n.initialiseNginxConf(); err != nil {
		return fmt.Errorf("unable to initialise nginx config: %v", err)
	}

	if err := n.nginx.Start(); err != nil {
		return fmt.Errorf("unable to start nginx: %v", err)
	}

	n.running.Set(true)
	go n.waitForNginxToFinish()

	time.Sleep(nginxStartDelay)
	if !n.running.Get() {
		return errors.New("nginx died shortly after starting")
	}

	go n.periodicallyUpdateMetrics()
	go n.backgroundSignaller()

	return nil
}

func (n *nginxUpdater) logNginxVersion() error {
	cmd := exec.Command(n.BinaryLocation, "-v")
	cmd.Stdout = log.StandardLogger().Writer()
	cmd.Stderr = log.StandardLogger().Writer()
	return cmd.Run()
}

func (n *nginxUpdater) initialiseNginxConf() error {
	err := os.Remove(n.nginxConfFile())
	if err != nil {
		log.Debugf("Can't remove nginx.conf: %v", err)
	}
	_, err = n.update(controller.IngressUpdate{Entries: []controller.IngressEntry{}})
	return err
}

func (n *nginxUpdater) waitForNginxToFinish() {
	err := n.nginx.Wait()
	if err != nil {
		log.Error("Nginx has exited with an error: ", err)
	} else {
		log.Info("Nginx has shutdown successfully")
	}
	n.running.Set(false)
	n.lastErr.Set(err)
	close(n.doneCh)
}

func (n *nginxUpdater) periodicallyUpdateMetrics() {
	n.updateMetrics()
	ticker := time.NewTicker(metricsUpdateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-n.doneCh:
			return
		case <-ticker.C:
			n.updateMetrics()
		}
	}
}

func (n *nginxUpdater) backgroundSignaller() {
	log.Debugf("Nginx reload will check for updates every %v", n.UpdatePeriod)
	throttle := time.NewTicker(n.UpdatePeriod)
	defer throttle.Stop()
	for {
		select {
		case <-n.doneCh:
			log.Info("Signalling shut down")
			return
		case <-throttle.C:
			n.signalIfRequired()
		}
	}
}

func (n *nginxUpdater) updateMetrics() {
	if err := parseAndSetNginxMetrics(n.HealthPort, "/basic_status"); err != nil {
		log.Warnf("Unable to update nginx metrics: %v", err)
		n.metricsUnhealthy.Set(true)
	} else {
		n.metricsUnhealthy.Set(false)
	}
}

func (n *nginxUpdater) Stop() error {
	log.Info("Shutting down nginx process")
	if err := n.nginx.sigquit(); err != nil {
		return fmt.Errorf("error shutting down nginx: %v", err)
	}
	<-n.doneCh
	return n.lastErr.Get()
}

// This is called by a single go routine from the controller
func (n *nginxUpdater) Update(entries controller.IngressUpdate) error {
	updated, err := n.update(entries)
	if err != nil {
		return fmt.Errorf("unable to update nginx: %v", err)
	}

	if updated {
		if !n.initialUpdateApplied.Get() {
			log.Info("Initial update. Signalling nginx synchronously.")
			n.nginx.sighup()
			n.initialUpdateApplied.Set(true)
		} else {
			n.signalRequired()
		}
	}

	return nil
}

func (n *nginxUpdater) update(entries controller.IngressUpdate) (bool, error) {
	updatedConfig, err := n.createConfig(entries)
	if err != nil {
		return false, err
	}

	existingConfig, err := ioutil.ReadFile(n.nginxConfFile())
	if err != nil {
		log.Debugf("Error trying to read nginx.conf: %v", err)
		log.Info("Creating nginx.conf for the first time")
		return writeFile(n.nginxConfFile(), updatedConfig)
	}

	return n.diffAndUpdate(existingConfig, updatedConfig)
}

func (n *nginxUpdater) diffAndUpdate(existing, updated []byte) (bool, error) {
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
	_, err = writeFile(n.nginxConfFile(), updated)

	if err != nil {
		log.Errorf("Unable to write nginx configuration: %v", err)
		return false, err
	}

	err = n.checkNginxConfig()
	if err != nil {
		return false, err
	}

	return true, nil
}

func (n *nginxUpdater) checkNginxConfig() error {
	cmd := exec.Command(n.BinaryLocation, "-t", "-c", n.nginxConfFile())
	var out bytes.Buffer
	cmd.Stderr = &out
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("invalid config: %v: %s", err, out.String())
	}
	return nil
}

func (n *nginxUpdater) createConfig(update controller.IngressUpdate) ([]byte, error) {
	tmpl, err := template.New("nginx.tmpl").ParseFiles(n.WorkingDir + "/nginx.tmpl")
	if err != nil {
		return nil, err
	}

	entries := createNginxEntries(update)
	n.AccessLogHeaders = n.getNginxLogHeaders()
	var output bytes.Buffer
	err = tmpl.Execute(&output, loadBalancerTemplate{Conf: n.Conf, Entries: entries})

	if err != nil {
		return []byte{}, fmt.Errorf("unable to create nginx config from template: %v", err)
	}

	return output.Bytes(), nil
}

func (n *nginxUpdater) getNginxLogHeaders() string {
	headersString := ""
	for _, nginxLogHeader := range n.LogHeaders {
		headersString = headersString + " " + nginxLogHeader + "=$http_" + strings.Replace(nginxLogHeader, "-", "_", -1)
	}

	return headersString
}

type pathSet map[string]struct{}

func createNginxEntries(update controller.IngressUpdate) []*nginxEntry {
	sortedIngressEntries := update.SortedByNamespaceName().Entries
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

		ngxEntry, exists := hostToNginxEntry[ingressEntry.Host]
		if !exists {
			ngxEntry = &nginxEntry{ServerName: ingressEntry.Host}
			hostToNginxEntry[ingressEntry.Host] = ngxEntry
			nginxEntries = append(nginxEntries, ngxEntry)
			hostToPaths[ingressEntry.Host] = make(map[string]struct{})
		}

		paths := hostToPaths[ingressEntry.Host]
		if _, exists := paths[location.Path]; exists {
			log.Infof("Ignoring '%s' because it duplicates the host/path of a previous entry", ingressEntry.NamespaceName())
			continue
		}
		paths[location.Path] = struct{}{}

		ngxEntry.Name += " " + ingressEntry.NamespaceName()
		ngxEntry.Upstreams = append(ngxEntry.Upstreams, upstream)
		ngxEntry.Locations = append(ngxEntry.Locations, location)
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

func (n *nginxUpdater) Health() error {
	if !n.running.Get() {
		return errors.New("nginx is not running")
	}
	if !n.initialUpdateApplied.Get() {
		return errors.New("waiting for initial update")
	}
	if n.metricsUnhealthy.Get() {
		return errors.New("nginx metrics are failing to update")
	}
	return nil
}

func (n *nginxUpdater) String() string {
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
