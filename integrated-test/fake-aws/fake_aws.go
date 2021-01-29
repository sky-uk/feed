package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	logr "github.com/sirupsen/logrus"
)

var signalsCh chan os.Signal

func main() {
	doneCh := make(chan bool, 1)
	startServer(9000)
	handleSignals(doneCh)
	<-doneCh
	logr.Info("Shut down complete")
}

func startServer(adminPort int) *http.Server {
	serverMux := http.NewServeMux()
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", adminPort),
		Handler: serverMux,
	}
	serverMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		logr.Infof("Received request: %v", r.RequestURI)
		switch requestUri := r.RequestURI; requestUri {
		case "/dynamic/instance-identity/document":
			_, err := w.Write([]byte("{ \"instanceId\" : \"i-1234567890abcdef0\"}"))
			if err != nil {
				logr.Errorf("Error while writing response: %v", err)
			}
		default:
			bodyContent, err := ioutil.ReadAll(r.Body)
			defer r.Body.Close()
			if err != nil {
				logr.Errorf("Error while reading body, %v", err)
			}
			logr.Infof("Request body: %v", string(bodyContent))
			w.WriteHeader(http.StatusNoContent)
		}
	})

	go func() {
		logr.Infof("Starting to listen at: http://0.0.0.0%s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logr.Fatalf("Unexpected failure when starting the server: %v", err)
		}
	}()
	return server
}

func handleSignals(doneCh chan bool) {
	signalsCh = make(chan os.Signal, 1)
	signal.Notify(signalsCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-signalsCh
		logr.Infof("Received signal %s", sig)
		doneCh <- true
	}()
}
