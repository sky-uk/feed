# Feed

Kubernetes controllers for managing external ingress with AWS.

## feed-ingress

`feed-ingress` manages an nginx instance for load balancing ingress traffic to Kubernetes services.
It's intended to be replicated to scale.

## feed-dns

`feed-dns` manages Route53 entries to point to the correct ELBs.

## feed-elb

`feed-elb` manages ELBs, attaching and removing ingress nodes.

# Building

Build and test with:

    make

# Dependencies

Dependencies are managed and vendored with https://github.com/FiloSottile/gvt. This is a thin wrapper
for golang 1.6 support of a `vendor` directory.

    go get -u github.com/FiloSottile/gvt

To add a dependency:

    gvt fetch github.com/golang/glog

Make sure to commit changes to `vendor`, ideally as a separate commit to any other code change.

