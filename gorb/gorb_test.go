package gorb

import (
  "testing"
  "net/http"
  "net/http/httptest"
  log "github.com/Sirupsen/logrus"
  "encoding/json"
  "github.com/sky-uk/feed/controller"
  . "github.com/onsi/ginkgo"
  . "github.com/onsi/gomega"
  "net/url"
  "fmt"
  "io/ioutil"
)

func TestE2E(t *testing.T) {
  RegisterFailHandler(Fail)
  RunSpecs(t, "E2E Suite")
}

const (
  instanceIP = "10.10.0.1"
  drainImmediately = 0
  servicesName = "http-proxy"
  servicesPort = "80"
  backendWeight = 1000
  backendMethod = "dr"
)

type gorbResponsePrimer struct {
  response   string
  statusCode int
}

type gorbRecordedRequest struct {
  url *url.URL
  body *Backend
}

type gorbHandler struct {
  responsePrimer []gorbResponsePrimer
  recordedRequest []gorbRecordedRequest
  requestCounter int
}

func (h *gorbHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
  defer func() { h.requestCounter++ }()
  recordedRequest := gorbRecordedRequest{url: r.URL, body: &Backend{}}
  h.recordedRequest = append(h.recordedRequest, recordedRequest)

  log.Info("Recorded requests: ", len(h.recordedRequest))
  log.Info("Recorded request url: ", r.URL)

  bodyAsBytes, err := ioutil.ReadAll(r.Body)
  if err != nil {
    panic(err.Error())
  }

  if len(bodyAsBytes) > 0 {
    if err := json.Unmarshal(bodyAsBytes, &recordedRequest.body); err != nil {
      panic(err.Error())
    }
  }

  if (h.responsePrimer[h.requestCounter].statusCode != 0) {
    w.WriteHeader(h.responsePrimer[h.requestCounter].statusCode)
  }
  if (h.responsePrimer[h.requestCounter].response != "") {
    data, _ := json.Marshal(h.responsePrimer[h.requestCounter].response)
    w.Write(data)
  }

}

var _ = Describe("Gorb", func() {
  var (
    gorb controller.Updater
    server *httptest.Server
    serverURL string
    responsePrimer []gorbResponsePrimer
    recordedRequests []gorbRecordedRequest
    gorbH *gorbHandler
  )

  BeforeSuite(func() {
    responsePrimer = []gorbResponsePrimer{}
    recordedRequests = []gorbRecordedRequest{}
    gorbH = &gorbHandler{responsePrimer: responsePrimer, recordedRequest: recordedRequests}
    server = httptest.NewServer(gorbH)

    serverURL = server.URL
    log.Info("url ", serverURL)
    gorb, _ = New(serverURL, instanceIP, drainImmediately, servicesName, servicesPort, backendWeight, backendMethod)
  })

  BeforeEach(func() {
    gorbH.responsePrimer = []gorbResponsePrimer{}
    gorbH.recordedRequest = []gorbRecordedRequest{}
    gorbH.requestCounter = 0
  })


  AfterSuite(func() {
    server.Close()
  })

  Describe("Health endpoint", func() {
    It("should be healthy when status code is 200", func() {
      gorbH.responsePrimer = append(gorbH.responsePrimer, gorbResponsePrimer{statusCode: 200})
      err := gorb.Health()
      Expect(err).NotTo(HaveOccurred())
    })
    It("should return error when status code is not 200", func() {
      gorbH.responsePrimer = append(gorbH.responsePrimer, gorbResponsePrimer{statusCode: 500})
      err := gorb.Health()
      Expect(err).To(HaveOccurred())
    })
  })

  Describe("Update backends", func() {
    It("should add itself as new backend", func() {
      gorbH.responsePrimer = append(gorbH.responsePrimer, gorbResponsePrimer{statusCode: 404})
      gorbH.responsePrimer = append(gorbH.responsePrimer, gorbResponsePrimer{statusCode: 200})
      err := gorb.Update(controller.IngressEntries{})
      Expect(len(gorbH.recordedRequest)).To(Equal(2))
      Expect(gorbH.recordedRequest[1].url.RequestURI()).To(Equal(fmt.Sprintf("/service/http-proxy/node-%s", instanceIP)))
      Expect(err).NotTo(HaveOccurred())
    })

    It("should modify when already exist", func() {
      gorbH.responsePrimer = append(gorbH.responsePrimer, gorbResponsePrimer{statusCode: 200})
      gorbH.responsePrimer = append(gorbH.responsePrimer, gorbResponsePrimer{statusCode: 200})
      err := gorb.Update(controller.IngressEntries{})
      Expect(len(gorbH.recordedRequest)).To(Equal(2))
      Expect(err).NotTo(HaveOccurred())
    })

    It("should remove itself on shutdown", func() {
      gorbH.responsePrimer = append(gorbH.responsePrimer, gorbResponsePrimer{statusCode: 200})
      gorbH.responsePrimer = append(gorbH.responsePrimer, gorbResponsePrimer{statusCode: 200})
      err := gorb.Stop()
      Expect(len(gorbH.recordedRequest)).To(Equal(2))
      Expect(err).NotTo(HaveOccurred())
    })

    //It("what if it erros on retrieval or update?", func() {
    //	gorb.Update(controller.IngressEntries{})
    //})
    //It("look up the scheme to work out whether to add https or both http/https?", func() {
    //	gorb.Update(controller.IngressEntries{})
    //})
  })
})
