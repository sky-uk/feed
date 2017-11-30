/*
Package gorb provide the ability to register and deregister the backend
*/
package gorb

import (
	"errors"

	"time"

	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os/exec"
	"strings"

	"io"

	"path"

	"strconv"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/go-multierror"
	"github.com/sethgrid/pester"
	"github.com/sky-uk/feed/controller"
)

// PulseArgs defines health check URI
type PulseArgs struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Expect string `json:"expect"`
}

// Pulse defines backend health check
type Pulse struct {
	TypeHealthcheck string    `json:"type"`
	Args            PulseArgs `json:"args"`
	Interval        string    `json:"interval"`
}

// BackendConfig defines the backend configuration
type BackendConfig struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Method string `json:"method"`
	Weight int    `json:"weight"`
	Pulse  Pulse  `json:"pulse"`
}

// VirtualService defines the virtual services
type VirtualService struct {
	Name string
	Port int
}

// Config defines all the configuration required for gorb
type Config struct {
	ServerBaseURL              string
	InstanceIP                 string
	DrainDelay                 time.Duration
	ServicesDefinition         []VirtualService
	BackendWeight              int
	BackendMethod              string
	VipLoadbalancer            string
	ManageLoopback             bool
	BackendHealthcheckInterval string
	BackendHealthcheckType     string
	InterfaceProcFsPath        string
}

// Backend defines the backend configuration + service name
type backend struct {
	serviceName   string
	backendConfig BackendConfig
}

type loopbackAction string

const (
	addLoopback    loopbackAction = "add"
	deleteLoopback loopbackAction = "del"
)

// New creates a gorb handler
func New(c *Config) (controller.Updater, error) {
	if c.ServerBaseURL == "" {
		return nil, errors.New("unable to create Gorb updater: missing server ip address")
	}
	initMetrics()
	log.Infof("Gorb server url: %s, drainDelay: %v, instance ip adddress: %s, vipLoadbalancer: %s", c.ServerBaseURL, c.DrainDelay, c.InstanceIP, c.VipLoadbalancer)

	backendDefinitions := []backend{}

	var backendDefinition backend
	for _, service := range c.ServicesDefinition {
		pulse := Pulse{
			TypeHealthcheck: c.BackendHealthcheckType,
			Interval:        c.BackendHealthcheckInterval,
		}
		if c.BackendHealthcheckType == "http" {
			pulse.Args = PulseArgs{
				Method: "GET",
				Path:   "/",
				Expect: "404",
			}
		}

		backendDefinition = backend{
			serviceName: service.Name,
			backendConfig: BackendConfig{
				Host:   c.InstanceIP,
				Port:   service.Port,
				Method: c.BackendMethod,
				Weight: c.BackendWeight,
				Pulse:  pulse,
			},
		}

		backendDefinitions = append(backendDefinitions, backendDefinition)
	}

	httpClient := pester.New()
	httpClient.Timeout = time.Second * 2
	httpClient.MaxRetries = 3

	return &gorb{
		command:    &SimpleCommandRunner{},
		config:     c,
		backend:    backendDefinitions,
		httpClient: httpClient,
	}, nil
}

// CommandRunner is a cut down version of exec.Cmd for running commands
type CommandRunner interface {
	Execute(cmd string) ([]byte, error)
}

// SimpleCommandRunner implements CommandRunner
type SimpleCommandRunner struct {
}

// Execute runs the given command and returns its output
func (c *SimpleCommandRunner) Execute(cmd string) ([]byte, error) {
	log.Infof(fmt.Sprintf("Executing cmd: %s", cmd))
	return exec.Command("bash", "-c", cmd).Output()
}

type gorb struct {
	command    CommandRunner
	config     *Config
	httpClient *pester.Client
	backend    []backend
}

func (g *gorb) Start() error {
	return nil
}

// Stop removes this instance from Gorb
func (g *gorb) Stop() error {
	var errorArr *multierror.Error
	for _, backend := range g.backend {
		backend.backendConfig.Weight = 0
		err := g.modifyBackend(&backend)
		if err != nil {
			log.WithFields(log.Fields{"err": err}).Error("Unable to set the backend weight to 0")
			errorArr = multierror.Append(errorArr, err)
		} else {
			log.WithFields(log.Fields{"backend": backend}).Infof("Set backend weight to 0")
		}
	}

	log.Infof("Waiting %v to finish gorb draining", g.config.DrainDelay)
	time.Sleep(g.config.DrainDelay)

	if g.config.ManageLoopback {
		err := g.manageLoopBack(deleteLoopback)
		errorArr = multierror.Append(errorArr, err)
	}

	for _, backend := range g.backend {
		err := g.removeBackend(&backend)
		if err != nil {
			log.WithFields(log.Fields{"err": err}).Error("Unable to remove Backend ")
			errorArr = multierror.Append(errorArr, err)
		} else {
			log.WithFields(log.Fields{"backend": backend}).Infof("Backend succesfully removed")
		}
	}

	return errorArr.ErrorOrNil()
}

func (g *gorb) Health() error {
	resp, err := g.httpClient.Get(fmt.Sprintf("%s/service", g.config.ServerBaseURL))
	if err != nil {
		return fmt.Errorf("unable to check service details: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gorb server is not healthy. Status code: %d, Response Body %s", resp.StatusCode, resp.Body)
	}

	return nil
}

func (g *gorb) Update(controller.IngressEntries) error {
	var errorArr *multierror.Error
	if g.config.ManageLoopback {
		err := g.manageLoopBack(addLoopback)
		errorArr = multierror.Append(errorArr, err)
	}

	for _, backend := range g.backend {
		backendNotFound, err := g.backendNotFound(&backend)
		if err != nil {
			log.WithFields(log.Fields{"err": err, "backend": backend}).Error("Unable to check if backend already exists")
		}

		if backendNotFound {
			err := g.addBackend(&backend)
			if err != nil {
				log.WithFields(log.Fields{"err": err, "backend": backend}).Error("Error adding backend ")
				errorArr = multierror.Append(errorArr, err)
			} else {
				log.WithFields(log.Fields{"backend": backend}).Infof("Backend added successfully")
				attachedFrontendGauge.Set(float64(1))
			}
		}
	}

	return errorArr.ErrorOrNil()
}

func (g *gorb) manageLoopBack(action loopbackAction) error {
	var arpIgnore int
	var arpAnnounce int
	var expectedVipCount int
	var interfaceAction string

	switch action {
	// disable ARP for the loopback interface - see http://kb.linuxvirtualserver.org/wiki/Using_arp_announce/arp_ignore_to_disable_ARP
	case addLoopback:
		arpIgnore = 1
		arpAnnounce = 2
		interfaceAction = "add"
		expectedVipCount = 0
	case deleteLoopback:
		arpIgnore = 0
		arpAnnounce = 0
		interfaceAction = "del"
		expectedVipCount = 1
	default:
		return fmt.Errorf("unsupported loopback action %s", action)
	}

	var errorArr *multierror.Error
	vipLoadbalancers := strings.Split(g.config.VipLoadbalancer, ",")
	for index, vip := range vipLoadbalancers {
		vipCount, err := g.loopbackInterfaceCount(fmt.Sprintf("lo:%d", index), vip)
		errorArr = multierror.Append(errorArr, err)
		if vipCount == expectedVipCount {
			_, err = g.command.Execute(fmt.Sprintf("sudo ip addr %s %s/32 dev lo label lo:%d", interfaceAction, vip, index))
			errorArr = multierror.Append(errorArr, err)
		}

		_, err = g.command.Execute(fmt.Sprintf("echo %d | sudo tee %s > /dev/null", arpIgnore, path.Join(g.config.InterfaceProcFsPath, "arp_ignore")))
		errorArr = multierror.Append(errorArr, err)

		_, err = g.command.Execute(fmt.Sprintf("echo %d | sudo tee %s > /dev/null", arpAnnounce, path.Join(g.config.InterfaceProcFsPath, "arp_announce")))
		errorArr = multierror.Append(errorArr, err)
	}

	return errorArr.ErrorOrNil()
}

func (g *gorb) loopbackInterfaceCount(label string, vip string) (int, error) {
	cmdOutput, err := g.command.Execute(fmt.Sprintf("sudo ip addr show label %s | grep -c %s/32 | xargs echo", label, vip))
	if err != nil {
		return -1, fmt.Errorf("unable to check whether loopback interface exists for label: %s and vip: %s, error %v", label, vip, err)
	}

	vipCount, err := strconv.Atoi(strings.TrimSpace(string(cmdOutput)))
	if err != nil {
		return -1, fmt.Errorf("unable to parse loopback interface count from the output: %s, error :%v", string(cmdOutput), err)
	}
	return vipCount, nil
}

func (g *gorb) backendNotFound(backend *backend) (bool, error) {
	resp, err := g.httpClient.Get(g.serviceRequest(backend))
	if err != nil {
		return false, fmt.Errorf("unable to retrieve backend details for instance ip: %s, error :%v", g.config.InstanceIP, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusNotFound, nil
}

func (g *gorb) addBackend(backend *backend) error {
	payload, err := json.Marshal(backend.backendConfig)
	if err != nil {
		return fmt.Errorf("error while marshalling backend: %v, error: %v", backend, err)
	}

	err = g.executeRequest("PUT", backend, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("error while adding backend: %v, error: %v", backend, err)
	}
	return nil
}

func (g *gorb) modifyBackend(backend *backend) error {
	payload, err := json.Marshal(backend.backendConfig)
	if err != nil {
		return fmt.Errorf("error while marshalling backend: %v, error: %v", backend, err)
	}

	err = g.executeRequest("PATCH", backend, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("error while modifying backend: %v, error: %v", backend, err)
	}
	return nil
}

func (g *gorb) removeBackend(backend *backend) error {
	err := g.executeRequest("DELETE", backend, nil)
	if err != nil {
		return fmt.Errorf("error while removing backend: %v, error: %v", backend, err)
	}
	return nil
}

func (g *gorb) executeRequest(method string, backend *backend, payload io.Reader) error {
	req, err := http.NewRequest(method, g.serviceRequest(backend), payload)
	if err != nil {
		return fmt.Errorf("error while creating %s request: %v", method, err)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error while sending %s backend request, error: %v", method, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		log.WithFields(log.Fields{"StatusCode": resp.StatusCode}).Infof("Error while sending %s backend request", method)
		return fmt.Errorf("failed to send %s backend request, status code: %d, response: %v", method, resp.StatusCode, body)
	}
	return nil
}

func (g *gorb) serviceRequest(backend *backend) string {
	return fmt.Sprintf("%s/service/%s/node-%s-%s", g.config.ServerBaseURL, backend.serviceName, backend.serviceName, g.config.InstanceIP)
}

func (g *gorb) String() string {
	return "Gorb frontend"
}
