# v4.5.0

* New static mode which sets ingress status to a static hostname [#238](https://github.com/sky-uk/feed/pull/238)

# v4.4.0

* Upgrade to nginx version 1.21.3 from 1.15.7
* Configure and add a dynamic module which will suppress the `Server` response header. This will add to the existing
  feature of suppressing the nginx version and build information in the response headers.

# v4.3.2

* Refine the docker build process to pull in newer packages from Alpine
  and eliminate trivy security warnings for the resulting docker images
* Bump the version of the go `crypto` library to pull in the fix for a
  trivy-reported security warning.

# v4.3.1
* [BUGFIX] Fix issue with feed not waiting for the drain duration despite having at least one successful call to de-register from the list of registered target groups.
    * Feed instance can attach to multiple target groups belonging to the load balancer having a tag matching the value in the flag `ingress-class`. The fix is for feed instance to wait for the drain duration even if the de-register call succeeds to at least one of those target groups.

# v4.3.0
* Provide support for multiple labels for namespace selection
  * Remove flag `--ingress-controller-namespace-selector`
  * Add flag `--ingress-controller-namespace-selectors` - which can accept comma separated or repeated inputs
  * Add flag `--match-all-namespace-selectors` - for how to match the above provided labels

# v4.2.0
* Add flag `set-real-ip-from-header` to specify the name of the request header for the [real ip module](http://nginx.org/en/docs/http/ngx_http_realip_module.html) to use
  * The name of the header will be used by the real ip module in the `set_real_ip_from` directive.

# v4.1.0
* Switch feed-ingress base image to alpine to reduce the number of vulnerabilities

# v4.0.0
* Support latest backend config for nginx upstream module
  * Support setting keepalive_requests and keepalive_timeout for nginx upstream module
  * Support annotations on ingress to be able to set the values in nginx config for each upstream
* Build upstream id including ingress name (This might increase the number of virtual host entries)
  * Needed to be able to configure an upstream for ingresses which has same hostname and backend service but with a different path. This is so that the configuration won't be overwritten in this scenario.

# v3.1.7
* [BUGFIX] Do not generate Prometheus metrics for invalid nginx-vts entries

# v3.1.6
* [BUGFIX] Reduce the number of DescribeTargetGroups to avoid reaching the API limits
* [BUGFIX] Do not set the nlb updater as initialised if it failed to describe the target groups

# v3.1.5
* [BUGFIX] Generate the root path location for servers without the root ingress (#225)

# v3.1.4
* [BUGFIX] Account for concurrent status updates (#219)

# v3.1.3
* [BUGFIX] Trim whitespace from sky.uk/allow entries (#223)

# v3.1.2
* [BUGFIX] Validate `sky.uk/allow` annotation entries (#220)

# v3.1.1
* [BUGFIX] Wait for informer cache to be fully populated before interrogating it (#218)

# v3.1.0
* Fix X-Forwarded-Host to correctly forward client Host header (#217)

# v3.0.0
* Breaking change
  Feed will not bind port http 8080 by default, unless `--ingress-port=XXXX` is provided
  * Remove http binding by default (#216)

# v2.3.0
* Support IP and Instance TargetGroup attachments (#214)

# v2.2.3
* [BUGFIX] Handle receiving 0 ingresses from the k8s client by not reloading the Nginx config
* [BUGFIX] Improve sorting when choosing which duplicate ingresses to keep
* Added a new metric for Nginx reloads

# v2.2.2
* [BUGFIX] feed-ingress metrics now have correct prefix of `feed_ingress_` (was `feed_dns_`)

# v2.2.1
* [BUG] feed-ingress metrics are prefixed with `feed_dns_` instead of `feed_ingress_`. Fixed in [v2.2.2](#v222)
* Attach to NLBs using the instance's private IP address rather than the instance ID to allow services to route to a feed instance on
the same host.  More information can be found here: https://aws.amazon.com/premiumsupport/knowledge-center/target-connection-fails-load-balancer/.

# v2.2.0
* [BUG] feed-ingress metrics are prefixed with `feed_dns_` instead of `feed_ingress_`. Fixed in [v2.2.2](#v222)
* Reintroduce support for deprecated ingress resource annotations
  `sky.uk/frontend-elb-scheme` and `sky.uk/backend-keepalive-seconds`

# v2.1.0
* [BUG] feed-ingress metrics are prefixed with `feed_dns_` instead of `feed_ingress_`. Fixed in [v2.2.2](#v222)
* Added support for AWS Network Load Balancers with the `nlb` subcommand

# v2.0.0
* Added support for multiple feed-ingress controllers per cluster
* Feed-ingress invocation split into subcommands, using double-dashed arguments
* Remove support for deprecated ingress resource annotations:
  `sky.uk/frontend-elb-scheme` (replacement `sky.uk/frontend-scheme`),
  `sky.uk/backend-keepalive-seconds` (replacement `sky.uk/backend-timeout-seconds`)
* Remove deprecated feed-ingress command-line argument `--nginx-default-backend-keepalive-seconds` (replacement `--nginx-default-backend-timeout-seconds`)

This is a breaking change.  Follow the instructions to [upgrade from v1 to v2](https://github.com/sky-uk/feed#upgrade-from-v1-to-v2)

# v1.14.2
* Skip ingress when http and/or path are not defined

# v1.14.1
* Set max_conns default to 0

# v1.14.0
* Upgrade Nginx from 1.12.2 to 1.15.7

# v1.13.0
* Add support for exact paths when specified as locations
  https://github.com/sky-uk/feed/pull/197

# v1.12.3
* New flag to set the worker shutdown timeout

# v1.12.2
* Formatting changes
  https://github.com/sky-uk/feed/pull/190
* Fix duplicate path when path is not specified
  https://github.com/sky-uk/feed/pull/193
* Remove unnecessary config reload after start up
  https://github.com/sky-uk/feed/pull/194

# v1.12.1
* Fix bug in v1.12.0 with the nginx.conf template
  https://github.com/sky-uk/feed/pull/188

# v1.12.0
* Enable overriding proxy buffer values. Defaults to `proxy_buffer_size 16k` and `proxy_buffers 4 16k`
Can be overridden with relevant annotations
* The values overridden by the annotations are capped at a permissible max `proxy_buffer_size 32k` and `proxy_buffers 8 32k`
* Supports handling of large client requests (header and body). Refer https://github.com/sky-uk/feed#handling-large-client-requests

# v1.11.4
* DO NOT USE THIS VERSION

# v1.11.2
* Update nginx-opentracing version for bug fix to proxy headers

# v1.11.1
* Manually compile OpenTracing modules to avoid binary incompatibilities

# v1.11.0
* Add OpenTracing support

# v1.10.3
* Expose status updater error logs rather than printing a list of failed ingresses

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
