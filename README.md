![travis](https://travis-ci.org/sky-uk/feed.svg?branch=master)

# Feed

Kubernetes controllers for managing external ingress with AWS.

## feed-ingress

`feed-ingress` manages an nginx instance for load balancing ingress traffic to Kubernetes services.
It's intended to be replicated to scale.

Run with:

    docker run skycirrus/feed-ingress:latest -h

See all tags at https://hub.docker.com/r/skycirrus/feed-ingress/tags/.

## feed-dns

`feed-dns` manages Route53 entries to point to the correct ELBs. It is designed to be run as a single
instance in your cluster.

Run with:

    docker run skycirrus/feed-dns:latest -h
    
See all the tags at https://hub.docker.com/r/skycirrus/feed-dns/tags/.

### Discovering ELBs

ELBs are discovered that have the `sky.uk/KubernetesClusterFrontend` tag set to the value passed in
with the value passed in with the `-elb-label-value` option.

It is assumed that there is at most one internal ELB and at most one internet facing ELB and they route traffic
to a `feed-ingress` instance.

### DNS records

The feed-dns controller assumes that it controls an entire Route53 HostedZone and manages an ALIAS records per
ingress.

On startup all ingress entries are queried and compared to all the Record Sets in the
configured hosted zone.

Any resource sets that do not have an ingress entry are deleted and for any new ingress entry an ALIAS record is created
to point to the correct ELB.

Each ingress must have the following tag `sky.uk/frontend-elb-scheme` set to `internal` or `internet-facing` so the A record can be set to the correct
ELB.

# Building

Requires these tools:

    go get -u github.com/golang/lint/golint
    go get -u golang.org/x/tools/cmd/goimports
    
Build and test with:

    make
    
# Releasing

Travis is configured to build the Docker image and push it to Dockerhub for each merge to master.

# Dependencies

Dependencies are managed with [govendor](https://github.com/kardianos/govendor). 
This is a thin wrapper for golang 1.6 support of a `vendor` directory.

    go get -u github.com/kardianos/govendor

To add a dependency:

    govendor fetch github.com/golang/glog

Make sure to commit changes to `vendor`, ideally as a separate commit to any other code change.

