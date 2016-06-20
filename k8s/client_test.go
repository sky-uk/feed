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

	"crypto/tls"
	"crypto/x509"

	log "github.com/Sirupsen/logrus"
)

const smallWaitTime = time.Millisecond * 50

func newClient(url string, caCert []byte, token string) (Client, error) {
	return New(Conf{
		APIServerURL: url,
		CaCert:       caCert,
		Token:        token,
	})
}

func TestInvalidUrlReturnsError(t *testing.T) {
	_, err := newClient("%gh&%ij", []byte{}, "")
	assert.Error(t, err)
}

func TestRetrievesIngressesFromKubernetes(t *testing.T) {
	assert := assert.New(t)

	ingressFixture := createIngressesFixture()
	handler, _ := handleGetIngresses(ingressFixture)
	ts := httptest.NewTLSServer(handler)
	defer ts.Close()

	client, err := newClient(ts.URL, apiServerCert, testAuthToken)
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

	client, err := newClient(ts.URL, apiServerCert, testAuthToken)
	assert.NoError(err)

	services, err := client.GetServices()
	assert.NoError(err)

	assert.Equal(servicesFixture.Items, services)
}

func TestClientCertificatesWork(t *testing.T) {
	assert := assert.New(t)

	ingressFixture := createIngressesFixture()
	handler, _ := handleGetIngresses(ingressFixture)

	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(clientCa); !ok {
		assert.FailNow("unable to parse ca certificate")
	}
	ts := httptest.NewUnstartedServer(handler)
	ts.TLS = &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  pool,
	}
	ts.StartTLS()
	defer ts.Close()

	client, err := New(Conf{
		APIServerURL: ts.URL,
		CaCert:       apiServerCert,
		Token:        testAuthToken,
		ClientCert:   clientCert,
		ClientKey:    clientKey,
	})
	assert.NoError(err)
	_, err = client.GetIngresses()
	assert.NoError(err)
}

func TestTokenIsOptional(t *testing.T) {
	assert := assert.New(t)

	ingressFixture := createIngressesFixture()
	handler, _ := handleGetIngresses(ingressFixture)
	skipAuth = true
	ts := httptest.NewTLSServer(handler)
	defer ts.Close()

	client, err := New(Conf{
		APIServerURL: ts.URL,
		CaCert:       apiServerCert,
	})
	assert.NoError(err)

	_, err = client.GetIngresses()
	assert.NoError(err)
	skipAuth = false
}

func TestErrorIfNon200StatusCode(t *testing.T) {
	assert := assert.New(t)

	ts := httptest.NewTLSServer(http.NotFoundHandler())
	defer ts.Close()

	client, err := newClient(ts.URL, apiServerCert, testAuthToken)
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

	client, err := newClient(ts.URL, apiServerCert, testAuthToken)
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
		client, err := newClient(ts.URL, apiServerCert, testAuthToken)
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

var skipAuth bool

func validAuthToken(r *http.Request) bool {
	auths := r.Header["Authorization"]
	if skipAuth && len(auths) == 0 {
		return true
	}
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
	paths := []HTTPIngressPath{{
		Path: testIngressPath,
		Backend: IngressBackend{
			ServiceName: testSvcName,
			ServicePort: FromInt(testSvcPort),
		},
	}}
	return &IngressList{Items: []Ingress{
		{
			ObjectMeta: ObjectMeta{Name: "foo-ingress"},
			Spec: IngressSpec{
				Rules: []IngressRule{{
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
		{
			ObjectMeta: ObjectMeta{Name: "foo-service"},
			Spec: ServiceSpec{
				Type: ServiceTypeClusterIP,
				Ports: []ServicePort{{
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
var apiServerCert = []byte(`-----BEGIN CERTIFICATE-----
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

var clientCert = []byte(`-----BEGIN CERTIFICATE-----
MIIDLzCCAhegAwIBAgIJAIQtlMeG0ZEfMA0GCSqGSIb3DQEBCwUAMBIxEDAOBgNV
BAMMB2t1YmUtY2EwHhcNMTYwNjE3MTc0NTIyWhcNMTcwNjE3MTc0NTIyWjAVMRMw
EQYDVQQDDAprdWJlLWFkbWluMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKC
AQEAyxitmsH+A7meslmvjuqAey+VVT4TsjsiLj/wlJJZO4xMOvdaHhC/lkzYTGJU
FqrcWyCQtWPxgHr+CBmtXtCoBp0/ektHNn3RKbaCZIiWYEK9sHO18w66WmcFJVEd
g4/33gl83xHRWzyd1Sb/Repqxfl4PvcZN5kp/snJw8JY8TTVOnoSJdx8IYH5eJMT
80bFxv5a8thWY2O9jwZ4vj8svGrInhOZhear/JLLbRHHv5CK16uPp41TJsZikCCG
drcujy2T+s6Qb/NYau9qwfiYbiRJStAJts6L/pik/ebZITLZfX8Yc4wDPAdaoOf5
yzGvKY6YpUlxDA71EYRC4Yh65QIDAQABo4GEMIGBMAkGA1UdEwQCMAAwCwYDVR0P
BAQDAgXgMGcGA1UdEQRgMF6CCmt1YmVybmV0ZXOCEmt1YmVybmV0ZXMuZGVmYXVs
dIIWa3ViZXJuZXRlcy5kZWZhdWx0LnN2Y4Ika3ViZXJuZXRlcy5kZWZhdWx0LnN2
Yy5jbHVzdGVyLmxvY2FsMA0GCSqGSIb3DQEBCwUAA4IBAQBZZgxDcv95t6d0wzHo
s/g98CQOmB1wR9pqjqrKesVUjKS2lJL+WbX2vHB9bncCXuE40nWEK4jqvJ/pfOs8
L1TEloqF62X5hTsLl/pnJ+xRPpU/wtfvrP3YQvKIEOFLzjDbVCfVhKx6VECLfYT0
Cj9ns/Gsu8EmJidpWrgx1e8ToLJ2aVBZ+kwb6pvSzRrtCPYGc3VWK49T14K4PSbh
xMA+Vv8fieWBNfx0+vRKQyn2lK6tKeTgdNodIewoPyUJaQn6Ph2JwJBHk35FmmJ9
zDR3uQckSwip1Bz6OykKle6hI30N1Cu+OkwsxhFHQje1PM2kNy34+/9vpBBlZKIF
B3MO
-----END CERTIFICATE-----`)

var clientKey = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEpQIBAAKCAQEAyxitmsH+A7meslmvjuqAey+VVT4TsjsiLj/wlJJZO4xMOvda
HhC/lkzYTGJUFqrcWyCQtWPxgHr+CBmtXtCoBp0/ektHNn3RKbaCZIiWYEK9sHO1
8w66WmcFJVEdg4/33gl83xHRWzyd1Sb/Repqxfl4PvcZN5kp/snJw8JY8TTVOnoS
Jdx8IYH5eJMT80bFxv5a8thWY2O9jwZ4vj8svGrInhOZhear/JLLbRHHv5CK16uP
p41TJsZikCCGdrcujy2T+s6Qb/NYau9qwfiYbiRJStAJts6L/pik/ebZITLZfX8Y
c4wDPAdaoOf5yzGvKY6YpUlxDA71EYRC4Yh65QIDAQABAoIBADIDmsTwnug17tHG
6kfMkeVEG4dJaTpL+6feERXVUGosq50dyrB6uWN++wkccc6/NtKuG1TADvnvz90Y
zav6wFYYpUgtf5T4uOiHzGaLiFSeOu5YIGeBqfyXQBondpgufQDN31VjouXP8KJM
HzMNfkvQmn8PBMO/USswcCJoGtUTF6ern8udDdJeQkPLuQU5M9CWvQCtwf7ES9re
f8vR03MXcKGsqvq2YsvwVt1Xv54cA6+cKq9e0PuvlolvYSN/zDf5kmTfA3DPhvP9
tmDmZwPXDheQ6rm6v+ntlPCcYu9xrWlDD4cbefPoA5o+zC9rVpNXDU7FvxA90zhW
NDsaXKUCgYEA/yTWKFpPOD3Ju20gcdBZGWUOD+Kz0Z5FQcbz6eaNtVDQIlD1RlEz
OOXYXsN1/SONId9p0m7PlefIO+M2hiogMsKdvJhQTWrlwo22eND+ZwZuZ/ndb9Kz
OXIgJGU/bNElUn5NBqPzJh464lONJnqwPVT83icK3+M9ePuKmHz0vBcCgYEAy8ci
Q551PjA0uAYuhiP4xrulflrTX3Ab41vrHS9SbonvMxOQzth8/I+w8SYNuvCYrTLz
AqhnMII5O/ZPTc3FxAPsnQ1q89qzlkjB8OaLIJJMorLA+sRf8q7F1SeVRKDQPPP9
SJSW9ZXwhEIWFnSpxDU1KYwTTCzJHjX2a7Na8mMCgYEAx+GF3MsTMM5HEgwl1MQS
aTCf2ZYSpW9Gdod0YpN6BMewppGh9Vp7tGFsJqEd+Bg34odyEac5/Qg995zDBExQ
OTP5+tugXWYXZVk70F56Tx/cspwu/AGm4qQjxh+Dlq4qfPvxP/iE7iHUo6Ys+C45
j3LbPvZ7MHaHnBYDt/58hDUCgYEAx5btqolDkHuqxyvW2a/V9ODKAW54ZZvq1M+t
A1LcTERxsvdQ+Cf2k3Ex/6AkBputDsc+WbYUC+EgqehgWHZZY9nsIQ+JV/s3ttTg
kFFep7JjuV+XwIYi7BHe1x4EB8ny7CCWTkarbTNE9mW8OJZfyTvMLDt0k0GyYxK7
n1V2mL0CgYEAwEKai2uskyjEjcpCR7aMfurOJHNQsAkI0H+sJIfxaEho0yYI1bpy
TWXpBZC7cCDym581RG5Ow+b2wy0gz63dDOJPV7imFAZVNjlyBoSx3vyX+iDB+siV
1L66FJa2BrRpBOtHSFqjJyOdaODQSuwwSF7GFG1bq3JIgreHephoHsY=
-----END RSA PRIVATE KEY-----`)

var clientCa = []byte(`-----BEGIN CERTIFICATE-----
MIIC9zCCAd+gAwIBAgIJAOBMrn4UIoBRMA0GCSqGSIb3DQEBCwUAMBIxEDAOBgNV
BAMMB2t1YmUtY2EwHhcNMTYwNjE3MTc0NTIyWhcNNDMxMTAzMTc0NTIyWjASMRAw
DgYDVQQDDAdrdWJlLWNhMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA
0g7sX4PsCo4toZDPyKwZhnI7q9sCyiKafSwljGsB/UHzqYeQHfwxBDuz4UAiAtKY
Qy1ktNEgH4ztlfdA3JG4HrYoBHNHrVR4pk8S8iZ3+6TAtQ5elCGHoXb33cfZB/y1
Wu7DnVEiGNzNYDMRHS8SZeI+tkb50h1shQs2dfHpn9OjYifKB+oIhEJ0MopJNwH6
L/jV2fmVQ8mkbdvbgFGhfQJEXe/D0mjXLuOZOG4ahRlSvXtoQTCN3ryyuxPypZJT
Oy1uosDyG6SE6RkeSU0FQ7vw6QJebY7OnV+Z2cD0AgTcw8So/Vg0YW+zkfS5HHjS
FxGS8K3PsOILJIcdpIKiJQIDAQABo1AwTjAdBgNVHQ4EFgQUTNyAWOnBVFEMvWAL
eB4rtikqGPIwHwYDVR0jBBgwFoAUTNyAWOnBVFEMvWALeB4rtikqGPIwDAYDVR0T
BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAqDqMmhGCha6PAkTtaElQ4AcdZn0D
fycP5+abXd340iwYKez/xCAiefmbsj3L996AxX0lTadhDb7mQtPRXLXTou3koPOg
26j1dULKP4KbExw4woyrbTHQPnImzt2cYyIXYECyzvQksBC22FtRw+CDHlbKV4OO
bbo4V+t4xUHRwlRn4hYoo4Gpa9hGYKuvwxsyPAfQ8kVf6+17stTjdqSOpHS3GBPk
HDtwGKwz2U0vakIBedCSE/eQcS3i8Jrlu0RBthN+7ap27UfHhxJziRBuWOyBrH/C
G41Yh33wt+Zv2P9qstCf/7GaU3vLKsIZQJ58X15v7/YlG653v36I0l7fig==
-----END CERTIFICATE-----`)
