#!/usr/bin/env bash

[ -f /var/log/nginx ] || mkdir -p /var/log/nginx
chown nginx:nginx /var/log/nginx/
# non group or public write required by logrotate
chmod 755 /var/log/nginx/
