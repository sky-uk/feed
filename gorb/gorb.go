/*
Package gorb provide the ability to register and deregister the backend
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
  "strings"
  "strconv"
  "io/ioutil"
  "github.com/hashicorp/go-multierror"
)

type ArgPulse struct {
  method string `json:"method"`
  path string `json:"path"`
  expect string `json:"expect"`
}

type Pulse struct {
  typeHealthcheck string `json:"type"`
  args ArgPulse `json:"args"`
  interval string `json:"interval"`
}

type BackendConf struct {
  host string `json:"host"`
  port int `json:"port"`
  method string `json:"method"`
  weight int `json:"weight"`
  pulse Pulse `json:"pulse"`
}

type Backend struct {
  serviceName string
  backendConf BackendConf
}

func New(serverBaseUrl string, instanceIp string, drainDelay time.Duration, servicesName string, servicesPort string, backendWeight int, backendMethod string) (controller.Updater, error) {
  if serverBaseUrl == "" {
    return nil, errors.New("unable to create Gorb updater: missing server ip address")
  }
  //initMetrics()
  log.Infof("Gorb server url: %s, drainDelay: %d, instance ip adddress: %s ", serverBaseUrl, drainDelay, instanceIp)

  backendDefinition := []Backend {}
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
      method: "GET",
      path: "/",
      expect: "200",
    }
    pulse := Pulse{
      args: args,
      typeHealthcheck: "http",
      interval: "1s",
    }
    backend = Backend {
        serviceName: service,
        backendConf: BackendConf {
          host: instanceIp,
          port: port,
          method: backendMethod,
          weight: backendWeight,
          pulse: pulse,
        },
    }

    backendDefinition = append(backendDefinition, backend)
  }

  return &gorb{
    serverBaseUrl:     serverBaseUrl,
    drainDelay:        drainDelay,
    instanceIp:        instanceIp,
    backend:           backendDefinition,
  }, nil
}

type gorb struct {
  serverBaseUrl string
  drainDelay    time.Duration
  instanceIp    string
  backend       []Backend
}

func (g *gorb) Start() error {
  return nil
}

// Stop removes this instance from Gorb
func (g *gorb) Stop() error {

  var errorArr error
  for _, backend := range g.backend {
    backend.backendConf.weight = 0
    err := g.modifyBackend(&backend)
    if err != nil {
      log.Error("Unable to set the backend weight to 0", err)
      errorArr = multierror.Append(errorArr, err)
    } else {
      log.Infof("Set backend weight to 0", g.instanceIp)
    }
  }

  log.Infof("Waiting %v to finish gorb draining", g.drainDelay)
  time.Sleep(g.drainDelay)

  for _, backend := range g.backend {
    err := g.removeBackend(&backend)
    if err != nil {
      log.Error("Unable to remove Backend ", err)
      errorArr = multierror.Append(errorArr, err)
    } else {
    log.Infof("Backend succesfully removed", g.instanceIp)
    }
  }

  if errorArr != nil {
    return errorArr
  } else {
    return nil
  }

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

  var errorArr error
  for _, backend := range g.backend {

    backendExists, err := g.backendExists()
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
    log.Info("Backend Succesfully added: ", g.instanceIp)
  }

  if errorArr != nil {
    return errorArr
  } else {
    return nil
  }
}

func (g *gorb) backendExists() (int, error) {
  resp, err := http.Get(fmt.Sprintf("%s/service/node-%s", g.serverBaseUrl, g.instanceIp))
  if err != nil {
    return 0, fmt.Errorf("Unable to retrieve backend details for instance ip: %s error :%v", g.instanceIp, err)
  }
  return resp.StatusCode, nil
}

func (g *gorb) addBackend(backend *Backend) error {
  payload, err := json.Marshal(backend.backendConf)

  req, err := http.NewRequest("PUT", fmt.Sprintf("%s/service/%s/node-%s", g.serverBaseUrl, backend.serviceName, g.instanceIp), bytes.NewBuffer(payload))
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
  payload, err := json.Marshal(backend.backendConf)

  req, err := http.NewRequest("PATCH", fmt.Sprintf("%s/service/%s/node-%s", g.serverBaseUrl, backend.serviceName, g.instanceIp), bytes.NewBuffer(payload))

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

  req, err := http.NewRequest("DELETE", fmt.Sprintf("%s/service/%s/node-%s", g.serverBaseUrl, backend.serviceName, g.instanceIp), nil)
  if err != nil {
    return fmt.Errorf("Error while creating add backend request: %v", err)
  }

  client := &http.Client{}
  resp, err := client.Do(req)
  if err != nil {
    return fmt.Errorf("Error while removing backend: %v, error: %v", err)
  }
  log.Infof("Error removing backend", resp.StatusCode)
  if resp.StatusCode != 200 {
    body, _ := ioutil.ReadAll(resp.Body)
    return fmt.Errorf("Failed to remove backend: %v, status code: %d, response: %v", resp.StatusCode, body)
  }
  return nil
}


func (g *gorb) String() string {
  return "Gorb frontend"
}
