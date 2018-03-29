package merlin

import (
	"fmt"

	"context"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/prometheus/common/log"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/merlin/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const merlinTimeout = time.Second * 10

// Config for merlin updater.
type Config struct {
	Endpoint            string
	ServiceID           string
	InstanceIP          string
	InstancePort        uint16
	ForwardMethod       string
	HealthPort          uint16
	HealthPath          string
	HealthUpThreshold   uint32
	HealthDownThreshold uint32
	HealthPeriod        time.Duration
	HealthTimeout       time.Duration
	DrainDelay          time.Duration
	VIP                 string
	VIPInterface        string
}

type updater struct {
	Config
	clientConn *grpc.ClientConn
	client     types.MerlinClient
	nl         netlinkWrapper
}

// New merlin updater.
func New(conf Config) (controller.Updater, error) {
	u := &updater{
		Config: conf,
		nl:     &netlinkWrapperImpl{},
	}
	return u, nil
}

func (u *updater) Start() error {
	if err := u.createClient(); err != nil {
		return err
	}
	if err := u.addVIP(); err != nil {
		return err
	}
	if err := u.registerWithMerlin(); err != nil {
		if err := u.removeVIP(); err != nil {
			log.Warnf("Unable to remove VIP: %v", err)
		}
		return err
	}
	return nil
}

func (u *updater) Stop() error {
	if u.clientConn != nil {
		if err := u.clientConn.Close(); err != nil {
			log.Warnf("error when stopping merlin grpc connection: %v", err)
		}
	}
	u.deregisterWithMerlin()
	return u.removeVIP()
}

func (u *updater) Update(controller.IngressEntries) error {
	return nil
}

func (u *updater) Health() error {
	return nil
}

func (u *updater) createBaseRealServer() *types.RealServer {
	return &types.RealServer{
		ServiceID: u.ServiceID,
		Key: &types.RealServer_Key{
			Ip:   u.InstanceIP,
			Port: uint32(u.InstancePort),
		},
	}
}

func (u *updater) registerWithMerlin() error {
	ctx, cancel := context.WithTimeout(context.Background(), merlinTimeout)
	defer cancel()

	forward, ok := types.ForwardMethod_value[strings.ToUpper(u.ForwardMethod)]
	if !ok {
		return fmt.Errorf("unrecognized forward method: %s", u.ForwardMethod)
	}
	server := u.createBaseRealServer()
	server.Config = &types.RealServer_Config{
		Weight:  &wrappers.UInt32Value{Value: 1},
		Forward: types.ForwardMethod(forward),
	}
	server.HealthCheck = &types.RealServer_HealthCheck{
		Endpoint:      &wrappers.StringValue{Value: fmt.Sprintf("http://:%d/%s", u.HealthPort, u.HealthPath)},
		UpThreshold:   u.HealthUpThreshold,
		DownThreshold: u.HealthDownThreshold,
		Period:        ptypes.DurationProto(u.HealthPeriod),
		Timeout:       ptypes.DurationProto(u.HealthTimeout),
	}

	_, err := u.client.CreateServer(ctx, server)
	if status.Code(err) == codes.AlreadyExists {
		if _, err := u.client.UpdateServer(ctx, server); err != nil {
			return fmt.Errorf("unable to register with merlin: %v", err)
		}
	} else if err != nil {
		return fmt.Errorf("unable to register with merlin: %v", err)
	}

	return nil
}

func (u *updater) deregisterWithMerlin() {
	// drain
	func() {
		server := u.createBaseRealServer()
		server.Config = &types.RealServer_Config{Weight: &wrappers.UInt32Value{Value: 0}}

		ctx, cancel := context.WithTimeout(context.Background(), merlinTimeout)
		defer cancel()
		if _, err := u.client.UpdateServer(ctx, server); err != nil {
			log.Warnf("Unable to set weight to 0 in merlin for draining: %v", err)
			return
		}

		time.Sleep(u.DrainDelay)
	}()

	// deregister
	server := u.createBaseRealServer()
	ctx, cancel := context.WithTimeout(context.Background(), merlinTimeout)
	defer cancel()
	if _, err := u.client.DeleteServer(ctx, server); err != nil {
		log.Errorf("Unable to deregister server from merlin, please remove manually: %v", err)
	}
}

func (u *updater) createClient() error {
	if u.client != nil {
		return nil
	}

	c, err := grpc.Dial(u.Endpoint, grpc.WithInsecure(), grpc.WithBalancerName(roundrobin.Name))
	if err != nil {
		return fmt.Errorf("unable to create merlin grpc client: %v", err)
	}
	u.clientConn = c
	u.client = types.NewMerlinClient(c)

	return nil
}

func (u *updater) addVIP() error {
	if u.VIPInterface == "" || u.VIP == "" {
		return nil
	}
	return u.nl.addVIP(u.VIPInterface, u.VIP)
}

func (u *updater) removeVIP() error {
	if u.VIPInterface == "" || u.VIP == "" {
		return nil
	}
	return u.nl.removeVIP(u.VIPInterface, u.VIP)
}

func (u *updater) String() string {
	return "merlin attacher"
}
