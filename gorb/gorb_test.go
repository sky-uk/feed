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
	instanceIp = "10.10.0.1"
	drainImmediately = 0
)

var _ = Describe("Gorb", func() {
	var (
		gorb controller.Updater
		server *httptest.Server
		serverUrl string
		responsePrimer *gorbResponsePrimer
		recordedRequests []*gorbRecordedRequest

	)

	BeforeSuite(func() {
		responsePrimer = &gorbResponsePrimer{}
		recordedRequests = make([]*gorbRecordedRequest, 0)
		server = httptest.NewServer(&gorbHandler{responsePrimer, recordedRequests})

		serverUrl = server.URL
		log.Info("url", serverUrl)
		gorb, _ = New(serverUrl, instanceIp, drainImmediately)
	})

	AfterSuite(func() {
		server.Close()
	})

	//Describe("Health endpoint", func() {
	//	It("should be healthy when status code is 200", func() {
	//		responsePrimer.statusCode = 200
	//		err := gorb.Health()
	//		Expect(err).NotTo(HaveOccurred())
	//	})
	//	It("should return error when status code is not 200", func() {
	//		responsePrimer.statusCode = 500
	//		err := gorb.Health()
	//		Expect(err).To(HaveOccurred())
	//	})
	//})

	Describe("Update backends", func() {
		It("should add itself as new backend", func() {
			responsePrimer.statusCode = 404
			gorb.Update(controller.IngressEntries{})

			Expect(len(recordedRequests)).To(Equal(2))
			Expect(recordedRequests[0].url.RequestURI()).To(Equal(fmt.Sprintf("/service/http-proxy/node-%s", instanceIp)))
		})
		//
		//It("should skip adding when already exist", func() {
		//	responsePrimer.statusCode = 200
		//	gorb.Update(controller.IngressEntries{})
		//})
		//It("should remove itself on shutdown", func() {
		//	gorb.Update(controller.IngressEntries{})
		//})
		//It("what if it erros on retrieval or update?", func() {
		//	gorb.Update(controller.IngressEntries{})
		//})
		//It("look up the scheme to work out whether to add https or both http/https?", func() {
		//	gorb.Update(controller.IngressEntries{})
		//})
	})
})


type gorbResponsePrimer struct {
	response   string
	statusCode int
}

type gorbRecordedRequest struct {
	url *url.URL
	body *Backend
}

type gorbHandler struct {
	responsePrimer *gorbResponsePrimer
	recordedRequest []*gorbRecordedRequest
}

func (h *gorbHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	recordedRequest := &gorbRecordedRequest{url: r.URL, body: &Backend{}}
	h.recordedRequest = append(h.recordedRequest, recordedRequest)

	log.Info("Recorded requests", len(h.recordedRequest))
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

	if (h.responsePrimer.statusCode != 0) {
		w.WriteHeader(h.responsePrimer.statusCode)
	}
	if (h.responsePrimer.response != "") {
		data, _ := json.Marshal(h.responsePrimer.response)
		w.Write(data)
	}
}