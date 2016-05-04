package main

import (
	feed "github.com/sky-uk/umc-infra/feed/lib"
)

func main() {
	controller := feed.NewController()
	controller.Run()
}
