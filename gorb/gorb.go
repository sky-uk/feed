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
func New(serverBaseURL string, instanceIP string, drainDelay time.Duration, servicesName string, servicesPort string, backendWeight int, backendMethod string) (controller.Updater, error) {
	if serverBaseURL == "" {
		return nil, errors.New("unable to create Gorb updater: missing server ip address")
	}
	//initMetrics()
	log.Infof("Gorb server url: %s, drainDelay: %d, instance ip adddress: %s ", serverBaseURL, drainDelay, instanceIP)

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

	return &gorb{
		serverBaseURL: serverBaseURL,
		drainDelay:    drainDelay,
		instanceIP:    instanceIP,
		backend:       backendDefinition,
	}, nil
}

type gorb struct {
	serverBaseURL string
	drainDelay    time.Duration
	instanceIP    string
	backend       []Backend
}

func (g *gorb) Start() error {
	return nil
}

// Stop removes this instance from Gorb
func (g *gorb) Stop() error {

	var errorArr error
	for _, backend := range g.backend {
		backend.BackendConf.Weight = 0
		err := g.modifyBackend(&backend)
		if err != nil {
			log.Error("Unable to set the backend weight to 0", err)
			errorArr = multierror.Append(errorArr, err)
		} else {
			log.Infof("Set backend weight to 0", g.instanceIP)
		}
	}

	log.Infof("Waiting %v to finish gorb draining", g.drainDelay)
	time.Sleep(g.drainDelay)

	go func() {
		cmd := `while [[ $(curl -s -o /dev/null -w %{http_code} http://localhost:80) != '000' ]] ; do  sleep 0.25; echo 'Waiting ingress stopped '; done; ip addr del 10.154.0.10/32 dev lo label lo:0 ; echo 'VIP loopback deleted '; echo 0 > /host-ipv4-proc/arp_ignore; echo 0 > /host-ipv4-proc/arp_announce`
		outCmd, errCmd := exec.Command("nohup", "bash", "-c", cmd).Output()
		log.Infof("output deletion VIP: ", outCmd)
		log.Infof("err deletion VIP: ", errCmd)
	}()

	for _, backend := range g.backend {
		err := g.removeBackend(&backend)
		if err != nil {
			log.Error("Unable to remove Backend ", err)
			errorArr = multierror.Append(errorArr, err)
		} else {
			log.Infof("Backend succesfully removed", g.instanceIP)
		}
	}

	if errorArr != nil {
		return errorArr
	}

	return nil

}

func (g *gorb) Health() error {
	resp, err := http.Get(fmt.Sprintf("%s/service", g.serverBaseURL))
	if err != nil {
		return fmt.Errorf("Unable to check service details: %v", err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("Gorb server is not healthy. Status code: %d", resp.StatusCode)
	}

	return nil
}

func (g *gorb) Update(controller.IngressEntries) error {

	cmd := `while [[ $(curl -s -o /dev/null -w %{http_code} http://localhost:80) != '404' ]] ; do  sleep 0.25; echo 'Waiting ingres started ' ; done; echo 1 > /host-ipv4-proc/arp_ignore; echo 2 > /host-ipv4-proc/arp_announce; ip addr add 10.154.0.10/32 dev lo label lo:0; echo 'VIP loopback created'`
	outCmd, errCmd := exec.Command("nohup", "bash", "-c", cmd).Output()
	log.Infof("output creation VIP: ", outCmd)
	log.Infof("err creation VIP: ", errCmd)

	var errorArr error
	for _, backend := range g.backend {

		backendExists, err := g.backendExists(&backend)
		if err != nil {
			log.Error("Unable to check if backend already exists", err)
		} else {
			log.Info("Backend Already Exists: ", backendExists)
		}

		if backendExists == 404 {
			err := g.addBackend(&backend)
			if err != nil {
				log.Error("Error add Backend ", err)
				errorArr = multierror.Append(errorArr, err)
			} else {
				log.Infof("Backend added successfully", backend)
			}
		} else if backendExists == 200 {
			err := g.modifyBackend(&backend)
			if err != nil {
				log.Error("Error modifying Backend ", err)
				errorArr = multierror.Append(errorArr, err)
			}
		}
	}

	if errorArr != nil {
		return errorArr
	}

	return nil
}

func (g *gorb) backendExists(backend *Backend) (int, error) {
	resp, err := http.Get(fmt.Sprintf("%s/service/%s/node-%s-%s", g.serverBaseURL, backend.ServiceName, backend.ServiceName, g.instanceIP))
	if err != nil {
		return 0, fmt.Errorf("Unable to retrieve backend details for instance ip: %s error :%v", g.instanceIP, err)
	}
	return resp.StatusCode, nil
}

func (g *gorb) addBackend(backend *Backend) error {
	payload, err := json.Marshal(backend.BackendConf)

	req, err := http.NewRequest("PUT", fmt.Sprintf("%s/service/%s/node-%s-%s", g.serverBaseURL, backend.ServiceName, backend.ServiceName, g.instanceIP), bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("Error while creating add backend request: %v", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Error while adding backend: %v, error: %v", backend, err)
	}
	log.Infof("Error PUT add backend", resp.StatusCode)
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("Failed to add backend: %v, status code: %d, response: %v", backend, resp.StatusCode, body)
	}
	return nil
}

func (g *gorb) modifyBackend(backend *Backend) error {
	payload, err := json.Marshal(backend.BackendConf)

	req, err := http.NewRequest("PATCH", fmt.Sprintf("%s/service/%s/node-%s-%s", g.serverBaseURL, backend.ServiceName, backend.ServiceName, g.instanceIP), bytes.NewBuffer(payload))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Error while modifying backend: %v, error: %v", backend, err)
	}
	log.Infof("Error PATCH modification backend", resp.StatusCode)
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("Failed to modify backend: %v, status code: %d, response: %v", backend, resp.StatusCode, body)
	}
	return nil
}

func (g *gorb) removeBackend(backend *Backend) error {

	req, err := http.NewRequest("DELETE", fmt.Sprintf("%s/service/%s/node-%s-%s", g.serverBaseURL, backend.ServiceName, backend.ServiceName, g.instanceIP), nil)
	if err != nil {
		return fmt.Errorf("Error while creating add backend request: %v", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Error while removing backend, error: %v", err)
	}
	log.Infof("Error removing backend", resp.StatusCode)
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("Failed to remove backend, status code: %d, response: %v", resp.StatusCode, body)
	}
	return nil
}

func (g *gorb) String() string {
	return "Gorb frontend"
}
