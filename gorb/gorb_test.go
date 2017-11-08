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
	RunSpecs(t, "Gorb E2E Suite")
}

const (
	instanceIP                 = "10.10.0.1"
	drainImmediately           = 0
	backendHealthcheckInterval = "1s"
	backendWeight              = 1000
	backendMethod              = "dr"
	vipLoadbalancer            = "127.0.0.1"
	interfaceProcFsPath        = "/host_ipv4_proc/"
	manageLoopback             = false
)

type gorbResponsePrimer struct {
	response   string
	statusCode int
}

type gorbRecordedRequest struct {
	url    *url.URL
	body   *BackendConfig
	method string
}

type gorbHandler struct {
	responsePrimers  []gorbResponsePrimer
	recordedRequests []gorbRecordedRequest
	requestCounter   int
}

func (h *gorbHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	recordedRequest := gorbRecordedRequest{url: r.URL, body: &BackendConfig{}, method: r.Method}
	h.recordedRequests = append(h.recordedRequests, recordedRequest)

	log.Info("Recorded requests: ", len(h.recordedRequests))
	log.Info("Recorded request url: ", r.URL)

	bodyAsBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		panic(err.Error())
	}

	if len(bodyAsBytes) > 0 {
		if err := json.Unmarshal(bodyAsBytes, recordedRequest.body); err != nil {
			panic(err.Error())
		}
	}

	if h.responsePrimers[h.requestCounter].statusCode != 0 {
		w.WriteHeader(h.responsePrimers[h.requestCounter].statusCode)
	}
	if h.responsePrimers[h.requestCounter].response != "" {
		data, _ := json.Marshal(h.responsePrimers[h.requestCounter].response)
		w.Write(data)
	}
	h.requestCounter++
}

var _ = Describe("Gorb", func() {
	var (
		gorb             controller.Updater
		server           *httptest.Server
		serverURL        string
		responsePrimers  []gorbResponsePrimer
		recordedRequests []gorbRecordedRequest
		gorbH            *gorbHandler
	)

	BeforeSuite(func() {
		metrics.SetConstLabels(make(prometheus.Labels))
		responsePrimers = []gorbResponsePrimer{}
		recordedRequests = []gorbRecordedRequest{}
		gorbH = &gorbHandler{responsePrimers: responsePrimers, recordedRequests: recordedRequests}
		server = httptest.NewServer(gorbH)

		serverURL = server.URL
		log.Info("url ", serverURL)
		config := &Config{
			ServerBaseURL:              serverURL,
			InstanceIP:                 instanceIP,
			DrainDelay:                 drainImmediately,
			ServicesDefinition:         []VirtualService{{Name: "http-proxy", Port: 80}},
			BackendMethod:              backendMethod,
			BackendWeight:              backendWeight,
			VipLoadbalancer:            vipLoadbalancer,
			ManageLoopback:             manageLoopback,
			BackendHealthcheckInterval: backendHealthcheckInterval,
			InterfaceProcFsPath:        interfaceProcFsPath,
		}
		gorb, _ = New(config)
	})

	BeforeEach(func() {
		gorbH.responsePrimers = []gorbResponsePrimer{}
		gorbH.recordedRequests = []gorbRecordedRequest{}
		gorbH.requestCounter = 0
	})

	AfterSuite(func() {
		server.Close()
	})

	Describe("Health endpoint", func() {
		It("should be healthy when status code is 200", func() {
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 200})
			err := gorb.Health()
			Expect(len(gorbH.recordedRequests)).To(Equal(1))
			Expect(gorbH.recordedRequests[0].url.RequestURI()).To(Equal("/service"))
			Expect(err).NotTo(HaveOccurred())
		})
		It("should return error when status code is not 200", func() {
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 500})
			err := gorb.Health()
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Update backends", func() {
		It("should add itself as new backend", func() {
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 404})
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 200})
			err := gorb.Update(controller.IngressEntries{})
			Expect(len(gorbH.recordedRequests)).To(Equal(2))
			Expect(gorbH.recordedRequests[0].method).To(Equal("GET"))
			Expect(gorbH.recordedRequests[0].url.RequestURI()).To(Equal(fmt.Sprintf("/service/http-proxy/node-http-proxy-%s", instanceIP)))

			Expect(gorbH.recordedRequests[1].method).To(Equal("PUT"))
			Expect(gorbH.recordedRequests[1].body.Host).To(Equal("10.10.0.1"))
			Expect(gorbH.recordedRequests[1].body.Port).To(Equal(80))
			Expect(gorbH.recordedRequests[1].body.Method).To(Equal("dr"))
			Expect(gorbH.recordedRequests[1].body.Weight).To(Equal(1000))
			Expect(gorbH.recordedRequests[1].url.RequestURI()).To(Equal(fmt.Sprintf("/service/http-proxy/node-http-proxy-%s", instanceIP)))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should remove itself on shutdown", func() {
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 200})
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 200})
			err := gorb.Stop()
			Expect(len(gorbH.recordedRequests)).To(Equal(2))
			Expect(gorbH.recordedRequests[0].method).To(Equal("PATCH"))
			Expect(gorbH.recordedRequests[0].body.Host).To(Equal("10.10.0.1"))
			Expect(gorbH.recordedRequests[0].body.Port).To(Equal(80))
			Expect(gorbH.recordedRequests[0].body.Method).To(Equal("dr"))
			Expect(gorbH.recordedRequests[0].body.Weight).To(Equal(0))
			Expect(gorbH.recordedRequests[0].url.RequestURI()).To(Equal(fmt.Sprintf("/service/http-proxy/node-http-proxy-%s", instanceIP)))

			Expect(gorbH.recordedRequests[1].method).To(Equal("DELETE"))
			Expect(gorbH.recordedRequests[1].url.RequestURI()).To(Equal(fmt.Sprintf("/service/http-proxy/node-http-proxy-%s", instanceIP)))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return an error when failing to add a backend", func() {
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 404})
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 500})
			err := gorb.Update(controller.IngressEntries{})
			Expect(len(gorbH.recordedRequests)).To(Equal(2))
			Expect(err).To(HaveOccurred())
		})

	})

	Describe("Multiple services", func() {
		It("should all have their backends", func() {
			config := &Config{
				ServerBaseURL:              serverURL,
				InstanceIP:                 instanceIP,
				DrainDelay:                 drainImmediately,
				ServicesDefinition:         []VirtualService{{Name: "http-proxy", Port: 80}, {Name: "https-proxy", Port: 443}},
				BackendMethod:              backendMethod,
				BackendWeight:              backendWeight,
				VipLoadbalancer:            vipLoadbalancer,
				ManageLoopback:             manageLoopback,
				BackendHealthcheckInterval: backendHealthcheckInterval,
				InterfaceProcFsPath:        interfaceProcFsPath,
			}
			gorb, _ = New(config)
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 404})
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 200})
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 404})
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 200})
			err := gorb.Update(controller.IngressEntries{})
			Expect(len(gorbH.recordedRequests)).To(Equal(4))
			Expect(err).NotTo(HaveOccurred())
			Expect(gorbH.recordedRequests[1].url.RequestURI()).To(Equal(fmt.Sprintf("/service/http-proxy/node-http-proxy-%s", instanceIP)))
			Expect(gorbH.recordedRequests[1].body.Port).To(Equal(80))

			Expect(gorbH.recordedRequests[3].url.RequestURI()).To(Equal(fmt.Sprintf("/service/https-proxy/node-https-proxy-%s", instanceIP)))
			Expect(gorbH.recordedRequests[3].body.Port).To(Equal(443))
		})
	})
})
