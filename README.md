![travis](https://travis-ci.org/sky-uk/feed.svg?branch=master)

# Feed

This project contains Kubernetes controllers for managing external ingress with AWS or [IPVS/gorb](https://github.com/sky-uk/gorb). 
There are two controllers provided, `feed-ingress` which runs an nginx instance, and `feed-dns` which manages route53 entries. 
They can be run independently as needed, or together to provide a full ingress solution.

Feed is actively used in production and should be stable enough for general usage. We can scale to many thousands of
requests per second with only a handful of replicas.

# Using

Docker images are released using semantic versioning. See the [examples](examples/) for deployment yaml files that
can be applied to a cluster.

## Requirements

* An internal and internet-facing ELB has been created and can reach your kubernetes cluster. The ELBs should be tagged with `sky.uk/KubernetesClusterFrontend=<name>` which is used by feed to discover them.
* A Route53 hosted zone has been created to match your ingress resources.

## Known Limitations

* nginx reloads can be disruptive. On reload, nginx will finish in-flight requests, then abruptly
  close all server connections. This is a limitation of nginx, and affects all nginx solutions. We mitigate this by:
    * Rate limiting reloads. This is user configurable.
    * Using service IPs, which are stable. Reloads will only happen if an ingress or service changes, which is rare
      compared to pod changes.
* feed-dns only supports a single hosted zone at this time, but this should be straightforward to add support for.
  PRs are welcome.

# Overview

## feed-ingress

`feed-ingress` manages an nginx instance, updating its configuration dynamically for ingress resources. It attaches to
ELBs which are intended to be the frontend for all traffic.

See the command line options with:

    docker run skycirrus/feed-ingress:v1.1.0 -h

### SSL/TLS

## SSL termination on ELB
SSL termination could be done on ELBs, probably the safest and best performing
approach for production usage. Unfortunately, ELBs don't support SNI at this time, so this limits SSL usage to
a single domain. One workaround is to use a wildcard certificate for the entire zone that `feed-dns` manages.
Another is to place an SSL termination EC2 instance in front of the ELBs.

## SSL termination on feed-ingress

SSL termination could be done on feed-ingress. You will be able handle the secure websocket and HTTPS with a layer 4 in front (ELB, IPVS(GORB)).

For the moment you can setup a default wildcard ssl:
```
 # Set default ssl path + name file without extension - expected default-ssl.crt and default-ssl.key into /etc/ssl/default-ssl/
 - -default-ssl-path=/etc/ssl/default-ssl/default-ssl
```

You can mount the `.key` and `.crt` though a Kubernetes Secret see [feed-ingress-deployment-ssl](examples/feed-ingress-deployment-ssl.yml).

### Gorb / IPVS Support

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

### Running feed-ingress on privileged ports

feed-ingress can be run on privileged ports by defining  the `NET_BIND_SERVICE` Linux capability.
```
securityContext:
  capabilities:
    add:
    - NET_BIND_SERVICE

```

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

# Development

Requires these tools:

    go get -u github.com/golang/lint/golint
    go get -u golang.org/x/tools/cmd/goimports
    
Build and test with:

    make
    
## Releasing

Tag the commit in master and push it to release it. Only maintainers can do this.

## Dependencies

Dependencies are managed with [govendor](https://github.com/kardianos/govendor). 
This is a thin wrapper for golang 1.6 support of a `vendor` directory.

    go get -u github.com/kardianos/govendor

To add a dependency:

    govendor fetch github.com/golang/glog

Make sure to commit changes to `vendor`, ideally as a separate commit to any other code change.

