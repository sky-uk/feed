# v1.10.2
* Bug fix for k8s/status updater where feed would exit the update loop if any ingress was 'unchanged'

# v1.10.1
* Added `merlin-internal-hostname` and `merlin-internet-facing-hostname` flags for setting Merlin ingress status,
replacing the `merlin-internet-facing-vip` flag
* Included extra testing around ingress validation

# v1.10.0
* Added `k8s/status` library for setting ingress status
* ELB and Merlin updaters set relevant ingress status

# v1.9.3
* Attach to https in addition to http for merlin.
* Fix merlin deregistration, which was failing due to long lived connections getting killed.

# v1.9.2
* Bug fix for merlin attacher - fix netlink and capabilities for feed-ingress.

# v1.9.1
* Introduced flag to set the amount of memory allocated to the vhost statistics module (default: 1 MiB)

# v1.9.0
* Add ability to specify health checks for merlin frontends.

# v1.8.0
* Upgraded to Nginx 1.12.2 and VTS 0.1.15
* Introduced `sky.uk/backend-max-connections` annotation that sets upstream.max_conns (http://nginx.org/en/docs/http/ngx_http_upstream_module.html#max_conns)
* Introduced flag to set global value for upstream.max_conns (default: 1024)

# v1.7.0
* Add support for [merlin](https://github.com/sky-uk/merlin) frontend
* Swap to dep from govendor

# v1.6.1
* Moved to using [pester](https://github.com/sethgrid/pester) as an http client
* Implemented retries on calls to the gorb API

# v1.6.0
* Enable SSL termination
Set default ssl path + name file without extension.
Feed expects two files: one ending in .crt (the CA) and the other in .key (the private key), for example:
-ssl-path=/etc/ssl/default-ssl/default-ssl

# v1.5.0
* Add `gorb-backend-healthcheck-type` that can be either 'tcp', 'http' or 'none'
* Remove deprecated `elb-drain-delay` feed-ingress flag 

# v1.4.2
* Reduce logging each ingress in the controller from Info to Debug, introduced in v1.3.0

# v1.4.1
* Fix wrong output direction when managing loopback interface using sudo

# v1.4.0
* Add support for configuring IPVS via [gorb](https://github.com/sky-uk/gorb) with Direct Server Return packet-forwarding method.
  Various flags prefixed with `gorb-` have been added to feed-ingress to customise gorb configuration.
* Add `registration-frontend-type` feed-ingress flag to specify either elb, alb or gorb.
* Deprecate `elb-drain-delay` feed-ingress flag in favour of the more generic `drain-delay`.

# v1.3.0
* Add support for non-AWS load balancers, which are referenced by static hostnames.

# v1.2.2
* Stop logging out the entire Route53 record set on update at Info in feed-dns: reduce this to Debug,
  and instead emit the number of records currently in the record set at Info

# v1.2.1
* Aggressively rotate access logs to avoid excessive file cache usage. This can lead to kernel
  allocation failures when running feed inside a container with a memory limit.

# v1.2.0
* Rename annotation `sky.uk/backend-keepalive-seconds` to `sky.uk/backend-timeout-seconds` to make it
  clear that this value only affects request timeouts. The old annotation is preserved for backwards
  compatibility.
* Update to golang 1.9.1.

# v1.1.3
* Fix bug where no ELB updater would be created if the `elb-label-value` is provided.

# v1.1.2

* Fix bug where feed-ingress would wait for `elb-drain-delay` and `alb-target-group-deregistration-delay`
  even if no instances where attached.
* Do not create ELB or ALB updater when `elb-label-value` or `alb-target-group-names`,
  respectively, are empty.
* Note: this image is broken, it does not create an ELB updater if the `elb-label-value` is provided.

# v1.1.1

Make deduping ingress entries deterministic.

The previous approach tried to order ingress by CreationTimestamp before
picking the most recent ingress.  This did not work because multiple
duplicate ingresses could be created at the same time.

This fix orders ingress entries by Namespace,Name,Host,Path and only
uses the first ingress 'Host/Path' encountered to dedupe.  Kubernetes
guarentees unique ingress for a given 'Namespace/Name' which will make
this deduping deterministic.

# v1.1.0

* Do not delete unassociated resource record sets (http://github.com/sky-uk/feed/pull/144)

# v1.0.2

* Fix bug where feed-ingress could return 404s for a brief period upon startup.

# v1.0.1

* Fix bug in feed that causes unhealthy status at startup (https://github.com/sky-uk/feed/pull/141)

# v1.0.0

* First official release with our production-ready ingress controllers.
