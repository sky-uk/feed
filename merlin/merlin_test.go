package merlin

import (
	"testing"

	"time"

	"errors"

	"github.com/gogo/protobuf/proto"
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
		merlin controller.Updater
		client *mocks.MerlinClient
		nl     *nlMock
		conf   Config

		emptyResponse = &empty.Empty{}
	)

	BeforeEach(func() {
		client = &mocks.MerlinClient{}
		nl = &nlMock{}
		conf = Config{
			ServiceID:     "service1",
			InstanceIP:    "172.16.16.1",
			InstancePort:  uint16(8080),
			ForwardMethod: "route",
			DrainDelay:    time.Millisecond,
		}
	})

	JustBeforeEach(func() {
		m, err := New(conf)
		Expect(err).ToNot(HaveOccurred())
		m.(*updater).client = client
		m.(*updater).nl = nl
		merlin = m
	})

	It("registers itself on start", func() {
		expectedServer := &types.RealServer{
			ServiceID: conf.ServiceID,
			Key: &types.RealServer_Key{
				Ip:   conf.InstanceIP,
				Port: uint32(conf.InstancePort),
			},
			Config: &types.RealServer_Config{
				Weight:  &wrappers.UInt32Value{Value: 1},
				Forward: types.ForwardMethod_ROUTE,
			},
		}
		client.On("CreateServer", mock.Anything, expectedServer).Return(emptyResponse, nil)

		err := merlin.Start()

		Expect(err).ToNot(HaveOccurred())
		client.AssertExpectations(GinkgoT())
	})

	It("updates itself on start if already exists", func() {
		expectedServer := &types.RealServer{
			ServiceID: conf.ServiceID,
			Key: &types.RealServer_Key{
				Ip:   conf.InstanceIP,
				Port: uint32(conf.InstancePort),
			},
			Config: &types.RealServer_Config{
				Weight:  &wrappers.UInt32Value{Value: 1},
				Forward: types.ForwardMethod_ROUTE,
			},
		}
		client.On("CreateServer", mock.Anything, expectedServer).Return(emptyResponse,
			status.Error(codes.AlreadyExists, "already exists"))
		client.On("UpdateServer", mock.Anything, expectedServer).Return(emptyResponse, nil)

		err := merlin.Start()

		Expect(err).ToNot(HaveOccurred())
		client.AssertExpectations(GinkgoT())

	})

	It("deregisters itself on stop", func() {
		drainServer := &types.RealServer{
			ServiceID: conf.ServiceID,
			Key: &types.RealServer_Key{
				Ip:   conf.InstanceIP,
				Port: uint32(conf.InstancePort),
			},
			Config: &types.RealServer_Config{
				Weight: &wrappers.UInt32Value{Value: 0},
			},
		}
		delServer := proto.Clone(drainServer).(*types.RealServer)
		delServer.Config = nil
		client.On("UpdateServer", mock.Anything, drainServer).Return(emptyResponse, nil)
		client.On("DeleteServer", mock.Anything, delServer).Return(emptyResponse, nil)

		err := merlin.Stop()

		Expect(err).ToNot(HaveOccurred())
		client.AssertExpectations(GinkgoT())
	})

	Context("manages VIP", func() {
		BeforeEach(func() {
			conf.VIPInterface = "eth1"
			conf.VIP = "10.10.10.1"
		})

		It("should add VIP on start", func() {
			nl.On("addVIP", conf.VIPInterface, conf.VIP).Return(nil)
			client.On("CreateServer", mock.Anything, mock.Anything).Return(emptyResponse, nil)

			err := merlin.Start()

			Expect(err).ToNot(HaveOccurred())
			nl.AssertExpectations(GinkgoT())
		})

		It("should remove VIP on stop", func() {
			nl.On("removeVIP", conf.VIPInterface, conf.VIP).Return(nil)
			client.On("UpdateServer", mock.Anything, mock.Anything).Return(emptyResponse, nil)
			client.On("DeleteServer", mock.Anything, mock.Anything).Return(emptyResponse, nil)

			err := merlin.Stop()

			Expect(err).ToNot(HaveOccurred())
			nl.AssertExpectations(GinkgoT())
		})

		It("should remove the VIP if attach fails on start", func() {
			nl.On("addVIP", conf.VIPInterface, conf.VIP).Return(nil)
			client.On("CreateServer", mock.Anything, mock.Anything).Return(nil, errors.New("kaboom"))
			nl.On("removeVIP", conf.VIPInterface, conf.VIP).Return(nil)

			err := merlin.Start()

			Expect(err).To(HaveOccurred())
			nl.AssertExpectations(GinkgoT())
		})
	})
})

type nlMock struct {
	mock.Mock
}

func (m *nlMock) addVIP(vipInterface, vip string) error {
	args := m.Called(vipInterface, vip)
	return args.Error(0)
}

func (m *nlMock) removeVIP(vipInterface, vip string) error {
	args := m.Called(vipInterface, vip)
	return args.Error(0)
}
