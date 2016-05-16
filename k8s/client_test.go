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

	assertEqualIngresses(t, ingressFixture.Items, ingresses)
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

	// given: initial ingresses and a client
	ingresses := createIngressesFixture()
	ingresses.ResourceVersion = "99"

	handler, eventChan := handleGetIngresses(ingresses)
	ts := httptest.NewTLSServer(handler)
	defer ts.Close()
	defer close(eventChan)

	// when: watch ingresses
	client, err := New(ts.URL, caCert, testAuthToken)
	assert.NoError(err)

	watcher := NewWatcher()
	defer close(watcher.Done())
	err = client.WatchIngresses(watcher)
	assert.NoError(err)

	// consume watcher updates into buffer so the watcher isn't blocked
	updates := bufferChan(watcher.Updates(), watcher.Done())

	// then
	// single update to notify of existing ingresses
	okEvent := dummyEvent{Name: "OK"}
	eventChan <- okEvent
	assert.Equal(1, countUpdates(updates), "received update for initial ingresses")

	// send an old resource to test resourceVersion is used
	oldEvent := dummyEvent{Name: "old-ingress", ResourceVersion: 80}
	eventChan <- oldEvent
	assert.Equal(0, countUpdates(updates), "ignore old-ingress")

	// send a disconnect event to terminate long poll and ensure that watcher reconnects
	disconnectEvent := dummyEvent{Name: "DISCONNECT"}
	eventChan <- disconnectEvent
	eventChan <- okEvent
	assert.Equal(1, countUpdates(updates), "received update for reconnect")

	// send a modified ingress
	modifiedIngressEvent := dummyEvent{Name: "modified-ingress", ResourceVersion: 100}
	eventChan <- modifiedIngressEvent
	assert.Equal(1, countUpdates(updates), "got modified-ingress")

	// send 500 bad request to check retry logic
	// first reset the resource version
	handlerMutex.Lock()
	ingresses.ResourceVersion = "110"
	handlerMutex.Unlock()

	// then send 500s followed by 410 gone from a k8s restart
	eventChan <- disconnectEvent
	badEvent := dummyEvent{Name: "BAD"}
	goneEvent := dummyEvent{Name: "GONE"}
	eventChan <- badEvent
	eventChan <- badEvent
	eventChan <- goneEvent
	time.Sleep(smallWaitTime * 10)
	assert.Equal(1, countUpdates(updates), "received update for reconnect")

	// send modified ingress again, should be ignored
	eventChan <- modifiedIngressEvent
	assert.Equal(0, countUpdates(updates), "should have ignored modified ingress after reconnect")

	// send new modified ingress, should cause an update
	modified2IngressEvent := dummyEvent{Name: "modified-2-ingress", ResourceVersion: 127}
	eventChan <- modified2IngressEvent
	assert.Equal(1, countUpdates(updates), "got modified-2-ingress")
}

func bufferChan(ch <-chan interface{}, done <-chan struct{}) <-chan interface{} {
	buffer := make(chan interface{}, 100)
	go func() {
		defer close(buffer)
		for {
			select {
			case <-done:
				return
			case update := <-ch:
				buffer <- update
			}
		}
	}()
	return buffer
}

func countUpdates(updates <-chan interface{}) int {
	timeout := make(chan struct{})
	go func() {
		time.Sleep(smallWaitTime)
		close(timeout)
	}()

	var count int
	for {
		var finish bool
		select {
		case <-timeout:
			finish = true
		case <-updates:
			count++
		}
		if finish {
			break
		}
	}

	return count
}

func assertEqualIngresses(t *testing.T, expected []Ingress, actual []Ingress) {
	assert := assert.New(t)
	assert.Equal(len(expected), len(actual))
	for i, expected := range expected {
		actual := actual[i]
		assert.Equal(expected.Name, actual.Name)
		assert.Equal(len(expected.Spec.Rules), len(actual.Spec.Rules))
		for j := range expected.Spec.Rules {
			assert.Equal(expected.Spec.Rules[j], actual.Spec.Rules[j])
		}
	}
}

type dummyEvent struct {
	Name            string
	ResourceVersion int
}

var handlerMutex = &sync.Mutex{}

func handleGetIngresses(ingressList *IngressList) (http.Handler, chan<- dummyEvent) {
	eventChan := make(chan dummyEvent, 100)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Debug("test: Handling ingress request")
		defer log.Debug("test: Finished handling ingress request")

		if r.URL.Path != "/apis/extensions/v1beta1/ingresses" {
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
			writeAsJSON(ingressList, w)
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

func handleLongPollWatch(eventChan <-chan dummyEvent, w http.ResponseWriter, r *http.Request) {
	resourceVersion, _ := strconv.Atoi(r.FormValue("resourceVersion"))

	for {
		select {
		case event := <-eventChan:
			log.Debug("test: handling %v", event)
			if event.Name == "" {
				return
			}
			if event.Name == "OK" {
				w.WriteHeader(http.StatusOK)
				flush(w)
			}
			if event.Name == "DISCONNECT" {
				return
			}
			if event.Name == "BAD" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if event.Name == "GONE" {
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
