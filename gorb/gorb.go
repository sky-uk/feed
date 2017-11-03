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

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/go-multierror"
	"github.com/sky-uk/feed/controller"
)

// ArgPulse define healthcheck path
type ArgPulse struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Expect string `json:"expect"`
}

// Pulse define healthcheck
type Pulse struct {
	TypeHealthcheck string   `json:"type"`
	Args            ArgPulse `json:"args"`
	Interval        string   `json:"interval"`
}

// BackendConf define the backend configuration
type BackendConf struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Method string `json:"method"`
	Weight int    `json:"weight"`
	Pulse  Pulse  `json:"pulse"`
}

// Backend define the backend configuration + service name
type Backend struct {
	ServiceName string
	BackendConf BackendConf
}

// New creates a gorb handler
func New(serverBaseURL string, instanceIP string, drainDelay time.Duration, servicesName string, servicesPort string, backendWeight int, backendMethod string, vipLoadbalancer string, manageLoopback bool) (controller.Updater, error) {
	if serverBaseURL == "" {
		return nil, errors.New("unable to create Gorb updater: missing server ip address")
	}
	initMetrics()
	log.Infof("Gorb server url: %s, drainDelay: %v, instance ip adddress: %s, vipLoadbalancer: %s ", serverBaseURL, drainDelay, instanceIP, vipLoadbalancer)

	backendDefinition := []Backend{}
	servicesNameArr := strings.Split(servicesName, ",")
	servicesPortArr := strings.Split(servicesPort, ",")

	if len(servicesNameArr) != len(servicesPortArr) {
		return nil, errors.New("Unable to create Gorb updater: the number of serviceName and port are not the same")
	}

	var backend Backend
	for index, service := range servicesNameArr {

		port, err := strconv.Atoi(servicesPortArr[index])
		if err != nil {
			return nil, errors.New("Unable to convert port form string to int")
		}

		args := ArgPulse{
			Method: "GET",
			Path:   "/",
			Expect: "404",
		}
		pulse := Pulse{
			Args:            args,
			TypeHealthcheck: "http",
			Interval:        "1s",
		}
		backend = Backend{
			ServiceName: service,
			BackendConf: BackendConf{
				Host:   instanceIP,
				Port:   port,
				Method: backendMethod,
				Weight: backendWeight,
				Pulse:  pulse,
			},
		}

		backendDefinition = append(backendDefinition, backend)
	}

	tr := &http.Transport{
		MaxIdleConns:    10,
		IdleConnTimeout: 30 * time.Second,
	}
	var httpClient = &http.Client{
		Transport: tr,
		Timeout:   time.Second * 5,
	}

	return &gorb{
		serverBaseURL:   serverBaseURL,
		drainDelay:      drainDelay,
		instanceIP:      instanceIP,
		vipLoadbalancer: vipLoadbalancer,
		backend:         backendDefinition,
		httpClient:      httpClient,
		manageLoopback:  manageLoopback,
	}, nil
}

type gorb struct {
	serverBaseURL   string
	drainDelay      time.Duration
	instanceIP      string
	vipLoadbalancer string
	httpClient      *http.Client
	backend         []Backend
	manageLoopback  bool
}

func (g *gorb) Start() error {
	return nil
}

func (g *gorb) ManageLoopBack(action string, arpIgnore int, arpAnnounce int) error {

	var errorArr *multierror.Error
	vipLoadbalancerArr := strings.Split(g.vipLoadbalancer, ",")
	for index, vip := range vipLoadbalancerArr {
		log.Infof(fmt.Sprintf("VIP %s loopback: %s", action, vip))
		cmd := fmt.Sprintf("ip addr %s %s/32 dev lo label lo:%d", action, vip, index)
		_, errCmd := exec.Command("bash", "-c", cmd).Output()
		errorArr = multierror.Append(errorArr, errCmd)

		log.WithFields(log.Fields{"arp_ignore": arpIgnore}).Info("Set back arp_ignore")
		cmd = fmt.Sprintf("echo %d > /host-ipv4-proc/arp_ignore", arpIgnore)
		_, errCmd = exec.Command("bash", "-c", cmd).Output()
		errorArr = multierror.Append(errorArr, errCmd)

		log.WithFields(log.Fields{"arp_announce": arpAnnounce}).Info("Set back arp_announce")
		cmd = fmt.Sprintf("echo %d > /host-ipv4-proc/arp_announce", arpAnnounce)
		_, errCmd = exec.Command("bash", "-c", cmd).Output()
		errorArr = multierror.Append(errorArr, errCmd)
	}

	return errorArr.ErrorOrNil()
}

// Stop removes this instance from Gorb
func (g *gorb) Stop() error {

	var errorArr *multierror.Error
	for _, backend := range g.backend {
		backend.BackendConf.Weight = 0
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
		err := g.ManageLoopBack("del", 0, 0)
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
		return fmt.Errorf("Unable to check service details: %v", err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("Gorb server is not healthy. Status code: %d", resp.StatusCode)
	}

	return nil
}

func (g *gorb) Update(controller.IngressEntries) error {

	var errorArr *multierror.Error

	if g.manageLoopback {
		err := g.ManageLoopBack("add", 1, 2)
		errorArr = multierror.Append(errorArr, err)
	}

	for _, backend := range g.backend {

		backendExists, err := g.backendExists(&backend)
		if err != nil {
			log.WithFields(log.Fields{"err": err}).Error("Unable to check if backend already exists")
		}

		if backendExists == 404 {
			err := g.addBackend(&backend)
			if err != nil {
				log.WithFields(log.Fields{"err": err}).Error("Error add Backend ")
				errorArr = multierror.Append(errorArr, err)
			} else {
				log.WithFields(log.Fields{"backend": backend}).Infof("Backend added successfully")
			}
		}
	}

	return errorArr.ErrorOrNil()
}

func (g *gorb) backendExists(backend *Backend) (int, error) {
	resp, err := g.httpClient.Get(fmt.Sprintf("%s/service/%s/node-%s-%s", g.serverBaseURL, backend.ServiceName, backend.ServiceName, g.instanceIP))
	if err != nil {
		return 0, fmt.Errorf("Unable to retrieve backend details for instance ip: %s error :%v", g.instanceIP, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func (g *gorb) addBackend(backend *Backend) error {
	payload, err := json.Marshal(backend.BackendConf)

	req, err := http.NewRequest("PUT", fmt.Sprintf("%s/service/%s/node-%s-%s", g.serverBaseURL, backend.ServiceName, backend.ServiceName, g.instanceIP), bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("Error while creating add backend request: %v", err)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Error while adding backend: %v, error: %v", backend, err)
	}
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		log.WithFields(log.Fields{"StatusCode": resp.StatusCode}).Infof("Error PUT add backend")
		return fmt.Errorf("Failed to add backend: %v, status code: %d, response: %v", backend, resp.StatusCode, body)
	}
	return nil
}

func (g *gorb) modifyBackend(backend *Backend) error {
	payload, err := json.Marshal(backend.BackendConf)

	req, err := http.NewRequest("PATCH", fmt.Sprintf("%s/service/%s/node-%s-%s", g.serverBaseURL, backend.ServiceName, backend.ServiceName, g.instanceIP), bytes.NewBuffer(payload))

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Error while modifying backend: %v, error: %v", backend, err)
	}
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		log.WithFields(log.Fields{"StatusCode": resp.StatusCode}).Infof("Error PATCH modification backend")
		return fmt.Errorf("Failed to modify backend: %v, status code: %d, response: %v", backend, resp.StatusCode, body)
	}
	return nil
}

func (g *gorb) removeBackend(backend *Backend) error {

	req, err := http.NewRequest("DELETE", fmt.Sprintf("%s/service/%s/node-%s-%s", g.serverBaseURL, backend.ServiceName, backend.ServiceName, g.instanceIP), nil)
	if err != nil {
		return fmt.Errorf("Error while creating add backend request: %v", err)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Error while removing backend, error: %v", err)
	}
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		log.WithFields(log.Fields{"StatusCode": resp.StatusCode}).Infof("Error removing backend")
		return fmt.Errorf("Failed to remove backend, status code: %d, response: %v", resp.StatusCode, body)
	}
	return nil
}

func (g *gorb) String() string {
	return "Gorb frontend"
}
