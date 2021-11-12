FROM alpine:3.13 as os-image

# Update list of available packages and upgrade installed packages
RUN apk -U upgrade

# Install useful diagnostic packages
RUN apk add --no-cache \
    libcap \
    ca-certificates \
    pcre \
    curl \
    bind-tools \
    lsof \
    iproute2 \
    vim

FROM scratch as builder

# Copy the os-image -- this is to ensure we have an identical version of
# runtime dependencies (as of now: pcre) across the build and runtime images
COPY --from=os-image / /

# Install NGINX

ENV NGINX_VERSION 1.21.3
ENV NGINX_SHA256 14774aae0d151da350417efc4afda5cce5035056e71894836797e1f6e2d1175a
ENV VTS_VERSION 0.1.18
ENV VTS_SHA256 17ea41d4083f6d1ab1ab83dad9160eeca66867abe16c5a0421f85a39d7c84b65
ENV OPENTRACING_CPP_VERSION 1.4.2
ENV OPENTRACING_CPP_SHA256 2f04147ced383a2c834a92e923609de2db38b9192084075d8cf12d2ff6dc0aa0
ENV OPENTRACING_NGINX_VERSION 6cc2e9259be3a45a3dc943bacde870ee902f8866
ENV OPENTRACING_NGINX_SHA256 16cbc11a4ca9489c503fc1230481d0f5eec2177155ad45429e8c001e1bc2e6e4
ENV JAEGER_VERSION 0.4.2
ENV JAEGER_SHA256 21257af93a64fee42c04ca6262d292b2e4e0b7b0660c511db357b32fd42ef5d3
ENV MORE_HEADERS_VERSION 0.33
ENV MORE_HEADERS_SHA256 a3dcbab117a9c103bc1ea5200fc00a7b7d2af97ff7fd525f16f8ac2632e30fbf

COPY build-nginx.sh /tmp
RUN chmod 755 /tmp/build-nginx.sh
RUN /tmp/build-nginx.sh

FROM scratch

# Copy the up-to-date alpine image
COPY --from=os-image / /

# Copy the built binaries and libs

# Nginx
COPY --from=builder /nginx /nginx
COPY --from=builder /usr/sbin/nginx /usr/sbin/nginx
# Opentracing-cpp
COPY --from=builder \
    /usr/local/lib/libopentracing.so.1.4.2 \
    /usr/local/lib/libopentracing.so.1 \
    /usr/local/lib/libopentracing.so \
    /usr/local/lib/libopentracing.a \
    /usr/local/lib/libopentracing_mocktracer.so.1.4.2 \
    /usr/local/lib/libopentracing_mocktracer.so.1 \
    /usr/local/lib/libopentracing_mocktracer.so \
    /usr/local/lib/libopentracing_mocktracer.a \
    /usr/local/lib/
# Jaeger
COPY --from=builder \
    /usr/local/lib64/libjaegertracing.so.0.4.2 \
    /usr/local/lib64/libjaegertracing.so.0 \
    /usr/local/lib64/libjaegertracing.so \
    /usr/local/lib64/libjaegertracing.a \
    /usr/local/lib64/

# For binding to privileged ports in NGINX.
RUN setcap "cap_net_bind_service=+ep" /usr/sbin/nginx

# Setup feed controller
RUN adduser --shell /sbin/nologin --disabled-password feed
RUN mkdir -p /nginx /var/cache/nginx
RUN chown -R feed:feed /nginx /var/cache/nginx

COPY feed-ingress /
# For binding VIP for merlin.
RUN setcap "cap_net_admin=+ep" /feed-ingress

COPY nginx.tmpl /nginx/
RUN chown feed:feed /nginx/nginx.tmpl

USER feed
ENTRYPOINT ["/feed-ingress", "--nginx-workdir", "/nginx"]
