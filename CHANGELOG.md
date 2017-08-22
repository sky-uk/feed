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
