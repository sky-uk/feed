package k8s

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Implement cache.ResourceEventHandler
var _ informerFactory = &cacheInformerFactory{}

type informerFactory interface {
	createNamespaceInformer(time.Duration, cache.ResourceEventHandler) (cache.Store, cache.Controller)
	createIngressInformer(time.Duration, cache.ResourceEventHandler) (cache.Store, cache.Controller)
	createServiceInformer(time.Duration, cache.ResourceEventHandler) (cache.Store, cache.Controller)
}

type cacheInformerFactory struct {
	clientset *kubernetes.Clientset
}

func (c *cacheInformerFactory) createNamespaceInformer(resyncPeriod time.Duration, eventHandler cache.ResourceEventHandler) (cache.Store, cache.Controller) {
	namespaceLW := cache.NewListWatchFromClient(c.clientset.CoreV1().RESTClient(), "namespaces", "", fields.Everything())
	return cache.NewInformer(namespaceLW, &corev1.Namespace{}, resyncPeriod, eventHandler)
}

func (c *cacheInformerFactory) createIngressInformer(resyncPeriod time.Duration, eventHandler cache.ResourceEventHandler) (cache.Store, cache.Controller) {
	ingressLW := cache.NewListWatchFromClient(c.clientset.NetworkingV1().RESTClient(), "ingresses", "", fields.Everything())
	return cache.NewInformer(ingressLW, &networkingv1.Ingress{}, resyncPeriod, eventHandler)
}

func (c *cacheInformerFactory) createServiceInformer(resyncPeriod time.Duration, eventHandler cache.ResourceEventHandler) (cache.Store, cache.Controller) {
	serviceLW := cache.NewListWatchFromClient(c.clientset.CoreV1().RESTClient(), "services", "", fields.Everything())
	return cache.NewInformer(serviceLW, &corev1.Service{}, resyncPeriod, eventHandler)
}
