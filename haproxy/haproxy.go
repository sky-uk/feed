package haproxy

import "github.com/sky-uk/feed/controller"

type haproxy struct {
}

// New creates a new updater that manages an haproxy instance for proxying k8s ingress.
func New() controller.Updater {
	return &haproxy{}
}

func (h *haproxy) Start() error {
	return nil
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
