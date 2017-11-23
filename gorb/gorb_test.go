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
	"github.com/stretchr/testify/mock"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Gorb E2E Suite")
}

const (
	instanceIP                 = "10.10.0.1"
	drainImmediately           = 0
	backendHealthcheckInterval = "1s"
	backendHealthcheckType     = "http"
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

type fakeCommandRunner struct {
	mock.Mock
}

func (c *fakeCommandRunner) Execute(cmd string) ([]byte, error) {
	args := c.Called(cmd)
	return args.Get(0).([]byte), args.Error(1)
}

func mockLoopbackExistsCommand(mockCommand *fakeCommandRunner, vip string) {
	mockLoopbackCheckCommand(mockCommand, vip, "1\n") // may have trailing chars
}

func mockLoopbackDoesNotExistCommand(mockCommand *fakeCommandRunner, vip string) {
	mockLoopbackCheckCommand(mockCommand, vip, "0\n") // may have trailing chars
}

func mockLoopbackCheckCommand(mockCommand *fakeCommandRunner, vip string, expectedCount string) {
	mockCommand.On("Execute", fmt.Sprintf("sudo ip addr show label lo:0 | grep -c %s/32 | xargs echo", vip)).Return([]byte(expectedCount), nil)
}

func mockDisableArpCommand(mockCommand *fakeCommandRunner) {
	mockCommand.On("Execute", "echo 1 | sudo tee /host_ipv4_proc/arp_ignore > /dev/null").Return([]byte{}, nil)
	mockCommand.On("Execute", "echo 2 | sudo tee /host_ipv4_proc/arp_announce > /dev/null").Return([]byte{}, nil)
}

func mockEnableArpCommand(mockCommand *fakeCommandRunner) {
	mockCommand.On("Execute", "echo 0 | sudo tee /host_ipv4_proc/arp_ignore > /dev/null").Return([]byte{}, nil)
	mockCommand.On("Execute", "echo 0 | sudo tee /host_ipv4_proc/arp_announce > /dev/null").Return([]byte{}, nil)
}

func singleServiceConfig(serverURL string) *Config {
	config := newConfig(serverURL)
	config.ServicesDefinition = []VirtualService{{Name: "http-proxy", Port: 80}}
	return config
}

func multipleServicesConfig(serverURL string) *Config {
	config := newConfig(serverURL)
	config.ServicesDefinition = []VirtualService{{Name: "http-proxy", Port: 80}, {Name: "https-proxy", Port: 443}}
	return config
}

func loopbackManagingConfig(serverURL string) *Config {
	config := newConfig(serverURL)
	config.ManageLoopback = true
	return config
}

func newConfig(serverURL string) *Config {
	return &Config{
		ServerBaseURL:              serverURL,
		InstanceIP:                 instanceIP,
		DrainDelay:                 drainImmediately,
		ServicesDefinition:         []VirtualService{},
		BackendMethod:              backendMethod,
		BackendWeight:              backendWeight,
		VipLoadbalancer:            vipLoadbalancer,
		ManageLoopback:             manageLoopback,
		BackendHealthcheckInterval: backendHealthcheckInterval,
		BackendHealthcheckType:     backendHealthcheckType,
		InterfaceProcFsPath:        interfaceProcFsPath,
	}
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
		g                controller.Updater
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

			g, _ = New(singleServiceConfig(serverURL))
			err := g.Health()

			Expect(len(gorbH.recordedRequests)).To(Equal(1))
			Expect(gorbH.recordedRequests[0].url.RequestURI()).To(Equal("/service"))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error when status code is not 200", func() {
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 500})

			g, _ = New(singleServiceConfig(serverURL))
			err := g.Health()

			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Update backends", func() {
		It("should add itself as new backend", func() {
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 404})
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 200})

			g, _ = New(singleServiceConfig(serverURL))
			err := g.Update(controller.IngressEntries{})

			Expect(len(gorbH.recordedRequests)).To(Equal(2))
			Expect(gorbH.recordedRequests[0].method).To(Equal("GET"))
			Expect(gorbH.recordedRequests[0].url.RequestURI()).To(Equal(fmt.Sprintf("/service/http-proxy/node-http-proxy-%s", instanceIP)))
			Expect(gorbH.recordedRequests[1].method).To(Equal("PUT"))
			Expect(gorbH.recordedRequests[1].body.Host).To(Equal("10.10.0.1"))
			Expect(gorbH.recordedRequests[1].body.Port).To(Equal(80))
			Expect(gorbH.recordedRequests[1].body.Method).To(Equal("dr"))
			Expect(gorbH.recordedRequests[1].body.Weight).To(Equal(1000))
			Expect(gorbH.recordedRequests[1].body.Pulse.TypeHealthcheck).To(Equal("http"))
			Expect(gorbH.recordedRequests[1].body.Pulse.Interval).To(Equal(backendHealthcheckInterval))
			Expect(gorbH.recordedRequests[1].url.RequestURI()).To(Equal(fmt.Sprintf("/service/http-proxy/node-http-proxy-%s", instanceIP)))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create the backend healthcheck with tcp type", func() {
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 404})
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 200})

			config := singleServiceConfig(serverURL)
			config.BackendHealthcheckType = "tcp"
			g, _ = New(config)
			err := g.Update(controller.IngressEntries{})

			Expect(len(gorbH.recordedRequests)).To(Equal(2))
			Expect(gorbH.recordedRequests[0].url.RequestURI()).To(Equal(fmt.Sprintf("/service/http-proxy/node-http-proxy-%s", instanceIP)))
			Expect(gorbH.recordedRequests[1].body.Pulse.TypeHealthcheck).To(Equal("tcp"))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should remove itself on shutdown", func() {
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 200})
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 200})

			g, _ = New(singleServiceConfig(serverURL))
			err := g.Stop()

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

			g, _ = New(singleServiceConfig(serverURL))
			err := g.Update(controller.IngressEntries{})

			Expect(len(gorbH.recordedRequests)).To(Equal(2))
			Expect(err).To(HaveOccurred())
		})

	})

	Describe("Multiple services", func() {
		It("should all have their backends", func() {
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 404})
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 200})
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 404})
			gorbH.responsePrimers = append(gorbH.responsePrimers, gorbResponsePrimer{statusCode: 200})

			g, _ = New(multipleServicesConfig(serverURL))
			err := g.Update(controller.IngressEntries{})

			Expect(len(gorbH.recordedRequests)).To(Equal(4))
			Expect(err).NotTo(HaveOccurred())
			Expect(gorbH.recordedRequests[1].url.RequestURI()).To(Equal(fmt.Sprintf("/service/http-proxy/node-http-proxy-%s", instanceIP)))
			Expect(gorbH.recordedRequests[1].body.Port).To(Equal(80))
			Expect(gorbH.recordedRequests[3].url.RequestURI()).To(Equal(fmt.Sprintf("/service/https-proxy/node-https-proxy-%s", instanceIP)))
			Expect(gorbH.recordedRequests[3].body.Port).To(Equal(443))
		})
	})

	Describe("Loopback interface", func() {
		It("should be added when does not exists", func() {
			g, _ = New(loopbackManagingConfig(serverURL))
			mockCommand := &fakeCommandRunner{}
			g.(*gorb).command = mockCommand

			mockLoopbackDoesNotExistCommand(mockCommand, vipLoadbalancer)
			mockCommand.On("Execute", fmt.Sprintf("sudo ip addr add %s/32 dev lo label lo:0", vipLoadbalancer)).Return([]byte{}, nil)
			mockDisableArpCommand(mockCommand)

			err := g.Update(controller.IngressEntries{})
			Expect(err).NotTo(HaveOccurred())
			mockCommand.AssertExpectations(GinkgoT())
		})

		It("should be not be added when alredy exists", func() {
			g, _ = New(loopbackManagingConfig(serverURL))
			mockCommand := &fakeCommandRunner{}
			g.(*gorb).command = mockCommand

			mockLoopbackExistsCommand(mockCommand, vipLoadbalancer)
			mockDisableArpCommand(mockCommand)

			err := g.Update(controller.IngressEntries{})
			Expect(err).NotTo(HaveOccurred())
			mockCommand.AssertExpectations(GinkgoT())
		})

		It("should be deleted on stop", func() {
			g, _ = New(loopbackManagingConfig(serverURL))
			mockCommand := &fakeCommandRunner{}
			g.(*gorb).command = mockCommand

			mockLoopbackExistsCommand(mockCommand, vipLoadbalancer)
			mockCommand.On("Execute", fmt.Sprintf("sudo ip addr del %s/32 dev lo label lo:0", vipLoadbalancer)).Return([]byte{}, nil)
			mockEnableArpCommand(mockCommand)

			err := g.Stop()
			Expect(err).NotTo(HaveOccurred())
			mockCommand.AssertExpectations(GinkgoT())
		})

		It("should not be deleted on stop if not present", func() {
			g, _ = New(loopbackManagingConfig(serverURL))
			mockCommand := &fakeCommandRunner{}
			g.(*gorb).command = mockCommand

			mockLoopbackDoesNotExistCommand(mockCommand, vipLoadbalancer)
			mockEnableArpCommand(mockCommand)

			err := g.Stop()
			Expect(err).NotTo(HaveOccurred())
			mockCommand.AssertExpectations(GinkgoT())
		})
	})
})
