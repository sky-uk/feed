package gorb

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	log "github.com/Sirupsen/logrus"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/util/metrics"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

const (
	instanceIP       = "10.10.0.1"
	drainImmediately = 0
	servicesName     = "http-proxy"
	servicesPort     = "80"
	backendWeight    = 1000
	backendMethod    = "dr"
	vipLoadbalancer  = "127.0.0.1"
	manageLoopback   = false
)

type gorbResponsePrimer struct {
	response   string
	statusCode int
}

type gorbRecordedRequest struct {
	url  *url.URL
	body *Backend
}

type gorbHandler struct {
	responsePrimer  []gorbResponsePrimer
	recordedRequest []gorbRecordedRequest
	requestCounter  int
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

	if h.responsePrimer[h.requestCounter].statusCode != 0 {
		w.WriteHeader(h.responsePrimer[h.requestCounter].statusCode)
	}
	if h.responsePrimer[h.requestCounter].response != "" {
		data, _ := json.Marshal(h.responsePrimer[h.requestCounter].response)
		w.Write(data)
	}

}

var _ = Describe("Gorb", func() {
	var (
		gorb             controller.Updater
		server           *httptest.Server
		serverURL        string
		responsePrimer   []gorbResponsePrimer
		recordedRequests []gorbRecordedRequest
		gorbH            *gorbHandler
	)

	BeforeSuite(func() {
		metrics.SetConstLabels(make(prometheus.Labels))
		responsePrimer = []gorbResponsePrimer{}
		recordedRequests = []gorbRecordedRequest{}
		gorbH = &gorbHandler{responsePrimer: responsePrimer, recordedRequest: recordedRequests}
		server = httptest.NewServer(gorbH)

		serverURL = server.URL
		log.Info("url ", serverURL)

		gorb, _ = New(serverURL, instanceIP, drainImmediately, servicesName, servicesPort, backendWeight, backendMethod, vipLoadbalancer, manageLoopback)
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
			Expect(gorbH.recordedRequest[1].url.RequestURI()).To(Equal(fmt.Sprintf("/service/http-proxy/node-http-proxy-%s", instanceIP)))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should remove itself on shutdown", func() {
			gorbH.responsePrimer = append(gorbH.responsePrimer, gorbResponsePrimer{statusCode: 200})
			gorbH.responsePrimer = append(gorbH.responsePrimer, gorbResponsePrimer{statusCode: 200})
			err := gorb.Stop()
			Expect(len(gorbH.recordedRequest)).To(Equal(2))
			Expect(err).NotTo(HaveOccurred())
		})

	})
})
