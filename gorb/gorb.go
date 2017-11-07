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
	"strconv"
	"strings"

	"io"

	"path"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/go-multierror"
	"github.com/sky-uk/feed/controller"
)

// HealthcheckArgs defines health check URI
type HealthcheckArgs struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Expect string `json:"expect"`
}

// Pulse defines backend health check
type Pulse struct {
	TypeHealthcheck string          `json:"type"`
	Args            HealthcheckArgs `json:"args"`
	Interval        string          `json:"interval"`
}

// BackendConfig defines the backend configuration
type BackendConfig struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Method string `json:"method"`
	Weight int    `json:"weight"`
	Pulse  Pulse  `json:"pulse"`
}

// Backend defines the backend configuration + service name
type backend struct {
	ServiceName   string
	BackendConfig BackendConfig
}

type loopbackAction string

const (
	addLoopback    loopbackAction = "add"
	deleteLoopback loopbackAction = "del"
)

// New creates a gorb handler
func New(serverBaseURL string, instanceIP string, drainDelay time.Duration, servicesDefinition string, backendWeight int, backendMethod string, vipLoadbalancer string, manageLoopback bool, backendHealthcheckInterval string, interfaceProcFsPath string) (controller.Updater, error) {
	if serverBaseURL == "" {
		return nil, errors.New("unable to create Gorb updater: missing server ip address")
	}
	initMetrics()
	log.Infof("Gorb server url: %s, drainDelay: %v, instance ip adddress: %s, vipLoadbalancer: %s ", serverBaseURL, drainDelay, instanceIP, vipLoadbalancer)

	backendDefinitions := []backend{}
	servicesDefinitionArr := strings.Split(servicesDefinition, ",")

	var backendDefinition backend
	for _, service := range servicesDefinitionArr {

		servicesArr := strings.Split(service, ":")
		port, err := strconv.Atoi(servicesArr[1])
		if err != nil {
			return nil, errors.New("Unable to convert port form string to int")
		}

		args := HealthcheckArgs{
			Method: "GET",
			Path:   "/",
			Expect: "404",
		}
		pulse := Pulse{
			Args:            args,
			TypeHealthcheck: "http",
			Interval:        backendHealthcheckInterval,
		}
		backendDefinition = backend{
			ServiceName: servicesArr[0],
			BackendConfig: BackendConfig{
				Host:   instanceIP,
				Port:   port,
				Method: backendMethod,
				Weight: backendWeight,
				Pulse:  pulse,
			},
		}

		backendDefinitions = append(backendDefinitions, backendDefinition)
	}

	var httpClient = &http.Client{
		Timeout: time.Second * 5,
	}

	return &gorb{
		serverBaseURL:       serverBaseURL,
		drainDelay:          drainDelay,
		instanceIP:          instanceIP,
		vipLoadbalancer:     vipLoadbalancer,
		backend:             backendDefinitions,
		httpClient:          httpClient,
		manageLoopback:      manageLoopback,
		interfaceProcFsPath: interfaceProcFsPath,
	}, nil
}

type gorb struct {
	serverBaseURL       string
	drainDelay          time.Duration
	instanceIP          string
	vipLoadbalancer     string
	httpClient          *http.Client
	backend             []backend
	manageLoopback      bool
	interfaceProcFsPath string
}

func (g *gorb) Start() error {
	return nil
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
	vipLoadbalancerArr := strings.Split(g.vipLoadbalancer, ",")
	for index, vip := range vipLoadbalancerArr {
		log.Infof(fmt.Sprintf("VIP %s loopback: %s", action, vip))
		cmd := fmt.Sprintf("ip addr %s %s/32 dev lo label lo:%d", action, vip, index)
		_, errCmd := exec.Command("bash", "-c", cmd).Output()
		errorArr = multierror.Append(errorArr, errCmd)

		log.WithFields(log.Fields{"arp_ignore": arpIgnore}).Info("Set arp_ignore to: ")
		cmd = fmt.Sprintf("echo %d > %s", arpIgnore, path.Join(g.interfaceProcFsPath, "arp_ignore"))
		_, errCmd = exec.Command("bash", "-c", cmd).Output()
		errorArr = multierror.Append(errorArr, errCmd)

		log.WithFields(log.Fields{"arp_announce": arpAnnounce}).Info("Set arp_announce to: ")
		cmd = fmt.Sprintf("echo %d > %s", arpAnnounce, path.Join(g.interfaceProcFsPath, "arp_announce"))
		_, errCmd = exec.Command("bash", "-c", cmd).Output()
		errorArr = multierror.Append(errorArr, errCmd)
	}

	return errorArr.ErrorOrNil()
}

// Stop removes this instance from Gorb
func (g *gorb) Stop() error {
	var errorArr *multierror.Error
	for _, backend := range g.backend {
		backend.BackendConfig.Weight = 0
		err := g.modifyBackend(&backend)
		if err != nil {
			log.WithFields(log.Fields{"err": err}).Error("Unable to set the backend weight to 0")
			errorArr = multierror.Append(errorArr, err)
		} else {
			log.WithFields(log.Fields{"backend": backend}).Infof("Set backend weight to 0")
		}
	}

	log.Infof("Waiting %v to finish gorb draining", g.drainDelay)
	time.Sleep(g.drainDelay)

	if g.manageLoopback {
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
	resp, err := g.httpClient.Get(fmt.Sprintf("%s/service", g.serverBaseURL))
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
	if g.manageLoopback {
		err := g.manageLoopBack(addLoopback)
		errorArr = multierror.Append(errorArr, err)
	}

	for _, backend := range g.backend {

		backendExists, err := g.backendExists(&backend)
		if err != nil {
			log.WithFields(log.Fields{"err": err, "backend": backend}).Error("Unable to check if backend already exists")
		}

		if backendExists == 404 {
			err := g.addBackend(&backend)
			if err != nil {
				log.WithFields(log.Fields{"err": err, "backend": backend}).Error("Error add Backend ")
				errorArr = multierror.Append(errorArr, err)
			} else {
				log.WithFields(log.Fields{"backend": backend}).Infof("Backend added successfully")
			}
		}
	}

	return errorArr.ErrorOrNil()
}

func (g *gorb) backendExists(backend *backend) (int, error) {
	resp, err := g.httpClient.Get(g.serviceRequest(backend))
	if err != nil {
		return 0, fmt.Errorf("unable to retrieve backend details for instance ip: %s error :%v", g.instanceIP, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func (g *gorb) addBackend(backend *backend) error {
	payload, err := json.Marshal(backend.BackendConfig)
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
	payload, err := json.Marshal(backend.BackendConfig)
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
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		log.WithFields(log.Fields{"StatusCode": resp.StatusCode}).Infof("Error while sending %s backend request", method)
		return fmt.Errorf("failed to send %s backend request, status code: %d, response: %v", method, resp.StatusCode, body)
	}
	return nil
}

func (g *gorb) serviceRequest(backend *backend) string {
	return fmt.Sprintf("%s/service/%s/node-%s-%s", g.serverBaseURL, backend.ServiceName, backend.ServiceName, g.instanceIP)
}

func (g *gorb) String() string {
	return "Gorb frontend"
}
