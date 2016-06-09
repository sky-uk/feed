package k8s

import (
	"testing"

	"encoding/json"
	"net/http"
	"net/http/httptest"

	"time"

	"strconv"

	"github.com/stretchr/testify/assert"

	"sync"

	log "github.com/Sirupsen/logrus"
)

const smallWaitTime = time.Millisecond * 50

func TestInvalidUrlReturnsError(t *testing.T) {
	_, err := New("%gh&%ij", []byte{}, "")
	assert.Error(t, err)
}

func TestRetrievesIngressesFromKubernetes(t *testing.T) {
	assert := assert.New(t)

	ingressFixture := createIngressesFixture()
	handler, _ := handleGetIngresses(ingressFixture)
	ts := httptest.NewTLSServer(handler)
	defer ts.Close()

	client, err := New(ts.URL, caCert, testAuthToken)
	assert.NoError(err)

	ingresses, err := client.GetIngresses()
	assert.NoError(err)

	assert.Equal(ingressFixture.Items, ingresses)
}

func TestRetrievesServicesFromKubernetes(t *testing.T) {
	assert := assert.New(t)

	servicesFixture := createServicesFixture()
	handler, _ := handleGetServices(servicesFixture)
	ts := httptest.NewTLSServer(handler)
	defer ts.Close()

	client, err := New(ts.URL, caCert, testAuthToken)
	assert.NoError(err)

	services, err := client.GetServices()
	assert.NoError(err)

	assert.Equal(servicesFixture.Items, services)
}

func TestErrorIfNon200StatusCode(t *testing.T) {
	assert := assert.New(t)

	ts := httptest.NewTLSServer(http.NotFoundHandler())
	defer ts.Close()

	client, err := New(ts.URL, caCert, testAuthToken)
	assert.NoError(err)

	_, err = client.GetIngresses()
	assert.Error(err)
}

func TestErrorIfInvalidJson(t *testing.T) {
	assert := assert.New(t)

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte("{__Asdfkez--garbagel"))
		assert.NoError(err)
	}))
	defer ts.Close()

	client, err := New(ts.URL, caCert, testAuthToken)
	assert.NoError(err)

	_, err = client.GetIngresses()
	assert.Error(err)
}

func TestWatchesIngressUpdatesFromKubernetes(t *testing.T) {
	assert := assert.New(t)

	var tests = []struct {
		path        string
		fixture     versioned
		createWatch func(Client) Watcher
	}{
		{
			ingressPath,
			createIngressesFixture(),
			func(client Client) Watcher {
				return client.WatchIngresses()
			},
		},
		{
			servicePath,
			createServicesFixture(),
			func(client Client) Watcher {
				return client.WatchServices()
			},
		},
	}

	for _, test := range tests {
		// given: initial resource and a client
		resource := test.fixture
		resource.setVersion("99")

		handler, eventChan := handleGet(test.path, resource)
		ts := httptest.NewTLSServer(handler)
		defer ts.Close()
		defer close(eventChan)

		// when: watch resource
		client, err := New(ts.URL, caCert, testAuthToken)
		assert.NoError(err)
		watcher := test.createWatch(client)

		// consume watcher updates into buffer so the watcher isn't blocked
		updates := bufferChan(watcher.Updates())

		// then
		// single update to notify of existing ingresses
		eventChan <- okEvent
		assert.Equal(1, countUpdates(updates), "received update for initial ingresses")
		assert.NoError(watcher.Health())

		// send an old resource to test resourceVersion is used
		oldEvent := dummyEvent{Name: "old-ingress", ResourceVersion: 80}
		eventChan <- oldEvent
		assert.Equal(0, countUpdates(updates), "ignore old-ingress")

		// send a disconnect event to terminate long poll and ensure that watcher reconnects
		eventChan <- disconnectEvent
		assertNotHealthy(t, watcher)
		eventChan <- okEvent
		assert.Equal(1, countUpdates(updates), "received update for reconnect")
		assert.NoError(watcher.Health())

		// send a modified ingress
		modifiedIngressEvent := dummyEvent{Name: "modified-ingress", ResourceVersion: 100}
		eventChan <- modifiedIngressEvent
		assert.Equal(1, countUpdates(updates), "got modified-ingress")

		// send 500 bad request to check retry logic
		// first reset the resource version
		handlerMutex.Lock()
		resource.setVersion("110")
		handlerMutex.Unlock()

		// then send 500s followed by 410 gone from a k8s restart
		eventChan <- disconnectEvent
		eventChan <- badEvent
		eventChan <- badEvent
		assertNotHealthy(t, watcher)
		eventChan <- goneEvent
		time.Sleep(smallWaitTime * 10)
		assert.Equal(1, countUpdates(updates), "received update for reconnect")
		assert.NoError(watcher.Health())

		// send modified ingress again, should be ignored
		eventChan <- modifiedIngressEvent
		assert.Equal(0, countUpdates(updates), "should have ignored modified ingress after reconnect")

		// send new modified ingress, should cause an update
		modified2IngressEvent := dummyEvent{Name: "modified-2-ingress", ResourceVersion: 127}
		eventChan <- modified2IngressEvent
		assert.Equal(1, countUpdates(updates), "got modified-2-ingress")

		close(watcher.Done())
	}
}

type versioned interface {
	setVersion(string)
}

func (i *IngressList) setVersion(v string) {
	i.ResourceVersion = v
}

func (s *ServiceList) setVersion(v string) {
	s.ResourceVersion = v
}

func assertNotHealthy(t *testing.T, w Watcher) {
	// assumes retry time is > smallWaitTime, letting us query an unhealthy watcher while it waits
	time.Sleep(smallWaitTime)
	assert.Error(t, w.Health(), "watcher should not be watching")
}

func bufferChan(ch <-chan interface{}) <-chan interface{} {
	buffer := make(chan interface{}, 100)
	go func() {
		defer close(buffer)
		for update := range ch {
			buffer <- update
		}
	}()
	return buffer
}

func countUpdates(updates <-chan interface{}) int {
	t := time.NewTimer(smallWaitTime * 2)
	var count int
	for {
		select {
		case <-t.C:
			return count
		case <-updates:
			count++
		}
	}
}

type dummyEvent struct {
	Name            string
	ResourceVersion int
}

var handlerMutex = &sync.Mutex{}

func handleGetIngresses(ingressList *IngressList) (http.Handler, chan<- dummyEvent) {
	return handleGet(ingressPath, ingressList)
}

func handleGetServices(serviceList *ServiceList) (http.Handler, chan<- dummyEvent) {
	return handleGet(servicePath, serviceList)
}

func handleGet(path string, fixture interface{}) (http.Handler, chan<- dummyEvent) {
	eventChan := make(chan dummyEvent, 100)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Debug("test: Handling ingress request")
		defer log.Debug("test: Finished handling ingress request")

		if r.URL.Path != path {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if !validAuthToken(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		if r.FormValue("watch") == "true" {
			handleLongPollWatch(eventChan, w, r)
		} else {
			handlerMutex.Lock()
			defer handlerMutex.Unlock()
			writeAsJSON(fixture, w)
		}

	}), eventChan
}

func validAuthToken(r *http.Request) bool {
	auths := r.Header["Authorization"]
	if len(auths) != 1 {
		return false
	}

	return "Bearer "+testAuthToken == r.Header["Authorization"][0]
}

// various events for sending to client
var okEvent = dummyEvent{Name: "OK"}
var disconnectEvent = dummyEvent{Name: "DISCONNECT"}
var badEvent = dummyEvent{Name: "BAD"}
var goneEvent = dummyEvent{Name: "GONE"}

func handleLongPollWatch(eventChan <-chan dummyEvent, w http.ResponseWriter, r *http.Request) {
	resourceVersion, _ := strconv.Atoi(r.FormValue("resourceVersion"))

	for {
		select {
		case event := <-eventChan:
			log.Debug("test: handling %v", event)
			if event.Name == "" {
				return
			}
			if event == okEvent {
				w.WriteHeader(http.StatusOK)
				flush(w)
			}
			if event == disconnectEvent {
				return
			}
			if event == badEvent {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if event == goneEvent {
				w.WriteHeader(http.StatusGone)
				flush(w)
			}
			if event.ResourceVersion > resourceVersion {
				writeAsJSON(event, w)
			}
		}
	}
}

func writeAsJSON(val interface{}, w http.ResponseWriter) {
	bytes, err := json.Marshal(val)
	if err != nil {
		panic(err)
	}
	bytes = append(bytes, '\n')
	_, err = w.Write(bytes)
	if err != nil {
		panic(err)
	}

	flush(w)
}

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

const (
	testIngressHost = "foo.sky.com"
	testIngressPath = "/foo"
	testSvcName     = "foo-svc"
	testSvcPort     = 80
	testAuthToken   = "validtoken"
)

func createIngressesFixture() *IngressList {
	paths := []HTTPIngressPath{HTTPIngressPath{
		Path: testIngressPath,
		Backend: IngressBackend{
			ServiceName: testSvcName,
			ServicePort: FromInt(testSvcPort),
		},
	}}
	return &IngressList{Items: []Ingress{
		Ingress{
			ObjectMeta: ObjectMeta{Name: "foo-ingress"},
			Spec: IngressSpec{
				Rules: []IngressRule{IngressRule{
					Host: testIngressHost,
					IngressRuleValue: IngressRuleValue{HTTP: &HTTPIngressRuleValue{
						Paths: paths,
					}},
				}},
			},
		},
	}}
}

func createServicesFixture() *ServiceList {
	return &ServiceList{Items: []Service{
		Service{
			ObjectMeta: ObjectMeta{Name: "foo-service"},
			Spec: ServiceSpec{
				Type: ServiceTypeClusterIP,
				Ports: []ServicePort{ServicePort{
					Name:       "http",
					Protocol:   ProtocolTCP,
					Port:       8080,
					TargetPort: FromInt(80),
				}},
				Selector:        map[string]string{"app": "foo-app"},
				ClusterIP:       "10.254.0.99",
				SessionAffinity: ServiceAffinityNone,
			},
		},
	}}
}

// From testcert.go, used by httptest for TLS
var caCert = []byte(`-----BEGIN CERTIFICATE-----
MIICEzCCAXygAwIBAgIQMIMChMLGrR+QvmQvpwAU6zANBgkqhkiG9w0BAQsFADAS
MRAwDgYDVQQKEwdBY21lIENvMCAXDTcwMDEwMTAwMDAwMFoYDzIwODQwMTI5MTYw
MDAwWjASMRAwDgYDVQQKEwdBY21lIENvMIGfMA0GCSqGSIb3DQEBAQUAA4GNADCB
iQKBgQDuLnQAI3mDgey3VBzWnB2L39JUU4txjeVE6myuDqkM/uGlfjb9SjY1bIw4
iA5sBBZzHi3z0h1YV8QPuxEbi4nW91IJm2gsvvZhIrCHS3l6afab4pZBl2+XsDul
rKBxKKtD1rGxlG4LjncdabFn9gvLZad2bSysqz/qTAUStTvqJQIDAQABo2gwZjAO
BgNVHQ8BAf8EBAMCAqQwEwYDVR0lBAwwCgYIKwYBBQUHAwEwDwYDVR0TAQH/BAUw
AwEB/zAuBgNVHREEJzAlggtleGFtcGxlLmNvbYcEfwAAAYcQAAAAAAAAAAAAAAAA
AAAAATANBgkqhkiG9w0BAQsFAAOBgQCEcetwO59EWk7WiJsG4x8SY+UIAA+flUI9
tyC4lNhbcF2Idq9greZwbYCqTTTr2XiRNSMLCOjKyI7ukPoPjo16ocHj+P3vZGfs
h1fIw3cSS2OolhloGw/XM6RWPWtPAlGykKLciQrBru5NAPvCMsb/I1DAceTiotQM
fblo6RBxUQ==
-----END CERTIFICATE-----`)
