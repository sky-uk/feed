package cdns

import (
	"errors"
	"github.com/sky-uk/feed/controller"
	"github.com/sky-uk/feed/dns/adapter"
	log "github.com/sirupsen/logrus"

)

//NewUpdater for the Google Cloud DNS
func NewUpdater(config Config, adapter adapter.FrontendAdapter) (controller.Updater, error) {
	//TODO: initMetrics
	return &dnsUpdater{
		config:  config,
		adapter: adapter,
	}, nil
}

type dnsUpdater struct {
	config              Config
	adapter             adapter.FrontendAdapter
	schemeToFrontendMap map[string]adapter.DNSDetails
	domain              string
}

func (u *dnsUpdater) Start() error {
	log.Infof("Starting %s", u)
	schemeToFrontendMap, err := u.adapter.Initialise()
	if err != nil {
		return err
	}
	u.schemeToFrontendMap = schemeToFrontendMap
}

func (u *dnsUpdater) Stop() error {
	return errors.New("not implemented")
}

func (u *dnsUpdater) Update(controller.IngressEntries) error {
	return errors.New("not implemented")
}

func (u *dnsUpdater) Health() error {
	return errors.New("not implemented")
}

func (u *dnsUpdater) String() string {
	return "cdns updater"
}
