package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"
)

func main() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGQUIT, syscall.SIGHUP)

	if os.Args[1] == "-v" {
		fmt.Println("Asked for version")
		os.Exit(0)
	}

	if os.Args[1] == "-t" {
		fmt.Println("Asked for config validation")
		os.Exit(0)
	}

	startupMarkerFilename := startupMarkerFilename()
	err := ioutil.WriteFile(startupMarkerFilename, []byte("started!"), os.ModePerm)
	if err != nil {
		panic(err)
	}

	timer := time.NewTimer(5 * time.Second)

	select {
	case sig := <-sigCh:
		if sig == syscall.SIGQUIT {
			time.Sleep(500 * time.Millisecond)
			fmt.Println("Received sigquit, doing graceful shutdown")
			os.Exit(0)
		}

		if sig == syscall.SIGHUP {
			err := ioutil.WriteFile(startupMarkerFilename, []byte("reloaded!"), os.ModePerm)
			if err != nil {
				panic(err)
			}
		}

	case <-timer.C:
		fmt.Println("Quit after 5 seconds of nada")
		os.Exit(-1)
	}
}

func startupMarkerFilename() string {
	filename := strings.Split(os.Args[2], "/")
	filename = filename[:len(filename)-1]
	filename = append(filename, "nginx-log")
	return "/" + path.Join(filename...)
}
