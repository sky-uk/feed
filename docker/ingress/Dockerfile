FROM phusion/baseimage:0.9.22

RUN apt-get update \
    && apt-get dist-upgrade -y \
    && apt-get install --no-install-suggests --no-install-recommends -y \
        curl \
        dnsutils \
        vim-tiny \
        lsof \
        iproute2 \
    && apt-get clean -y \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/* /tmp/*

ENV NGINX_VERSION 1.12.2
ENV NGINX_SHA256 305f379da1d5fb5aefa79e45c829852ca6983c7cd2a79328f8e084a324cf0416
ENV VTS_VERSION 0.1.15
ENV VTS_SHA256 5112a054b1b1edb4c0042a9a840ef45f22abb3c05c68174e28ebf483164fb7e1

COPY build-nginx.sh /tmp
RUN /bin/bash /tmp/build-nginx.sh

COPY feed-ingress /
COPY nginx.tmpl /nginx/
RUN chown nginx:nginx /nginx/nginx.tmpl
RUN setcap "cap_net_bind_service=+ep" /usr/sbin/nginx

ADD logrotate.config /etc/logrotate.d/nginx
RUN chmod 600 /etc/logrotate.d/nginx

ADD logrotate.cron /etc/cron.d/nginx
RUN chmod 600 /etc/cron.d/nginx

# Defer execution as the log dir may be mounted when running
ADD log-dir-ownership.sh /etc/my_init.d/log-dir-ownership.sh

# Let feed shutdown gracefully by giving it plenty of time to stop.
# Give children processes 5 minutes to timeout
ENV KILL_PROCESS_TIMEOUT=300
# Give all other processes (such as those which have been forked) 5 minutes to timeout
ENV KILL_ALL_PROCESSES_TIMEOUT=300

ENTRYPOINT ["/sbin/my_init", "--quiet", "--", "/sbin/setuser", "nginx", \
    "/feed-ingress", "-nginx-workdir", "/nginx"]
