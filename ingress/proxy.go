package ingress

import "github.com/sky-uk/feed/controller"

// Proxy proxies traffic from external clients to the internal services in Kubernetes.
type Proxy controller.Updater
