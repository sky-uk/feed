package haproxy

import (
	"os/exec"

	"github.com/sky-uk/feed/controller"
	log "github.com/Sirupsen/logrus"
	"fmt"
	"os"
)

// Config of haproxy updater.
type Config struct {
	// Binary is the location of the haproxy binary.
	Binary string
}

type haproxy struct {
	Config
	cmd *exec.Cmd
}

// New creates a new updater that manages an haproxy instance for proxying k8s ingress.
func New(conf Config) controller.Updater {
	cmd := exec.Command(conf.Binary)
	cmd.Stdout = log.StandardLogger().Writer()
	cmd.Stderr = log.StandardLogger().Writer()

	return &haproxy{
		Config: conf,
		cmd: cmd,
	}
}

func (h *haproxy) Start() error {
	if err := h.logHaproxyVersion(); err != nil {
		return err
	}

	if err := h.initialiseHaproxyConf(); err != nil {
		return fmt.Errorf("unable to initialise haproxy config: %v", err)
	}

	//if err := n.nginx.Start(); err != nil {
	//	return fmt.Errorf("unable to start nginx: %v", err)
	//}
	//
	//n.running.Set(true)
	//go n.waitForNginxToFinish()
	//
	//time.Sleep(nginxStartDelay)
	//if !n.running.Get() {
	//	return errors.New("nginx died shortly after starting")
	//}
	//
	//go n.periodicallyUpdateMetrics()
	//go n.backgroundSignaller()

	return nil
}

func (h *haproxy) logHaproxyVersion() error {
	cmd := exec.Command(h.Binary, "-v")
	cmd.Stdout = log.StandardLogger().Writer()
	cmd.Stderr = log.StandardLogger().Writer()
	return cmd.Run()
}

func (h *haproxy) initialiseHaproxyConf() error {
	err := os.Remove(h.nginxConfFile())
	if err != nil {
		log.Debugf("Can't remove nginx.conf: %v", err)
	}
	_, err = n.update(controller.IngressUpdate{Entries: []controller.IngressEntry{}})
	return err
}

func (h *haproxy) Stop() error {
	return nil
}

func (h *haproxy) Update(controller.IngressUpdate) error {
	return nil
}

func (h *haproxy) Health() error {
	return nil
}
