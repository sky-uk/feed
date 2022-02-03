package nginx

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/util"
)

const (
	nginxStartDelay                         = time.Millisecond * 100
	metricsUpdateInterval                   = time.Second * 10
	defaultMaxRequestsPerUpstreamConnection = uint64(1024)
)

// Port configuration
type Port struct {
	Name string
	Port int
}

// Conf configuration for NGINX
type Conf struct {
	BinaryLocation               string
	WorkingDir                   string
	WorkerProcesses              int
	WorkerConnections            int
	WorkerShutdownTimeoutSeconds int
	KeepaliveSeconds             int
	BackendKeepalives            int
	BackendConnectTimeoutSeconds int
	ServerNamesHashBucketSize    int
	ServerNamesHashMaxSize       int
	HealthPort                   int
	TrustedFrontends             []string
	Ports                        []Port
	LogLevel                     string
	ProxyProtocol                bool
	AccessLog                    bool
	AccessLogDir                 string
	LogHeaders                   []string
	AccessLogHeaders             string
	UpdatePeriod                 time.Duration
	SSLPath                      string
	VhostStatsSharedMemory       int
	VhostStatsRequestBuckets     []string
	OpenTracingPlugin            string
	OpenTracingConfig            string
	HTTPConf
}

// HTTPConf configuration for http core module of nginx
type HTTPConf struct {
	ClientHeaderBufferSize        int
	ClientBodyBufferSize          int
	LargeClientHeaderBufferBlocks int
	NginxSetRealIPFromHeader      string
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
		incrementReloadMetric()
		_ = n.nginx.sighup()
		n.updateRequired.Set(false)
	}
}

// Nginx implementation
type nginxUpdater struct {
	Conf
	running                util.SafeBool
	lastErr                util.SafeError
	metricsUnhealthy       util.SafeBool
	nginxStarted           nginxStarted
	initialUpdateAttempted util.SafeBool
	doneCh                 chan struct{}
	nginx                  *nginx
	updateRequired         util.SafeBool
}

type nginxStarted struct {
	sync.Mutex
	done bool
}

// Used for generating nginx config
type loadBalancerTemplate struct {
	Conf
	Servers   []*server
	Upstreams []*upstream
}

type server struct {
	Name       string
	Names      []string
	ServerName string
	Locations  []*location
}

type upstream struct {
	ID                string
	Server            string
	MaxConnections    int
	KeepaliveTimeout  string
	KeepaliveRequests uint64
}

type location struct {
	Path                  string
	UpstreamID            string
	Allow                 []string
	StripPath             bool
	ExactPath             bool
	BackendTimeoutSeconds int
	ProxyBufferSize       int
	ProxyBufferBlocks     int
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
	_, err = n.updateNginxConf([]controller.IngressEntry{})
	return err
}

func (n *nginxUpdater) ensureNginxRunning() error {
	// Guard against the (hopefully unlikely) event that two updates could be
	// received concurrently and attempt to start Nginx twice.
	n.nginxStarted.Lock()
	defer n.nginxStarted.Unlock()

	if !n.nginxStarted.done {
		log.Info("Starting nginx for the first time")
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

		n.nginxStarted.done = true
	}

	return nil
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
	if n.running.Get() {
		log.Info("Shutting down nginx process")
		if err := n.nginx.sigquit(); err != nil {
			return fmt.Errorf("error shutting down nginx: %v", err)
		}
		<-n.doneCh
		return n.lastErr.Get()
	}

	return nil
}

// Update is called by a single go routine from the controller
func (n *nginxUpdater) Update(entries controller.IngressEntries) error {

	// We don't expect 0 entries so this will protect us against http 404s
	if len(entries) == 0 {
		return errors.New("nginx update has been called with 0 entries")
	}

	// Create new config
	hasChanged, err := n.updateNginxConf(entries)
	if err != nil {
		return fmt.Errorf("unable to update nginx config: %v", err)
	}

	// This will start Nginx if it's the first call to Update
	if nginxStartErr := n.ensureNginxRunning(); nginxStartErr != nil {
		return nginxStartErr
	}

	// If Nginx is already running and there are changes then reload the config
	if n.initialUpdateAttempted.Get() && hasChanged {
		n.signalRequired()
	}

	n.initialUpdateAttempted.Set(true)

	return nil
}

func (n *nginxUpdater) updateNginxConf(entries controller.IngressEntries) (bool, error) {
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

func (n *nginxUpdater) createConfig(entries controller.IngressEntries) ([]byte, error) {
	tmpl, err := template.New("nginx.tmpl").ParseFiles(n.WorkingDir + "/nginx.tmpl")
	if err != nil {
		return nil, err
	}

	serverEntries := createServerEntries(entries)
	upstreamEntries := createUpstreamEntries(entries)

	n.AccessLogHeaders = n.getNginxLogHeaders()
	var output bytes.Buffer
	lbTemplate := loadBalancerTemplate{
		Conf:      n.Conf,
		Servers:   serverEntries,
		Upstreams: upstreamEntries,
	}
	err = tmpl.Execute(&output, lbTemplate)

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

func createUpstreamEntries(entries controller.IngressEntries) []*upstream {
	idToUpstream := make(map[string]*upstream)

	for _, ingressEntry := range entries {
		maxRequestsPerConnection := defaultMaxRequestsPerUpstreamConnection
		if ingressEntry.BackendMaxRequestsPerConnection != 0 {
			maxRequestsPerConnection = ingressEntry.BackendMaxRequestsPerConnection
		}
		keepaliveTimeout := ""
		if ingressEntry.BackendKeepaliveTimeout != 0 {
			keepaliveTimeout = fmt.Sprintf("%ds", uint64(ingressEntry.BackendKeepaliveTimeout.Seconds()))
		}
		upstream := &upstream{
			ID:                upstreamID(ingressEntry),
			Server:            fmt.Sprintf("%s:%d", ingressEntry.ServiceAddress, ingressEntry.ServicePort),
			MaxConnections:    ingressEntry.BackendMaxConnections,
			KeepaliveRequests: maxRequestsPerConnection,
			KeepaliveTimeout:  keepaliveTimeout,
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
	return fmt.Sprintf("%s.%s.%s.%d", e.Namespace, e.Name, e.ServiceAddress, e.ServicePort)
}

func (s server) HasRootLocation() bool {
	for i := range s.Locations {
		if s.Locations[i].Path == "/" {
			return true
		}
	}
	return false
}

type servers []*server

func (s servers) Len() int           { return len(s) }
func (s servers) Less(i, j int) bool { return s[i].ServerName < s[j].ServerName }
func (s servers) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

type locations []*location

func (l locations) Len() int           { return len(l) }
func (l locations) Less(i, j int) bool { return l[i].Path < l[j].Path }
func (l locations) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }

func createServerEntries(entries controller.IngressEntries) []*server {
	hostToNginxEntry := make(map[string]*server)

	for _, ingressEntry := range uniqueIngressEntries(entries) {
		serverEntry, exists := hostToNginxEntry[ingressEntry.Host]
		if !exists {
			serverEntry = &server{ServerName: ingressEntry.Host}
			hostToNginxEntry[ingressEntry.Host] = serverEntry
		}

		location := location{
			Path:                  ingressEntry.Path,
			UpstreamID:            upstreamID(ingressEntry),
			Allow:                 ingressEntry.Allow,
			StripPath:             ingressEntry.StripPaths,
			ExactPath:             ingressEntry.ExactPath,
			BackendTimeoutSeconds: ingressEntry.BackendTimeoutSeconds,
			ProxyBufferSize:       ingressEntry.ProxyBufferSize,
			ProxyBufferBlocks:     ingressEntry.ProxyBufferBlocks,
		}

		serverEntry.Names = append(serverEntry.Names, ingressEntry.NamespaceName())
		serverEntry.Locations = append(serverEntry.Locations, &location)
	}

	var serverEntries []*server
	for _, serverEntry := range hostToNginxEntry {
		sort.Strings(serverEntry.Names)
		serverEntry.Name = strings.Join(serverEntry.Names, " ")
		sort.Sort(locations(serverEntry.Locations))
		serverEntries = append(serverEntries, serverEntry)
	}
	sort.Sort(servers(serverEntries))

	return serverEntries
}

type ingressKey struct {
	Host, Path string
}

func uniqueIngressEntries(entries controller.IngressEntries) []controller.IngressEntry {
	sort.Slice(entries, func(i, j int) bool {
		iEntry := entries[i]
		jEntry := entries[j]
		iString := strings.Join([]string{iEntry.Namespace, iEntry.Name, iEntry.Host, iEntry.Path,
			iEntry.ServiceAddress, string(iEntry.ServicePort)}, ":")
		jString := strings.Join([]string{jEntry.Namespace, jEntry.Name, jEntry.Host, jEntry.Path,
			jEntry.ServiceAddress, string(jEntry.ServicePort)}, ":")
		return iString < jString
	})

	uniqueIngress := make(map[ingressKey]controller.IngressEntry)
	for _, ingressEntry := range entries {
		ingressEntry.Path = createNginxPath(ingressEntry.Path, ingressEntry.ExactPath)
		key := ingressKey{ingressEntry.Host, ingressEntry.Path}
		existingIngressEntry, exists := uniqueIngress[key]
		if !exists {
			uniqueIngress[key] = ingressEntry
			continue
		}
		log.Infof("Ignoring '%s' because it duplicates the host/path of '%s'", ingressEntry, existingIngressEntry)
	}

	uniqueIngressEntries := make([]controller.IngressEntry, 0, len(uniqueIngress))
	for _, value := range uniqueIngress {
		uniqueIngressEntries = append(uniqueIngressEntries, value)
	}

	return uniqueIngressEntries
}

func createNginxPath(rawPath string, exactPath bool) string {
	if exactPath {
		return rawPath
	}

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
	if !n.initialUpdateAttempted.Get() {
		return errors.New("waiting for initial update")
	}
	if n.metricsUnhealthy.Get() {
		return errors.New("nginx metrics are failing to update")
	}
	return nil
}

func (n *nginxUpdater) Readiness() error {
	return n.Health()
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

	_, _ = f1.Write(b1)
	_, _ = f2.Write(b2)

	data, err := exec.Command("diff", "-u", f1.Name(), f2.Name()).CombinedOutput()
	if len(data) > 0 {
		return data, nil
	}
	return data, err
}
