#!/usr/bin/env bash

chown nginx:nginx /var/log/nginx/
# non group or public write required by logrotate
chmod 755 /var/log/nginx/
