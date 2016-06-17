/*
Package k8s implements a client for communicating with a Kubernetes apiserver. It is intended
to support an ingress controller, so it is limited to the types needed.

The types are copied from the stable api of the Kubernetes 1.3 release.
*/
package k8s

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"bufio"

	"time"

	"net"

	"io"

	log "github.com/Sirupsen/logrus"
)

const (
	ingressPath       = "/apis/extensions/v1beta1/ingresses"
	servicePath       = "/api/v1/services"
	initialRetryDelay = time.Millisecond * 100
	maxRetryDelay     = time.Second * 60
)

// Client for connecting to a Kubernetes cluster.
// Watchers will receive a notification whenever the client connects to the API server,
// including reconnects, to notify that there may be new ingresses that need to be retrieved.
// It's intended that client code will call the getters to retrieve the current state when notified.
type Client interface {
	// GetIngresses returns all the ingresses in the cluster.
	GetIngresses() ([]Ingress, error)

	// GetServices returns all the services in the cluster.
	GetServices() ([]Service, error)

	// WatchIngresses watches for updates to ingresses and notifies the Watcher.
	WatchIngresses() Watcher

	// WatchServices watches for updates to services and notifies the Watcher.
	WatchServices() Watcher
}

type client struct {
	baseURL string
	caCert  []byte
	token   string
	http    *http.Client
}

// Conf is the config for the k8s client.
type Conf struct {
	APIServerURL string
	CaCert       []byte
	Token        string
	ClientCert   []byte
	ClientKey    []byte
}

// New creates a client for the kubernetes apiserver.
func New(conf Conf) (Client, error) {
	parsedURL, err := url.Parse(conf.APIServerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url %s: %v", conf.APIServerURL, err)
	}
	baseURL := strings.TrimSuffix(parsedURL.String(), "/")

	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(conf.CaCert); !ok {
		return nil, fmt.Errorf("unable to parse ca certificate")
	}

	// same as net.DefaultTransport, with k8s CAs added
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool},
		Dial: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).Dial,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if len(conf.ClientCert) > 0 && len(conf.ClientKey) > 0 {
		cert, err := tls.X509KeyPair(conf.ClientCert, conf.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("unable to create client certificate: %v", err)
		}
		tr.TLSClientConfig.Certificates = []tls.Certificate{cert}
	}

	httpClient := &http.Client{Transport: tr}

	log.Debugf("Constructing client with url: %s, token: %s, caCert: %v",
		baseURL, conf.Token, string(conf.CaCert))

	return &client{
			baseURL: baseURL,
			caCert:  conf.CaCert,
			token:   conf.Token,
			http:    httpClient},
		nil
}

func (c *client) GetIngresses() ([]Ingress, error) {
	var ingressList IngressList
	if err := c.requestAndUnmarshall(ingressPath, &ingressList); err != nil {
		return nil, err
	}
	return ingressList.Items, nil
}

func (c *client) GetServices() ([]Service, error) {
	var serviceList ServiceList
	if err := c.requestAndUnmarshall(servicePath, &serviceList); err != nil {
		return nil, err
	}
	return serviceList.Items, nil
}

func (c *client) WatchIngresses() Watcher {
	return c.watch(ingressPath)
}

func (c *client) WatchServices() Watcher {
	return c.watch(servicePath)
}

func (c *client) watch(resourcePath string) Watcher {
	log.Debugf("Adding watcher for %s", resourcePath)

	w := newWatcher()
	w.notWatching()
	request := c.createRetryingWatchRequest(resourcePath, w.done)

	go func() {
		for watch(w, request) {
		}

		log.Debug("Watch %s has stopped", resourcePath)
	}()

	return w
}

func (c *client) createRetryingWatchRequest(resourcePath string, doneCh <-chan struct{}) func() (*http.Response, error) {
	request := func() (*http.Response, error) {
		resourceVersion, err := c.getResourceVersion(resourcePath)
		if err != nil {
			return nil, err
		}
		log.Debugf("Found %s resource version '%s'", resourcePath, resourceVersion)
		return c.request(resourcePath + "?watch=true&resourceVersion=" + resourceVersion)
	}

	retryRequest := func() (*http.Response, error) {
		return retryRequest(doneCh, request)
	}

	return retryRequest
}

type genericList struct {
	ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
}

func (c *client) getResourceVersion(resourcePath string) (string, error) {
	var ingresses genericList
	err := c.requestAndUnmarshall(resourcePath, &ingresses)
	if err != nil {
		return "", err
	}
	return ingresses.ResourceVersion, nil
}

// watch returns true if it should be retried, false if the watcher has terminated.
func watch(w *watcher, request func() (*http.Response, error)) bool {
	resp, err := request()
	if err != nil {
		log.Infof("Watcher could not make request, shutting down: %v", err)
		return false
	}
	defer resp.Body.Close()

	w.watching()
	defer w.notWatching()
	log.Infof("Watching %v", resp.Request.URL)

	// send an update for a successful watch start
	w.updates <- struct{}{}

	updateCh := make(chan interface{})
	go handleLongPoll(resp.Body, updateCh)

	for {
		select {
		case <-w.done:
			log.Debug("Watcher is done, stopping watch")
			return false
		case update := <-updateCh:
			if update == nil {
				log.Info("Long poll terminated, will reconnect")
				return true
			}
			log.Debug("Noticed update, sending notification to watcher")
			w.updates <- update
		}
	}
}

func handleLongPoll(r io.Reader, updateCh chan<- interface{}) {
	defer close(updateCh)

	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		event := scanner.Text()
		log.Debugf("Received event %s", event)
		updateCh <- struct{}{}
	}

	if err := scanner.Err(); err != nil {
		log.Debugf("Error while watching events, closing update channel: %v", err)
	}
}

func retryRequest(doneCh <-chan struct{}, request func() (*http.Response, error)) (*http.Response, error) {
	respCh := make(chan *http.Response)
	delay := initialRetryDelay
	go func() {
		defer close(respCh)
		t := time.NewTimer(0)

		for {
			select {
			case <-doneCh:
				log.Debug("Done, stopping retry")
				return
			case <-t.C:
				resp, err := request()

				if err != nil {
					log.Warnf("Failed to request watch, will retry in %v: %v", delay, err)
					t.Reset(delay)
					if delay < maxRetryDelay {
						delay = delay * 2
					}
					break
				}

				log.Debugf("Succeeded watching %v", resp.Request.URL)
				respCh <- resp
				return
			}
		}
	}()

	resp := <-respCh
	if resp == nil {
		return nil, fmt.Errorf("request retry cancelled")
	}
	return resp, nil
}

func (c *client) requestAndUnmarshall(path string, val interface{}) error {
	resp, err := c.request(path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	err = c.unmarshal(resp.Body, val)
	if err != nil {
		return err
	}
	return nil
}

func (c *client) unmarshal(r io.Reader, val interface{}) error {
	body, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	log.Debugf("<-k8s: %v", string(body))

	err = json.Unmarshal(body, val)
	if err != nil {
		return err
	}

	log.Debugf("marshalled to %v", val)

	return nil
}

func (c *client) request(path string) (*http.Response, error) {
	endpoint := c.baseURL + path
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	if c.token != "" {
		req.Header.Add("Authorization", "Bearer "+c.token)
	}

	log.Debugf("k8s<-: %v", *req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}

	log.Debugf("<-k8s: Status code %d", resp.StatusCode)
	if resp.StatusCode < 200 || 300 <= resp.StatusCode {
		if strings.Contains(path, "?watch") && resp.StatusCode == http.StatusGone {
			log.Debug("Watch returned 410 (Gone) due to k8s having no events yet, ignoring")
		} else {
			resp.Body.Close()
			return nil, fmt.Errorf("GET %s returned %v", endpoint, *resp)
		}
	}

	return resp, nil
}

func (c *client) String() string {
	return fmt.Sprintf("[k8s @ %s]", c.baseURL)
}
