package merlin

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/golang/protobuf/ptypes/wrappers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/merlin/mocks"
	"github.com/sky-uk/merlin/types"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMerlin(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Merlin Updater Test Suite")
}

var _ = Describe("merlin", func() {
	var (
		merlin        controller.Updater
		client        *mocks.MerlinClient
		nlMock        *netlinkMockType
		closeableMock *closeableMockType
		conf          Config

		healthCheck   *types.RealServer_HealthCheck
		emptyResponse = &empty.Empty{}

		expectedServer      *types.RealServer
		expectedHTTPSServer *types.RealServer
	)

	BeforeEach(func() {
		client = &mocks.MerlinClient{}
		nlMock = &netlinkMockType{}
		closeableMock = &closeableMockType{}
		closeableMock.On("Close").Return(nil)
		conf = Config{
			ServiceID:           "service1",
			HTTPSServiceID:      "https-service1",
			InstanceIP:          "172.16.16.1",
			InstancePort:        uint16(8080),
			InstanceHTTPSPort:   uint16(8443),
			ForwardMethod:       "route",
			HealthPort:          uint16(8081),
			HealthPath:          "health",
			HealthUpThreshold:   uint32(4),
			HealthDownThreshold: uint32(2),
			HealthPeriod:        time.Second * 10,
			HealthTimeout:       time.Second,
			DrainDelay:          time.Millisecond,
		}
	})

	JustBeforeEach(func() {
		m, err := New(conf)
		Expect(err).ToNot(HaveOccurred())
		m.(*updater).clientFactory = func(c *Config) (types.MerlinClient, closeable, error) {
			return client, closeableMock, nil
		}
		m.(*updater).nl = nlMock
		merlin = m
		healthCheck = &types.RealServer_HealthCheck{
			Endpoint:      &wrappers.StringValue{Value: fmt.Sprintf("http://:%d/%s", conf.HealthPort, conf.HealthPath)},
			UpThreshold:   conf.HealthUpThreshold,
			DownThreshold: conf.HealthDownThreshold,
			Period:        ptypes.DurationProto(conf.HealthPeriod),
			Timeout:       ptypes.DurationProto(conf.HealthTimeout),
		}

		expectedServer = createExpectedServer(conf.ServiceID, conf.InstanceIP, uint32(conf.InstancePort), healthCheck)
		expectedHTTPSServer = createExpectedServer(conf.HTTPSServiceID, conf.InstanceIP, uint32(conf.InstanceHTTPSPort), healthCheck)
	})

	It("registers itself on start", func() {
		client.On("CreateServer", mock.Anything, expectedServer).Return(emptyResponse, nil)
		client.On("CreateServer", mock.Anything, expectedHTTPSServer).Return(emptyResponse, nil)

		err := merlin.Start()

		Expect(err).ToNot(HaveOccurred())
		client.AssertExpectations(GinkgoT())
		closeableMock.AssertExpectations(GinkgoT())
	})

	It("updates itself on start if already exists", func() {
		// http
		client.On("CreateServer", mock.Anything, expectedServer).Return(emptyResponse,
			status.Error(codes.AlreadyExists, "already exists"))
		client.On("UpdateServer", mock.Anything, expectedServer).Return(emptyResponse, nil)
		// https
		client.On("CreateServer", mock.Anything, expectedHTTPSServer).Return(emptyResponse,
			status.Error(codes.AlreadyExists, "already exists"))
		client.On("UpdateServer", mock.Anything, expectedHTTPSServer).Return(emptyResponse, nil)

		err := merlin.Start()

		Expect(err).ToNot(HaveOccurred())
		client.AssertExpectations(GinkgoT())
	})

	It("deregisters itself on stop", func() {
		// http
		drainServer := createExpectedServer(conf.ServiceID, conf.InstanceIP, uint32(conf.InstancePort), nil)
		drainServer.Config = &types.RealServer_Config{Weight: &wrappers.UInt32Value{Value: 0}}
		delServer := createExpectedServer(conf.ServiceID, conf.InstanceIP, uint32(conf.InstancePort), nil)
		delServer.Config = nil
		// https
		drainHTTPSServer := createExpectedServer(conf.HTTPSServiceID, conf.InstanceIP, uint32(conf.InstanceHTTPSPort), nil)
		drainHTTPSServer.Config = &types.RealServer_Config{Weight: &wrappers.UInt32Value{Value: 0}}
		delHTTPSServer := createExpectedServer(conf.HTTPSServiceID, conf.InstanceIP, uint32(conf.InstanceHTTPSPort), nil)
		delHTTPSServer.Config = nil

		client.On("UpdateServer", mock.Anything, drainServer).Return(emptyResponse, nil)
		client.On("DeleteServer", mock.Anything, delServer).Return(emptyResponse, nil)
		client.On("UpdateServer", mock.Anything, drainHTTPSServer).Return(emptyResponse, nil)
		client.On("DeleteServer", mock.Anything, delHTTPSServer).Return(emptyResponse, nil)

		err := merlin.Stop()

		Expect(err).ToNot(HaveOccurred())
		client.AssertExpectations(GinkgoT())
		closeableMock.AssertExpectations(GinkgoT())
	})

	Context("service IDs are empty", func() {
		BeforeEach(func() {
			conf.ServiceID = ""
			conf.HTTPSServiceID = ""
		})

		It("doesn't register any servers", func() {
			err := merlin.Start()

			Expect(err).ToNot(HaveOccurred())
			client.AssertExpectations(GinkgoT())
		})

		It("doesn't deregister any servers", func() {
			err := merlin.Stop()

			Expect(err).ToNot(HaveOccurred())
			client.AssertExpectations(GinkgoT())
		})
	})

	Context("instance port is unset", func() {
		BeforeEach(func() {
			conf.InstancePort = 0
			conf.InstanceHTTPSPort = 0
		})

		It("doesn't register any servers", func() {
			err := merlin.Start()

			Expect(err).ToNot(HaveOccurred())
			client.AssertExpectations(GinkgoT())
		})

		It("doesn't deregister any servers", func() {
			err := merlin.Stop()

			Expect(err).ToNot(HaveOccurred())
			client.AssertExpectations(GinkgoT())
		})
	})

	Context("manages VIP", func() {
		BeforeEach(func() {
			conf.VIPInterface = "eth1"
			conf.VIP = "10.10.10.1"
		})

		It("should add VIP on start", func() {
			nlMock.On("addVIP", conf.VIPInterface, conf.VIP).Return(nil)
			client.On("CreateServer", mock.Anything, mock.Anything).Return(emptyResponse, nil)

			err := merlin.Start()

			Expect(err).ToNot(HaveOccurred())
			nlMock.AssertExpectations(GinkgoT())
		})

		It("should remove VIP on stop", func() {
			nlMock.On("removeVIP", conf.VIPInterface, conf.VIP).Return(nil)
			client.On("UpdateServer", mock.Anything, mock.Anything).Return(emptyResponse, nil)
			client.On("DeleteServer", mock.Anything, mock.Anything).Return(emptyResponse, nil)

			err := merlin.Stop()

			Expect(err).ToNot(HaveOccurred())
			nlMock.AssertExpectations(GinkgoT())
		})

		It("should remove the VIP if attach fails on start", func() {
			nlMock.On("addVIP", conf.VIPInterface, conf.VIP).Return(nil)
			client.On("CreateServer", mock.Anything, mock.Anything).Return(nil, errors.New("kaboom"))
			nlMock.On("removeVIP", conf.VIPInterface, conf.VIP).Return(nil)

			err := merlin.Start()

			Expect(err).To(HaveOccurred())
			nlMock.AssertExpectations(GinkgoT())
		})
	})
})

func createExpectedServer(serviceID string, instanceIP string, instancePort uint32, healthCheck *types.RealServer_HealthCheck) *types.RealServer {
	return &types.RealServer{
		ServiceID: serviceID,
		Key: &types.RealServer_Key{
			Ip:   instanceIP,
			Port: instancePort,
		},
		Config: &types.RealServer_Config{
			Weight:  &wrappers.UInt32Value{Value: 1},
			Forward: types.ForwardMethod_ROUTE,
		},
		HealthCheck: healthCheck,
	}
}

type netlinkMockType struct {
	mock.Mock
}

func (m *netlinkMockType) addVIP(vipInterface, vip string) error {
	args := m.Called(vipInterface, vip)
	return args.Error(0)
}

func (m *netlinkMockType) removeVIP(vipInterface, vip string) error {
	args := m.Called(vipInterface, vip)
	return args.Error(0)
}

type closeableMockType struct {
	mock.Mock
}

func (m *closeableMockType) Close() error {
	args := m.Called()
	return args.Error(0)
}
