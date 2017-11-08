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

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/go-multierror"
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
	log.Infof("Gorb server url: %s, drainDelay: %v, instance ip adddress: %s, vipLoadbalancer: %s ", c.ServerBaseURL, c.DrainDelay, c.InstanceIP, c.VipLoadbalancer)

	backendDefinitions := []backend{}

	var backendDefinition backend
	for _, service := range c.ServicesDefinition {
		args := PulseArgs{
			Method: "GET",
			Path:   "/",
			Expect: "404",
		}
		pulse := Pulse{
			Args:            args,
			TypeHealthcheck: "http",
			Interval:        c.BackendHealthcheckInterval,
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

	var httpClient = &http.Client{
		Timeout: time.Second * 5,
	}

	return &gorb{
		config:     c,
		backend:    backendDefinitions,
		httpClient: httpClient,
	}, nil
}

type gorb struct {
	config     *Config
	httpClient *http.Client
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

	if resp.StatusCode != 200 {
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

	switch action {
	// disable ARP for the loopback interface - see http://kb.linuxvirtualserver.org/wiki/Using_arp_announce/arp_ignore_to_disable_ARP
	case addLoopback:
		arpIgnore = 1
		arpAnnounce = 2
	case deleteLoopback:
		arpIgnore = 0
		arpAnnounce = 0
	default:
		return fmt.Errorf("unsupported loopback action %s", action)
	}

	var errorArr *multierror.Error
	vipLoadbalancerArr := strings.Split(g.config.VipLoadbalancer, ",")
	for index, vip := range vipLoadbalancerArr {
		log.Infof(fmt.Sprintf("VIP %s loopback: %s", action, vip))
		cmd := fmt.Sprintf("ip addr %s %s/32 dev lo label lo:%d", action, vip, index)
		_, errCmd := exec.Command("bash", "-c", cmd).Output()
		errorArr = multierror.Append(errorArr, errCmd)

		log.WithFields(log.Fields{"arp_ignore": arpIgnore}).Info("Set arp_ignore to: ")
		cmd = fmt.Sprintf("echo %d > %s", arpIgnore, path.Join(g.config.InterfaceProcFsPath, "arp_ignore"))
		_, errCmd = exec.Command("bash", "-c", cmd).Output()
		errorArr = multierror.Append(errorArr, errCmd)

		log.WithFields(log.Fields{"arp_announce": arpAnnounce}).Info("Set arp_announce to: ")
		cmd = fmt.Sprintf("echo %d > %s", arpAnnounce, path.Join(g.config.InterfaceProcFsPath, "arp_announce"))
		_, errCmd = exec.Command("bash", "-c", cmd).Output()
		errorArr = multierror.Append(errorArr, errCmd)
	}

	return errorArr.ErrorOrNil()
}

func (g *gorb) backendNotFound(backend *backend) (bool, error) {
	resp, err := g.httpClient.Get(g.serviceRequest(backend))
	if err != nil {
		return false, fmt.Errorf("unable to retrieve backend details for instance ip: %s error :%v", g.config.InstanceIP, err)
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
