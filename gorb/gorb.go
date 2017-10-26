/*
Package elb provides an updater for an ELB frontend to attach nginx to.
*/
package gorb

import (
	"errors"

	"time"

	"net/http"
	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"fmt"
	"encoding/json"
	"bytes"
	"io/ioutil"
)

type Backend struct {
	host string `json:"host"`
	port int `json:"port"`
	method string `json:"method"`
	weight int `json:"weight"`
}

func New(serverBaseUrl string, instanceIp string, drainDelay time.Duration) (controller.Updater, error) {
	if serverBaseUrl == "" {
		return nil, errors.New("unable to create Gorb updater: missing server ip address")
	}
	//initMetrics()
	log.Infof("Gorb server url: %s, drainDelay: %d, isntance ip adddress: %s ", serverBaseUrl, drainDelay, instanceIp)
	return &gorb{
		serverBaseUrl:     serverBaseUrl,
		drainDelay:        drainDelay,
		instanceIp:        instanceIp,
	}, nil
}

// LoadBalancerDetails stores all the elb information we use.
type LoadBalancerDetails struct {
	Name         string
	DNSName      string
	HostedZoneID string
	Scheme       string
}

type gorb struct {
	serverBaseUrl string
	drainDelay    time.Duration
	instanceIp    string
}

func (g *gorb) Start() error {
	return nil
}

// Stop removes this instance from all the front end ELBs
func (g *gorb) Stop() error {
	//var failed = false
	//for _, elb := range e.elbs {
	//	log.Infof("Deregistering instance %s with elb %s", e.instanceID, elb.Name)
	//	_, err := e.awsElb.DeregisterInstancesFromLoadBalancer(&aws_elb.DeregisterInstancesFromLoadBalancerInput{
	//		Instances:        []*aws_elb.Instance{{InstanceId: aws.String(e.instanceID)}},
	//		LoadBalancerName: aws.String(elb.Name),
	//	})
	//
	//	if err != nil {
	//		log.Warnf("unable to deregister instance %s with elb %s: %v", e.instanceID, elb.Name, err)
	//		failed = true
	//	}
	//}
	//if failed {
	//	return errors.New("at least one ELB failed to detach")
	//}
	//
	//log.Infof("Waiting %v to finish ELB deregistration", e.drainDelay)
	//time.Sleep(e.drainDelay)

	return nil
}

func (g *gorb) Health() error {
	resp, err := http.Get(fmt.Sprintf("%s/service", g.serverBaseUrl))
	if err != nil {
		return fmt.Errorf("Unable to check service details: %v", err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("Gorb server is not healthy. Status code: %d", resp.StatusCode)
	}

	return nil
}

func (g *gorb) Update(controller.IngressEntries) error {
	backendExists, err := g.backendExists()
	if err != nil {
		return err
	}

	if !backendExists {
		err := g.addBackend(&Backend{
			host: g.instanceIp,
			port: 80,
			method: "dr",
			weight: 100,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (g *gorb) backendExists() (bool, error) {
	resp, err := http.Get(fmt.Sprintf("%s/service/node-%s", g.serverBaseUrl, g.instanceIp))
	if err != nil {
		return false, fmt.Errorf("Unable to retrieve backend details for isntance ip: %s error :%v", g.instanceIp, err)
	}
	return resp.StatusCode == 200, nil
}

func (g *gorb) addBackend(backend *Backend) error {
	payload, err := json.Marshal(backend)

	req, err := http.NewRequest("PUT", fmt.Sprintf("%s/service/http-proxy/node-%s", g.serverBaseUrl, g.instanceIp), bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("Error while creating add backend request: %v", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Error while adding backend: %v, error: %v", backend, err)
	}
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("Failed to add backend: %v, status code: %d, response: %v", backend, resp.StatusCode, body)
	}
	return nil
}

func (g *gorb) String() string {
	return "Gorb frontend"
}
