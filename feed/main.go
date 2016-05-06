package main

import (
	feed "github.com/sky-uk/umc-ingress/feed/lib"
)

func main() {
	lb := struct {}{}
	client := struct {}{}
	controller := feed.NewController(lb, client)
	controller.Run()
}
