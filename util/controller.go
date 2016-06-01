package util

import (
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"fmt"

	log "github.com/Sirupsen/logrus"
)

// Controller interface for all ingress controllers
type Controller interface {
	// Run the controller, returning immediately after it starts or an error occurs.
	Start() error
	// Stop the controller, blocking until it stops or an error occurs.
	Stop() error
	// Healthy returns true for a healthy controller, false for unhealthy.
	Health() error
}

// ConfigureHealthPort is used to expose the controllers health over http
func ConfigureHealthPort(controller Controller, healthPort int) {
	http.HandleFunc("/health", checkHealth(controller))

	go func() {
		log.Error(http.ListenAndServe(":"+strconv.Itoa(healthPort), nil))
		log.Info(controller.Stop())
		os.Exit(-1)
	}()
}

func checkHealth(controller Controller) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := controller.Health(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, fmt.Sprintf("%v\n", err))
			return
		}

		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok\n")
	}
}

// AddSignalHandler allows the  controller to shutdown gracefully by respecting SIGTERM
func AddSignalHandler(controller Controller) {
	c := make(chan os.Signal, 1)
	// SIGTERM is used by Kubernetes to gracefully stop pods.
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for sig := range c {
			log.Infof("Signalled %v, shutting down gracefully", sig)
			err := controller.Stop()
			if err != nil {
				log.Error("Error while stopping controller: ", err)
				os.Exit(-1)
			}
			os.Exit(0)
		}
	}()
}

// ConfigureLogging sets logging to Stdout and manages setting debug level
func ConfigureLogging(debug bool) {
	// logging is the main output, so write it all to stdout
	log.SetOutput(os.Stdout)
	if debug {
		log.SetLevel(log.DebugLevel)
	}
}
