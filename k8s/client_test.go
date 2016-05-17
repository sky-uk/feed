package k8s

import (
	"testing"

	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/stretchr/testify/assert"
)

func TestInvalidUrlReturnsError(t *testing.T) {
	_, err := New("%gh&%ij", []byte{}, "")
	assert.Error(t, err)
}

func TestRetrievesIngressesFromKubernetes(t *testing.T) {
	assert := assert.New(t)

	ingressFixture := CreateIngressesFixture()
	ts := httptest.NewTLSServer(handleGetIngresses(t, authToken, ingressFixture))
	defer ts.Close()

	client, err := New(ts.URL, caCert, authToken)
	assert.NoError(err)

	ingresses, err := client.GetIngresses()
	assert.NoError(err)

	assertEqualIngresses(t, ingressFixture, ingresses)
}

func TestErrorIfNon200StatusCode(t *testing.T) {
	assert := assert.New(t)

	ts := httptest.NewTLSServer(http.NotFoundHandler())
	defer ts.Close()

	client, err := New(ts.URL, caCert, authToken)
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

	client, err := New(ts.URL, caCert, authToken)
	assert.NoError(err)

	_, err = client.GetIngresses()
	assert.Error(err)
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

func handleGetIngresses(t *testing.T, token string, ingresses []Ingress) http.Handler {
	assert := assert.New(t)
	ingressList := &IngressList{Items: ingresses}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal("/apis/extensions/v1beta1/ingresses", r.URL.Path)
		assertAuthToken(t, r)
		bytes, err := json.Marshal(ingressList)
		assert.NoError(err)
		_, err = w.Write(bytes)
		assert.NoError(err)
	})
}

func assertAuthToken(t *testing.T, r *http.Request) {
	auths := r.Header["Authorization"]
	if len(auths) != 1 {
		assert.Fail(t, "Expected one authorization header, but got none")
	} else {
		assert.Equal(t, "Bearer "+authToken, r.Header["Authorization"][0],
			"Should authenticate with correct token to apiserver")
	}
}

const (
	ingressHost    = "foo.sky.com"
	ingressPath    = "/foo"
	ingressSvcName = "foo-svc"
	ingressSvcPort = 80
	authToken      = "validtoken"
)

func CreateIngressesFixture() []Ingress {
	paths := []HTTPIngressPath{HTTPIngressPath{
		Path: ingressPath,
		Backend: IngressBackend{
			ServiceName: ingressSvcName,
			ServicePort: FromInt(ingressSvcPort),
		},
	}}
	return []Ingress{
		Ingress{
			ObjectMeta: ObjectMeta{Name: "foo-ingress"},
			Spec: IngressSpec{
				Rules: []IngressRule{IngressRule{
					Host: ingressHost,
					IngressRuleValue: IngressRuleValue{HTTP: &HTTPIngressRuleValue{
						Paths: paths,
					}},
				}},
			},
		},
	}
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
