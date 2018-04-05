package merlin

import (
	"fmt"

	"context"
	"strings"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/wrappers"
	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/merlin/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Config for merlin updater.
type Config struct {
	Endpoint            string
	Timeout             time.Duration
	ServiceID           string
	HTTPSServiceID      string
	InstanceIP          string
	InstancePort        uint16
	InstanceHTTPSPort   uint16
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

type closeable interface {
	Close() error
}

type updater struct {
	Config
	clientFactory func(*Config) (types.MerlinClient, closeable, error)
	nl            netlinkWrapper
}

// New merlin updater.
func New(conf Config) (controller.Updater, error) {
	u := &updater{
		Config:        conf,
		clientFactory: createRealClient,
		nl:            &netlinkWrapperImpl{},
	}
	return u, nil
}

func createRealClient(conf *Config) (types.MerlinClient, closeable, error) {
	c, err := grpc.Dial(conf.Endpoint, grpc.WithInsecure(), grpc.WithBalancerName(roundrobin.Name))
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create merlin grpc client: %v", err)
	}
	client := types.NewMerlinClient(c)
	return client, c, nil
}

func (u *updater) Start() error {
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

func (u *updater) createBaseRealServer() *types.RealServer {
	return &types.RealServer{
		ServiceID: u.ServiceID,
		Key: &types.RealServer_Key{
			Ip:   u.InstanceIP,
			Port: uint32(u.InstancePort),
		},
	}
}

func (u *updater) createHTTPSFrom(orig *types.RealServer) *types.RealServer {
	server := proto.Clone(orig).(*types.RealServer)
	server.ServiceID = u.HTTPSServiceID
	server.Key.Port = uint32(u.InstanceHTTPSPort)
	return server
}

func (u *updater) registerWithMerlin() error {
	// create merlin server values
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

	// create gRPC client
	client, conn, err := u.clientFactory(&u.Config)
	if err != nil {
		return err
	}
	defer conn.Close()

	// register with merlin
	if err := u.registerServer(client, server, "http"); err != nil {
		return err
	}
	httpsServer := u.createHTTPSFrom(server)
	return u.registerServer(client, httpsServer, "https")
}

func (u *updater) registerServer(client types.MerlinClient, server *types.RealServer, detail string) error {
	if server.ServiceID == "" || server.Key.Port == 0 {
		log.Infof("Skipping merlin registration for %s", detail)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), u.Timeout)
	defer cancel()

	_, err := client.CreateServer(ctx, server)
	if status.Code(err) == codes.AlreadyExists {
		if _, err := client.UpdateServer(ctx, server); err != nil {
			return fmt.Errorf("unable to register to %s in merlin: %v", server.ServiceID, err)
		}
	} else if err != nil {
		return fmt.Errorf("unable to register to %s in merlin: %v", server.ServiceID, err)
	}

	log.Infof("Successfully registered to %s in merlin", server.ServiceID)
	return nil
}

func (u *updater) Stop() error {
	u.deregisterWithMerlin()
	return u.removeVIP()
}

func (u *updater) deregisterWithMerlin() {
	// create gRPC client
	client, conn, err := u.clientFactory(&u.Config)
	if err != nil {
		log.Warnf("Unable to create client connection to merlin: %v", err)
	}
	defer conn.Close()

	// create server keys
	server := u.createBaseRealServer()
	httpsServer := u.createHTTPSFrom(server)

	// drain
	u.updateServerForDraining(client, server)
	u.updateServerForDraining(client, httpsServer)
	time.Sleep(u.DrainDelay)

	// deregister
	u.deregisterServer(client, server)
	u.deregisterServer(client, httpsServer)
}

func (u *updater) updateServerForDraining(client types.MerlinClient, orig *types.RealServer) {
	if orig.ServiceID == "" || orig.Key.Port == 0 {
		return
	}
	server := proto.Clone(orig).(*types.RealServer)
	server.Config = &types.RealServer_Config{Weight: &wrappers.UInt32Value{Value: 0}}
	ctx, cancel := context.WithTimeout(context.Background(), u.Timeout)
	defer cancel()
	if _, err := client.UpdateServer(ctx, server); err != nil {
		log.Warnf("Draining failed for %s, unable to set weight to 0: %v", server.ServiceID, err)
	} else {
		log.Infof("Started draining for %s, server weight set to 0", server.ServiceID)
	}
}

func (u *updater) deregisterServer(client types.MerlinClient, server *types.RealServer) {
	if server.ServiceID == "" || server.Key.Port == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), u.Timeout)
	defer cancel()
	if _, err := client.DeleteServer(ctx, server); err != nil {
		log.Errorf("Unable to deregister from %s in merlin, please remove manually: %v", server.ServiceID, err)
	} else {
		log.Infof("Successfully deregistered from %s in merlin", server.ServiceID)
	}
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

func (u *updater) Update(controller.IngressEntries) error {
	return nil
}

func (u *updater) Health() error {
	return nil
}

func (u *updater) String() string {
	return "merlin attacher"
}
