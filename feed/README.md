# Kubernetes ingress controller

Ingress controller for serving traffic from an external source to kubernetes services.

# Building

Build with:

    make
    
Run with:

    feed

# Dependencies

Dependencies are managed and vendored with https://github.com/FiloSottile/gvt. This is a thin wrapper
for golang 1.6 support of a `vendor` directory.

    go get -u github.com/FiloSottile/gvt

To add a dependency:

    gvt fetch github.com/golang/glog

Make sure to commit changes to `vendor`, ideally as a separate commit to any other code change.
