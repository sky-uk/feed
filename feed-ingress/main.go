package main

import (
	_ "net/http/pprof"

	"github.com/sky-uk/feed/feed-ingress/cmd"
)

func main() {
	cmd.Execute()
}
