package ingress

import (
	"fmt"

	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/controller"
)

type updater struct {
	frontend Frontend
	proxy    Proxy
}

// New creates an updater for the external frontend and internal proxy.
func New(frontend Frontend, proxy Proxy) controller.Updater {
	return &updater{
		frontend: frontend,
		proxy:    proxy,
	}
}

func (u *updater) Start() error {

	err := u.frontend.Attach()
	if err != nil {
		return fmt.Errorf("unable to attach to front end %v", err)
	}

	err = u.proxy.Start()

	if err != nil {
		return fmt.Errorf("unable to start proxy: %v", err)
	}

	return nil
}

func (u *updater) Stop() error {
	if err := u.frontend.Detach(); err != nil {
		log.Warnf("Error while detaching front end: %v", err)
	}

	if err := u.proxy.Stop(); err != nil {
		log.Warnf("Error while stopping proxy: %v", err)
	}

	return nil
}

func (u *updater) Health() error {
	if err := u.proxy.Health(); err != nil {
		return err
	}

	if err := u.frontend.Health(); err != nil {
		return err
	}

	return nil
}

func (u *updater) Update(update controller.IngressUpdate) error {
	updated, err := u.proxy.Update(update)
	if err != nil {
		return err
	}

	if updated {
		log.Info("Load balancer updated")
	} else {
		log.Info("No changes")
	}

	return nil
}
