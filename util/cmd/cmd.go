package cmd

import (
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"fmt"

	log "github.com/Sirupsen/logrus"
	"github.com/prometheus/client_golang/prometheus"
)

// Pulse represents something alive whose health can be checked.
type Pulse interface {
	// Health returns the current health, nil if healthy.
	Health() error
	// Stop the thing that's alive.
	Stop() error
}

// ConfigureHealthPort is used to expose the health over http
func ConfigureHealthPort(pulse Pulse, healthPort int) {
	http.HandleFunc("/health", healthHandler(pulse))
	http.Handle("/metrics", prometheus.Handler())

	go func() {
		log.Error(http.ListenAndServe(":"+strconv.Itoa(healthPort), nil))
		log.Info(pulse.Stop())
		os.Exit(-1)
	}()
}

func healthHandler(pulse Pulse) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := pulse.Health(); err != nil {
			log.Warnf("Returning unhealthy %v", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, fmt.Sprintf("%v\n", err))
			return
		}

		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok\n")
	}
}

// AddSignalHandler allows the  controller to shutdown gracefully by respecting SIGTERM
func AddSignalHandler(pulse Pulse) {
	c := make(chan os.Signal, 1)
	// SIGTERM is used by Kubernetes to gracefully stop pods.
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for sig := range c {
			log.Infof("Signalled %v, shutting down gracefully", sig)
			err := pulse.Stop()
			if err != nil {
				log.Errorf("Error while stopping: %v", err)
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
