package main

import (
	"os"

	"github.com/golang/glog"
	"github.com/sky-uk/feed/ingress"
)

func main() {
	controller := ingress.NewController(nil, nil)

	if err := controller.Run(); err != nil {
		glog.Fatalf("failed to start controller: %v", err)
		os.Exit(-1)
	}
}
