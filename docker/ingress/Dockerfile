FROM debian:stretch-slim

# Install useful diagnostic packages
RUN apt-get update \
    && apt-get dist-upgrade -y \
    && apt-get install --no-install-suggests --no-install-recommends -y \
        libcap2-bin \
        curl \
        ca-certificates \
        dnsutils \
        vim-tiny \
        lsof \
        iproute2 \
    && apt-get clean -y \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/* /tmp/*

# Install nginx

ENV NGINX_VERSION 1.15.7
ENV NGINX_SHA256 8f22ea2f6c0e0a221b6ddc02b6428a3ff708e2ad55f9361102b1c9f4142bdf93
ENV VTS_VERSION 0.1.15
ENV VTS_SHA256 5112a054b1b1edb4c0042a9a840ef45f22abb3c05c68174e28ebf483164fb7e1
ENV OPENTRACING_CPP_VERSION 1.4.2
ENV OPENTRACING_CPP_SHA256 2f04147ced383a2c834a92e923609de2db38b9192084075d8cf12d2ff6dc0aa0
ENV OPENTRACING_NGINX_VERSION 6cc2e9259be3a45a3dc943bacde870ee902f8866
ENV OPENTRACING_NGINX_SHA256 16cbc11a4ca9489c503fc1230481d0f5eec2177155ad45429e8c001e1bc2e6e4
ENV JAEGER_VERSION 0.4.2
ENV JAEGER_SHA256 21257af93a64fee42c04ca6262d292b2e4e0b7b0660c511db357b32fd42ef5d3

COPY build-nginx.sh /tmp
RUN chmod 755 /tmp/build-nginx.sh
RUN /tmp/build-nginx.sh
# For binding to privileged ports in nginx.
RUN setcap "cap_net_bind_service=+ep" /usr/sbin/nginx

# Setup feed controller
RUN useradd -s /sbin/nologin feed
RUN mkdir -p /nginx /var/cache/nginx
RUN chown -R feed:feed /nginx /var/cache/nginx

COPY feed-ingress /
# For binding VIP for merlin.
RUN setcap "cap_net_admin=+ep" /feed-ingress

COPY nginx.tmpl /nginx/
RUN chown feed:feed /nginx/nginx.tmpl

USER feed
ENTRYPOINT ["/feed-ingress", "-nginx-workdir", "/nginx"]
