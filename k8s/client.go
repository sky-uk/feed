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

	log "github.com/Sirupsen/logrus"
)

// Client for connecting to a Kubernetes cluster.
type Client interface {
	GetIngresses() ([]Ingress, error)
	WatchIngresses(Watcher) error
}

type client struct {
	baseURL string
	caCert  []byte
	token   string
	http    *http.Client
}

// New creates a client for the kubernetes apiserver.
func New(apiServerURL string, caCert []byte, token string) (Client, error) {
	parsedURL, err := url.Parse(apiServerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url %s: %v", apiServerURL, err)
	}
	baseURL := strings.TrimSuffix(parsedURL.String(), "/")

	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(caCert); !ok {
		return nil, fmt.Errorf("unable to parse ca certificate")
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}
	httpClient := &http.Client{Transport: tr}

	log.Debugf("Constructing client with url: %s, token: %s, caCert: %v",
		baseURL, token, string(caCert))

	return &client{
			baseURL: baseURL,
			caCert:  caCert,
			token:   token,
			http:    httpClient},
		nil
}

func (c *client) GetIngresses() ([]Ingress, error) {
	endpoint := c.baseURL + "/apis/extensions/v1beta1/ingresses"
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+c.token)

	log.Debugf("k8s<-: %v", *req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || 300 <= resp.StatusCode {
		return nil, fmt.Errorf("GET %s returned %v", endpoint, resp)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	log.Debugf("<-k8s:  %v", string(body))

	var ingressList IngressList
	err = json.Unmarshal(body, &ingressList)
	if err != nil {
		return nil, err
	}

	log.Debugf("marshalled to %v", ingressList)

	return ingressList.Items, nil
}

func (c *client) WatchIngresses(w Watcher) error {
	log.Info("Watching ingresses")
	return nil
}

func (c *client) String() string {
	return fmt.Sprintf("[k8s @ %s]", c.baseURL)
}
