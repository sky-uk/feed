package main

import (
	"github.com/sky-uk/feed/ingress"
)

func main() {
	controller := ingress.NewController(nil, nil)
	controller.Run()
}
