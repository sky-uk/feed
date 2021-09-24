![travis](https://travis-ci.org/sky-uk/feed.svg?branch=master)

# Feed
This project contains Kubernetes controllers for managing external ingress with
AWS or [IPVS](https://github.com/sky-uk/merlin) load balancers.

There are two controllers provided, `feed-ingress` which runs an NGINX instance, and `feed-dns` which manages Amazon Route 53 entries.
They can be run independently as needed, or together to provide a full ingress solution. `feed-ingress` can be arbitrarily scaled up to support any traffic load.

Feed is actively used in production and should be stable enough for general usage. We can scale to many thousands of
requests per second with only a handful of replicas.

## Comparison to official NGINX ingress controller
Feed was started before the [official NGINX ingress controller](https://github.com/kubernetes/ingress-nginx)
became production ready.
The main differences that exist now are:
* Feed has fewer features, as we only built it for our needs.
* Feed pods attach directly to AWS load balancers or IPVS nodes. The official controller relies on the
  `LoadBalancer` service type, which generally forwards traffic to every node in your cluster (`service.spec.externalTrafficPolicy` can be set in some providers to mitigate this). We found this problematic:
  * It increases the amount of traffic flowing through your cluster, as traffic is routed through every node unnecessarily.
  * ELB health checks don't work  - the ELBs will disable arbitrary nodes, rather than a broken ingress pod.
* Feed uses services, while the official controller uses endpoints:
  * Primarily to reduce the number of NGINX reloads that occur, which are problematic in busy environments.
    It may be possible to mitigate this though with a dynamic update of NGINX (via plugin), and is something
    we've discussed doing for service updates.
  * It's debatable whether using endpoints directly is a good idea conceptually, as it bypasses kube-proxy
    and any service mesh in place.

# Using
Docker images for `feed-ingress` and `feed-dns` are released using semantic versioning.
See the [examples](examples/) for deployment YAML files that can be applied to a cluster.

# Requirements
## RBAC permissions
The following RBAC permissions are required by the service account under which feed runs:

```yaml
rules:
- apiGroups:
  - ""
  - extensions
  resources:
  - ingresses
  - namespaces
  - services
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - extensions
  resources:
  - ingresses/status
  verbs:
  - update
```

## AWS components
When running `feed-dns` or `feed-ingress` with AWS load balancers, the following are required:
* An internal and internet-facing load balancer which can reach your Kubernetes cluster.
The load balancers should be tagged with `sky.uk/KubernetesClusterFrontend=<name>` which is used by feed to discover them.
If you are using v2 of `feed-ingress` they should also be tagged with `sky.uk/KubernetesClusterIngressClass=<name>`.
See [upgrading from v1 to v2](#upgrade-from-v1-to-v2) for more information.
* For `feed-dns`, a Route 53 hosted zone to match your ingress resources.

# feed-ingress
`feed-ingress` manages an NGINX instance, updating its configuration dynamically for ingress resources. It attaches to
ELBs which are intended to be the frontend for all traffic.

See the command line options with:

    docker run --rm skycirrus/feed-ingress:v2.0.0 -h

## Known limitations
* NGINX reloads can be disruptive. On reload, NGINX will finish in-flight requests, then abruptly
  close all server connections. This is a limitation of NGINX, and affects all NGINX solutions. We mitigate this by:
    * Rate limiting reloads. This is user configurable.
    * Using service IPs, which are stable. Reloads will only happen if an ingress or service changes, which is rare
      compared to pod changes.

## Upgrading from v1 to v2
This is a breaking change to support [multiple ingress controllers per cluster](#multiple-ingress-controllers-per-cluster).
The feed-ingress command-line structure has also changed. There are subcommands for the various load balancer types and
arguments use double dashes to be POSIX-compliant.

To upgrade, follow these steps:
1. Tag the ELBs with `sky.uk/KubernetesClusterIngressClass=<name>` to indicate which feed-ingress controllers should attach to them
1. Annotate all ingresses with `kubernetes.io/ingress.class=<name>`
1. Replace deprecated ingress resource annotations with their replacements:
   `sky.uk/frontend-elb-scheme` becomes `sky.uk/frontend-scheme`,
   `sky.uk/backend-keepalive-seconds` becomes `sky.uk/backend-timeout-seconds`
1. Use double dashes for all arguments
1. Provide the mandatory argument `--ingress-class=<name>` to feed-ingress with a value matching the ELB tag.
   For migrating existing deployments, you may provide the new (but deprecated) flag `--include-classless-ingresses`
   which instructs feed-ingress to additionally consider ingress resources that have no `kubernetes.io/ingress.class` annotation
1. Instead of using the argument `-registration-frontend-type`, use instead a subcommand of `feed-ingress`
   (for example `feed-ingress -registration-frontend-type=elb <args...>` becomes `feed-ingress elb <args>`)
1. Rename the argument `-elb-label-value` to `--elb-frontend-tag-value`
1. Rename the argument `-nginx-default-backend-keepalive-seconds` to `--nginx-default-backend-timeout-seconds`

## SSL termination
### SSL termination on load balancer
SSL termination could be done on ELBs, and we believe that this is the safest and best performing
approach for production usage. Unfortunately, ELBs don't support SNI at this time, so this limits SSL usage to
a single domain. One workaround is to use a wildcard certificate for the entire zone that `feed-dns` manages.
Another is to place an SSL termination EC2 instance in front of the ELBs.

### SSL termination on feed-ingress
SSL termination can be done on feed-ingress. This approach still requires a layer 4 load balancer, eg. ELB or IPVS, in front.

For the moment you can setup a default wildcard SSL:

```bash
# Set default SSL path + name file without extension. Feed expects two files: one ending in .crt (the CA)
# and the other in .key (the private key), for example: --ssl-path=/etc/ssl/default-ssl/default-ssl
# will cause feed to look for /etc/ssl/default-ssl.crt and /etc/ssl/default.ssl.key
```

You can mount the `.key` and `.crt` though a Kubernetes Secret see [feed-ingress-deployment-ssl](examples/feed-ingress-deployment-ssl.yml).

## Merlin support
Merlin is a distributed load balancer based on IPVS, with a gRPC based API. Feed supports attaching to merlin
as a frontend for ingress.

See the [example](examples/feed-ingress-deployment-merlin.yml) for details.

## GORB / IPVS Support
_GORB support is deprecated, and will be removed at some point. Use merlin instead._

Feed has support for configuring IPVS via [GORB](https://github.com/sky-uk/gorb).
GORB exposes a REST api to interrogate and modify the IPVS configuration such as virtual services and backends.
The configuration can be stored in a distributed key/value store.

Although IPVS supports multiple packet-forwarding methods, feed currently only supports 'DR' aka Direct Server Return.
It provides the ability to manage the loopback interface so the ingress instance can pretend to be IPVS at the IP level.
feed-ingress pod will need to define the `NET_ADMIN` Linux capability to be able to manage the loopback interface.

```yaml
securityContext:
  capabilities:
    add:
    - NET_ADMIN
```

See the [example deployment for GORB](examples/feed-ingress-deployment-gorb.yml)

## AWS load balancer support
Feed supports the three types of load balancer offered by AWS.

### Classic Load Balancers (ELBs)
See the [example deployment for ELB](examples/feed-ingress-deployment-elb.yml)

### Network Load Balancers (NLBs)
See the [example deployment for NLB](examples/feed-ingress-deployment-nlb.yml)

Please note that when a new Feed pod is added as a target to the target group of an NLB, it takes some time for the
new target to be registered and health-checked by the NLB before the NLB will send it any requests. Please see the
"Target Health Status" section of the
[AWS documentation on NLB target group health checks](https://docs.aws.amazon.com/elasticloadbalancing/latest/network/target-group-health-checks.html) for more details.

### Application Load Balancers (ALBs)
Feed has support for ALBs. Unfortunately, ALBs have a bug that prevents non-disruptive deployments of feed (specifically,
they don't respect the deregistration delay). As a result, we don't recommend using ALBs at this time.

## OpenTracing
The build now includes support for OpenTracing, and the default Docker image includes the Jaeger tracing vendor
implementation.

To enable OpenTracing, you will need to provide the following options:

```bash
# Define the path to the OpenTracing vendor plugin
--nginx-opentracing-plugin-path=/usr/local/lib64/libjaegertracing.so
# Define the path to the config for the vendor plugin
--nginx-opentracing-config-path=/etc/jaeger-nginx-config.json
```

Note that the status and metrics endpoints will *not* have OpenTracing applied.

## Handling large client requests
`feed-ingress` now supports handling of large client requests (header and body). The following are the default values for the same.

```
client_header_buffer_size 16k;
client_body_buffer_size 16k;
large_client_header_buffers 4 16k
```

They can be overridden by passing the following arguments during startup.

```bash
--nginx-client-header-buffer-size-in-kb=16
--nginx-client-body-buffer-size-in-kb=16

# Set the maximum number and size of buffers used for reading large client request header. If this value is set,
# --nginx-client-header-buffer-size-in-kb should be passed in as well. Otherwise, this value will be ignored.
--nginx-large-client-header-buffer-blocks=4
```

## Deriving client address from the request header
A flag `set-real-ip-from-header` can be used to specify the name of the request header for the [real ip module](http://nginx.org/en/docs/http/ngx_http_realip_module.html) to use in the `set_real_ip_from` directive.
The default value of this flag would be `X-Forwarded-For`

## Namespace selectors
Namespace selectors can be used for the feed-ingress instance to only process ingress definitions from only those namespaces which have labels matching the ones passed in the input.
The following 2 flags help facilitate this

1. `ingress-controller-namespace-selectors` - This flag will either be a repeated or comma separated value of namespace labels.

```
Examples:

1. --ingress-controller-namespace-selectors=app=some-app,team=some-team
2. --ingress-controller-namespace-selectors=app=some-app --ingress-controller-namespace-selectors=team=some-team
```

2. `match-all-namespace-selectors` - This flag is to determine how the above flags should be used for matching on the namespace labels. This would be false by default which would mean that a namespace matching any of the above labels will be picked.
If this flag is set, the namespace on which the ingress is defined should have all of the passed in labels.

## Ingress status
When using the [ELB](#elb), [NLB](#nlb), [Static](#static) or [Merlin](#merlin) updaters, the ingress status will be updated with relevant
load balancer information. This can then be used with other controllers such as `external-dns` which can set DNS for any
given ingress using the ingress status.

### AWS load balancers
Feed will automatically discover all of your load balancers and then use the `sky.uk/frontend-scheme` annotation to
match a load balancer label to an ingress. The updater will then set the ingress status to the load balancer's DNS name.

### Merlin
The Merlin updater is currently unable to auto-discover all hosted load balancers on a Merlin server; instead the status
updater supports two different types: `internal` and `internet-facing`. These two load balancers are set using the
`merlin-internal-hostname` and `merlin-internet-facing-hostname` flags respectively.

An ingress can select which load balancer it wants to be associated with by setting the `sky.uk/frontend-scheme`
annotation to either `internal` or `internet-facing`.

### Static
The static updater sets an Ingress's status hostname to a static value.
Two values are supported: `external-hostname` and `internal-hostname`.
An ingress can select which hostname it wants to be associated with by setting the `sky.uk/frontend-scheme`
annotation to either `internal` or `internet-facing`.

## Running feed-ingress on privileged ports
feed-ingress can be run on privileged ports by defining  the `NET_BIND_SERVICE` Linux capability.

```yaml
securityContext:
  capabilities:
    add:
    - NET_BIND_SERVICE
```

## Multiple ingress controllers per cluster
Multiple feed-ingress controllers can be created per cluster. Load balancers should be tagged with
`sky.uk/KubernetesClusterIngressClass=<name>` and feed instances started with `--ingress-class=<name>`.
Feed instances will attach to load balancers with matching ingress class names.

A feed ingress controller will adopt ingress resources with a matching `kubernetes.io/ingress.class=<value>` annotation.
Ingress resources with no annotation will normally not be adopted and will have no traffic sent to their associated services.
However, see the deprecated flag `--include-classless-ingresses` which instructs feed-ingress to additionally consider
ingress resources with no `kubernetes.io/ingress.class` annotation.

Use the script `classless-ingresses.sh` to find ingresses without this annotation.

This feature is supported by `feed-ingress` and the `elb` and `nlb` load balancer.  It is currently not supported by `feed-dns`
or any other load balancer type. PRs are welcome.

# feed-dns
`feed-dns` manages a Route 53 hosted zone, updating entries to point to ELBs or arbitrary hostnames. It is designed to
be run as a single instance per zone in your cluster.

See the command line options with:

    docker run skycirrus/feed-dns:v2.0.0 -h

## DNS records
The feed-dns controller assumes that it can overwrite any entry in the supplied DNS zone and manages ALIAS and CNAME
records per ingress.

On startup, all ingress entries are queried and compared to all the Record Sets in the configured hosted zone.

Any records pointing to one of the endpoints associated with this controller that do not have an ingress
entry are deleted. For any new ingress entry, a record is created to point to the correct endpoint. Existing
records which do not meet these conditions remain untouched.

Each ingress must have the following be annotated with `sky.uk/frontend-scheme` set to `internal` or `internet-facing`
so the record can be set to the correct endpoint.

If you're using ELBs then ALIAS (A) records will be created. If you've explicitly provided CNAMEs of your
load balancers then CNAMEs will be created.

## Known limitations
* `feed-dns` only supports a single hosted zone at this time, but this should be straightforward to add support for.
PRs are welcome.

# Development
Install the required tools and setup dependencies:

    make setup

Build and test with:

    make

## Releasing
Tag the commit in master and push it to release it. Only maintainers can do this.

## Dependencies
Dependencies are managed with [Go Modules](https://github.com/golang/go/wiki/Modules).
