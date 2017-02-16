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

	"sort"

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
	BinaryLocation             string
	WorkingDir                 string
	WorkerProcesses            int
	WorkerConnections          int
	KeepaliveSeconds           int
	UpstreamKeepalives         int
	ProxyConnectTimeoutSeconds int
	UpstreamMaxFails           int
	UpstreamFailTimeoutSeconds int
	ProxyNextUpstreamErrors    []string
	ServerNamesHashBucketSize  int
	ServerNamesHashMaxSize     int
	HealthPort                 int
	TrustedFrontends           []string
	IngressPort                int
	LogLevel                   string
	ProxyProtocol              bool
	AccessLog                  bool
	AccessLogDir               string
	LogHeaders                 []string
	AccessLogHeaders           string
	UpdatePeriod               time.Duration
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
	Servers   []*server
	Upstreams []*upstream
}

type server struct {
	Name       string
	ServerName string
	Locations  []*location
}

type upstream struct {
	ID        string
	Port      int32
	Addresses []string
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

	if len(nginxConf.ProxyNextUpstreamErrors) == 0 {
		log.Fatal("ProxyNextUpstreamErrors was empty, should contain at least one value.")
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
	if err := parseAndSetNginxMetrics(n.HealthPort); err != nil {
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
			log.Info("Loading nginx configuration for the first time.")
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

	log.Debugf("Updating nginx config: %s", string(diffOutput))
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

	serverEntries := createServerEntries(update)
	upstreamEntries := createUpstreamEntries(update)

	n.AccessLogHeaders = n.getNginxLogHeaders()
	var output bytes.Buffer
	template := loadBalancerTemplate{
		Conf:      n.Conf,
		Servers:   serverEntries,
		Upstreams: upstreamEntries,
	}
	err = tmpl.Execute(&output, template)

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

type upstreams []*upstream

func (u upstreams) Len() int           { return len(u) }
func (u upstreams) Less(i, j int) bool { return u[i].ID < u[j].ID }
func (u upstreams) Swap(i, j int)      { u[i], u[j] = u[j], u[i] }

func createUpstreamEntries(update controller.IngressUpdate) []*upstream {
	idToUpstream := make(map[string]*upstream)

	for _, entry := range update.Entries {
		upstream := &upstream{
			ID:        upstreamID(entry),
			Port:      entry.Service.Port,
			Addresses: entry.Service.Addresses,
		}
		idToUpstream[upstream.ID] = upstream
	}

	var sortedUpstreams []*upstream
	for _, upstream := range idToUpstream {
		sortedUpstreams = append(sortedUpstreams, upstream)
	}

	sort.Sort(upstreams(sortedUpstreams))
	return sortedUpstreams
}

func upstreamID(e controller.IngressEntry) string {
	return fmt.Sprintf("%s.%s.%d", e.Namespace, e.Service.Name, e.Service.Port)
}

type servers []*server

func (s servers) Len() int           { return len(s) }
func (s servers) Less(i, j int) bool { return s[i].ServerName < s[j].ServerName }
func (s servers) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

type locations []*location

func (l locations) Len() int           { return len(l) }
func (l locations) Less(i, j int) bool { return l[i].Path < l[j].Path }
func (l locations) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }

type pathSet map[string]bool

func createServerEntries(update controller.IngressUpdate) []*server {
	hostToNginxEntry := make(map[string]*server)
	hostToPaths := make(map[string]pathSet)

	for _, ingressEntry := range update.Entries {
		serverEntry, exists := hostToNginxEntry[ingressEntry.Host]
		if !exists {
			serverEntry = &server{ServerName: ingressEntry.Host}
			hostToNginxEntry[ingressEntry.Host] = serverEntry
			hostToPaths[ingressEntry.Host] = make(map[string]bool)
		}

		nginxPath := createNginxPath(ingressEntry.Path)
		location := location{
			Path:                    nginxPath,
			UpstreamID:              upstreamID(ingressEntry),
			Allow:                   ingressEntry.Allow,
			StripPath:               ingressEntry.StripPaths,
			BackendKeepaliveSeconds: ingressEntry.BackendKeepAliveSeconds,
		}

		paths := hostToPaths[ingressEntry.Host]
		if paths[location.Path] {
			log.Infof("Ignoring '%s' because it duplicates the host/path of a previous entry", ingressEntry.NamespaceName())
			continue
		}
		paths[location.Path] = true

		serverEntry.Name += " " + ingressEntry.NamespaceName()
		serverEntry.Locations = append(serverEntry.Locations, &location)
	}

	var serverEntries []*server
	for _, serverEntry := range hostToNginxEntry {
		sort.Sort(locations(serverEntry.Locations))
		serverEntries = append(serverEntries, serverEntry)
	}
	sort.Sort(servers(serverEntries))

	return serverEntries
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
