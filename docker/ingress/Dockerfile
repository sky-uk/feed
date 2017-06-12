FROM phusion/baseimage:0.9.19

RUN apt-get update \
    && apt-get dist-upgrade -y \
    && apt-get install --no-install-suggests --no-install-recommends -y \
        curl \
        dnsutils \
        vim-tiny \
        lsof \
    && apt-get clean -y \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/* /tmp/*

ENV NGINX_VERSION 1.10.2
ENV NGINX_SHA256 1045ac4987a396e2fa5d0011daf8987b612dd2f05181b67507da68cbe7d765c2
ENV VTS_VERSION 0.1.10
ENV VTS_SHA256 c6f3733e9ff84bfcdc6bfb07e1baf59e72c4e272f06964dd0ed3a1bdc93fa0ca

COPY build-nginx.sh /tmp
RUN /bin/bash /tmp/build-nginx.sh

COPY feed-ingress /
COPY nginx.tmpl /nginx/
RUN chown nginx:nginx /nginx/nginx.tmpl

ADD logrotate.config /etc/logrotate.d/nginx
RUN chmod 600 /etc/logrotate.d/nginx

ADD logrotate.cron /etc/cron.d/nginx
RUN chmod 600 /etc/cron.d/nginx

# Defer execution as the log dir may be mounted when running
ADD log-dir-ownership.sh /etc/my_init.d/log-dir-ownership.sh

# Let feed shutdown gracefully by giving it plenty of time to stop.
RUN sed -i 's/KILL_PROCESS_TIMEOUT = 5/KILL_PROCESS_TIMEOUT = 300/' /sbin/my_init \
    && grep 'KILL_PROCESS_TIMEOUT = 300' /sbin/my_init

ENTRYPOINT ["/sbin/my_init", "--quiet", "--", "/sbin/setuser", "nginx", \
    "/feed-ingress", "-nginx-workdir", "/nginx"]
