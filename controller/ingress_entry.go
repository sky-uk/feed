package controller

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	v1 "k8s.io/api/networking/v1"
)

// IngressEntries type
type IngressEntries []IngressEntry

// IngressEntry describes the ingress for a single host, path, and service.
type IngressEntry struct {
	// The name of the feed-ingress instance that will manage the ingress resource.
	IngressClass string
	// Namespace of the ingress.
	Namespace string
	// Name of the ingress.
	Name string
	// Host is the fully qualified domain name used for external access.
	Host string
	// Path is the url path after the hostname. Must be non-empty.
	Path string
	// ServiceAddress is a routable address for the Kubernetes backend service to proxy traffic to.
	// Must be non-empty.
	ServiceAddress string
	// ServicePort is the port to proxy traffic to. Must be non-zero.
	ServicePort int32
	// Allow are the ips or CIDRs that are allowed to access the service.
	Allow []string
	// LbScheme internet-facing or internal will dictate which kind of load balancer to attach to.
	LbScheme string
	// StripPaths before forwarding to the backend
	StripPaths bool
	// ExactPath indicates that the Path should be treated as an exact match rather than a prefix
	ExactPath bool
	// BackendTimeoutSeconds backend timeout
	BackendTimeoutSeconds int
	// BackendMaxConnections maximum backend connections
	BackendMaxConnections int
	// BackendKeepaliveTimeout timeout for idle connections to upstream
	BackendKeepaliveTimeout time.Duration
	// BackendMaxRequestsPerConnection max requests per connection to upstream, after which it will be closed
	BackendMaxRequestsPerConnection uint64
	// Ingress creation time
	CreationTimestamp time.Time
	// Ingress resource
	Ingress *v1.Ingress
	// Size of the buffer used for reading the first part of the response received from the proxied server.
	ProxyBufferSize int
	// Number of buffers used for reading a response from the proxied server, for a single connection.
	ProxyBufferBlocks int
}

// Borrowed from the go stdlib, net/url:shouldEscape()
func isPathValid(path string) bool {
	for i := 0; i < len(path); i++ {
		// RFC 3986, Appendix A.
		// pchar = unreserved / pct-encoded / sub-delims / ":" / "@".
		switch path[i] {
		case '-', '_', '.', '~':
			// ok - unreserved characters
			continue
		case '%':
			// ok - percent encoded
			continue
		case '!', '$', '&', '\'', '(', ')', '*', '+', ',', ';', '=':
			// ok - subdelims
			continue
		case ':', '@':
			// ok - specicfically allowed
			continue
		case '/':
			// ok - path delimiter
			continue
		}
		// ok - alphanumeric
		if 'a' <= path[i] && path[i] <= 'z' || 'A' <= path[i] && path[i] <= 'Z' || '0' <= path[i] && path[i] <= '9' {
			continue
		}
		// anything not marked previously as ok is not ok
		return false
	}
	return true
}

// validate returns error if entry has invalid fields.
func (e IngressEntry) validate() error {
	if e.Host == "" {
		return errors.New("missing host")
	}
	if e.ServiceAddress == "" {
		return errors.New("missing service address")
	}
	if e.ServiceAddress == "None" {
		return errors.New("service address is set to 'None'")
	}
	if e.ServicePort == 0 {
		return errors.New("missing service port")
	}
	if !isPathValid(e.Path) {
		return fmt.Errorf("path '%s' contains illegal characters", e.Path)
	}

	var invalidAllowEntries []string
	for _, allowEntry := range e.Allow {
		if net.ParseIP(allowEntry) == nil {
			if _, _, err := net.ParseCIDR(allowEntry); err != nil {
				if allowEntry == "" {
					invalidAllowEntries = append(invalidAllowEntries, "<empty>")
				} else {
					invalidAllowEntries = append(invalidAllowEntries, allowEntry)
				}
			}
		}
	}

	if len(invalidAllowEntries) > 0 {
		return fmt.Errorf("host %s: invalid entries in sky.uk/allow: %s", e.Host, strings.Join(invalidAllowEntries, ","))
	}

	return nil
}

// NamespaceName returns the string "Namespace/Name".
func (e IngressEntry) NamespaceName() string {
	return fmt.Sprintf("%s/%s", e.Namespace, e.Name)
}

func (e IngressEntry) String() string {
	return fmt.Sprintf("IngressEntry[Namespace=%s,Name=%s,Host=%s,Path=%s,ServiceAddress=%s,ServicePort=%d]",
		e.Namespace, e.Name, e.Host, e.Path, e.ServiceAddress, e.ServicePort)
}
