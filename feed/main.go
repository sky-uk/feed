package main

import (
	"os"

	"github.com/golang/glog"
	feed "github.com/sky-uk/umc-ingress/feed/lib"
)

func main() {
	lb := struct{}{}
	client := struct{}{}
	controller := feed.NewController(lb, client)

	if err := controller.Run(); err != nil {
		glog.Fatalf("failed to start controller: %v", err)
		os.Exit(-1)
	}
}
