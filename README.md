![travis](https://travis-ci.org/sky-uk/feed.svg?branch=master)

# Feed

This project contains Kubernetes controllers for managing external ingress with AWS or [IPVS](https://github.com/sky-uk/merlin). 

There are two controllers provided, `feed-ingress` which runs an nginx instance, and `feed-dns` which manages route53 entries. 
They can be run independently as needed, or together to provide a full ingress solution. `feed-ingress` can be arbitrarily scaled up to support any traffic load.

Feed is actively used in production and should be stable enough for general usage. We can scale to many thousands of
requests per second with only a handful of replicas.

# Using

Docker images are released using semantic versioning. See the [examples](examples/) for deployment yaml files that
can be applied to a cluster.

## Requirements

* An internal and internet-facing ELB has been created and can reach your kubernetes cluster.
The ELBs should be tagged with `sky.uk/KubernetesClusterFrontend=<name>` which is used by feed to discover them.
If you are using v2 of `feed-ingress` the ELBs should also be tagged with `sky.uk/KubernetesClusterIngressName=<name>`.
See [upgrade from v1 to v2](#upgrade-from-v1-to-v2) for more information.
* A Route53 hosted zone has been created to match your ingress resources.

## Known Limitations

* nginx reloads can be disruptive. On reload, nginx will finish in-flight requests, then abruptly
  close all server connections. This is a limitation of nginx, and affects all nginx solutions. We mitigate this by:
    * Rate limiting reloads. This is user configurable.
    * Using service IPs, which are stable. Reloads will only happen if an ingress or service changes, which is rare
      compared to pod changes.
* feed-dns only supports a single hosted zone at this time, but this should be straightforward to add support for.
  PRs are welcome.


# Upgrading

## Upgrade from v1 to v2

This is a breaking change to support [multiple ingresses per cluster](#multiple-ingresses-per-cluster).

To upgrade, follow these steps:
1. Tag the ELBs with `sky.uk/KubernetesClusterIngressName=<name>`
2. Provide the mandatory argument `--ingress-name` to feed-ingress with a matching value.

# Overview

## feed-ingress

`feed-ingress` manages an nginx instance, updating its configuration dynamically for ingress resources. It attaches to
ELBs which are intended to be the frontend for all traffic.

See the command line options with:

    docker run skycirrus/feed-ingress:v1.1.0 -h

### SSL termination on ELB

SSL termination could be done on ELBS, and we believe that this is the safest and best performing
approach for production usage. Unfortunately, ELBs don't support SNI at this time, so this limits SSL usage to
a single domain. One workaround is to use a wildcard certificate for the entire zone that `feed-dns` manages.
Another is to place an SSL termination EC2 instance in front of the ELBs.

### SSL termination on feed-ingress

SSL termination can be done on feed-ingress. This approach still requires a layer 4 load balancer, eg. ELB or IPVS, in front.

For the moment you can setup a default wildcard ssl:
```
 # Set default ssl path + name file without extension.  Feed expects two files: one ending in .crt (the CA) and the other in .key (the private key), for example:
 -ssl-path=/etc/ssl/default-ssl/default-ssl
 # ... will cause feed to look for /etc/ssl/default-ssl.crt and /etc/ssl/default.ssl.key
```

You can mount the `.key` and `.crt` though a Kubernetes Secret see [feed-ingress-deployment-ssl](examples/feed-ingress-deployment-ssl.yml).

### Merlin support

Merlin is a distributed loadbalancer based on IPVS, with a gRPC based API. feed supports attaching to merlin
as a frontend for ingress.

See the [example](examples/feed-ingress-deployment-merlin.yml) for details.

### Gorb / IPVS Support

_Gorb support is deprecated, and will be removed at some point. Use merlin instead._

feed has support for configuring IPVS via [gorb](https://github.com/sky-uk/gorb).
Gorb exposes a REST api to interrogate and modify the IPVS configuration such as virtual services and backends.
The configuration can be stored in a distributed key/value store.

Although IPVS supports multiple packet-forwarding methods, feed currently only supports 'DR' aka Direct Server Return.
It provides the ability to manage the loopback interface so the ingress instance can pretend to be IPVS at the IP level.  
feed-ingress pod will need to define the `NET_ADMIN` Linux capability to be able to manage the loopback interface.

```
securityContext:
  capabilities:
    add:
    - NET_ADMIN
```

See the [example ingress for gorb](examples/feed-ingress-deployment-gorb.yml)

### OpenTracing

The build now includes support for OpenTracing, and the default Docker image includes the Jaeger tracing vendor
implementation.

To enable OpenTracing, you will need to provide the following options:
```
 # Define the path to the OpenTracing vendor plugin
 -nginx-opentracing-plugin-path=/usr/local/lib/libjaegertracing_plugin.linux_amd64.so
 # Define the path to the config for the vendor plugin
 -nginx-opentracing-config-path=/etc/jaeger-nginx-config.json
```

Note that the status and metrics endpoints will *not* have OpenTracing applied.

### Handling large client requests

`feed-ingress` now supports handling of large client requests (header and body). The following are the default values for the same. 

```
client_header_buffer_size 16k; 
client_body_buffer_size 16k; 
large_client_header_buffers 4 16k
``` 

They can be overridden by passing the following arguments during startup.

```
    -nginx-client-header-buffer-size-in-kb=16
    -nginx-client-body-buffer-size-in-kb=16
    
    # Set the maximum number and size of buffers used for reading large client request header. If this value is set, 
    # -nginx-client-header-buffer-size-in-kb should be passed in as well. Otherwise, this value will be ignored.
    -nginx-large-client-header-buffer-blocks=4
```

### Ingress status

When using either the [elb](#elb) or [Merlin](#merlin) updater, the ingress status will be updated with relevant
loadbalancer information. This can then be used with other controllers such a `external-dns` which can set DNS for any
given ingress using the ingress status.

#### elb

feed will automatically discover all of your elb's and then use the `sky.uk/frontend-scheme` annotation to match an elb
label to an ingress. The updater will then set the ingress status to the elb's DNS name. 

#### Merlin

The Merlin updater is currently unable to auto-discover all hosted loadbalancers on a Merlin server; instead the status
updater supports two different types: `internal` and `internet-facing`. These two loadbalancers are set using the
`merlin-internal-hostname` and `merlin-internet-facing-hostname` flags respectively.

An ingress can select which loadbalancer it wants to be associated with by setting the `sky.uk/frontend-scheme`
annotation to either `internal` or `internet-facing`.

### Running feed-ingress on privileged ports

feed-ingress can be run on privileged ports by defining  the `NET_BIND_SERVICE` Linux capability.
```
securityContext:
  capabilities:
    add:
    - NET_BIND_SERVICE

```

### Multiple ingresses per cluster

Multiple ingresses can be created per cluster.  Feed will use the `sky.uk/KubernetesClusterIngressName=<name>` tag to
attach to the right ELB.

Ingress resources will be created where the `sky.uk/ingress-name` annotation matches the `ingress-name` argument.


#### Support

This feature is supported by `feed-ingress` and the `elb` loadbalancer.  It is currently not supported by `feed-dns` 
or any other loadbalancer type.  PRs are welcome.

## feed-dns

`feed-dns` manages a Route53 hosted zone, updating entries to point to ELBs or arbitrary hostnames. It is designed to
be run as a single instance per zone in your cluster.

See the command line options with:

    docker run skycirrus/feed-dns:v1.1.0 -h
   
### DNS records

The feed-dns controller assumes that it can overwrite any entry in the supplied DNS zone and manages ALIAS and CNAME
records per ingress.

On startup, all ingress entries are queried and compared to all the Record Sets in the configured hosted zone.

Any records pointing to one of the endpoints associated with this controller that do not have an ingress
entry are deleted. For any new ingress entry, a record is created to point to the correct endpoint. Existing
records which do not meet these conditions remain untouched.

Each ingress must have the following tag `sky.uk/frontend-scheme` (`sky.uk/frontend-elb-scheme` is **deprecated**) set to `internal` or `internet-facing` so the
record can be set to the correct endpoint.

If you're using ELBs then ALIAS (A) records will be created. If you've explicitly provided CNAMEs of your load-balancers then CNAMEs will be created.

## Ingress annotations

The controllers support several annotations on ingress resources. See the [example ingress](examples/ingress.yml) for details.

## ALB Support

feed has support for ALBs. Unfortunately, ALBs have a bug that prevents non-disruptive deployments of feed (specifically,
they don't respect the deregistration delay). As a result, we don't recommend using ALBs at this time.

# Comparison to official nginx ingress controller

feed was started before the [official nginx ingress controller](https://github.com/kubernetes/ingress-nginx) became production ready. The main differences that exist now are:
* feed has fewer features, as we only built it for our needs.
* feed pods attach directly to ELB/ALBs or IPVS nodes. The official controller relies on the `LoadBalancer` service type, which generally forwards traffic to every node in your cluster (`service.spec.externalTrafficPolicy` can be set in some providers to mitigate this). We found this problematic:
    * It increases the amount of traffic flowing through your cluster, as traffic is routed through every node unnecessarily.
    * ELB health checks don't work  - the ELBs will disable arbitrary nodes, rather than a broken ingress pod.
* feed uses services, while the official controller uses endpoints:
    * Primarily to reduce the number of nginx reloads that occur, which are problematic in busy environments. It may be possible to mitigate this though with a dynamic update of nginx (via plugin), and is something we've discussed doing for service updates.
    * It's debateable whether using endpoints directly is a good idea conceptually, as it bypasses kube-proxy and any service mesh in place.
  
# Development

Install the required tools and setup dependencies:

    make setup

Build and test with:

    make
    
## Releasing

Tag the commit in master and push it to release it. Only maintainers can do this.

## Dependencies

Dependencies are managed with [dep](https://golang.github.io/dep/). Run `dep ensure` to keep your vendor folder up
to date after a pull.
