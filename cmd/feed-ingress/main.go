package main

import (
	"os"
	"os/signal"
	//
	log "github.com/Sirupsen/logrus"
	"github.com/sky-uk/feed/ingress"
	"github.com/sky-uk/feed/k8s"
)

func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	lb := ingress.NewLB()
	client := k8s.NewClient()
	controller := ingress.NewController(lb, client)

	go func() {
		for sig := range c {
			log.Infof("Signalled {}, shutting down.", sig)
			controller.Stop()
		}
	}()

	controller.Run()
}
