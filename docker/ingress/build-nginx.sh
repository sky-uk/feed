#!/bin/bash

set -ex

apt-get update
apt-get install --no-install-suggests --no-install-recommends -y \
    build-essential \
    libc6 libc6-dev \
    libpcre3 libpcre3-dev libpcrecpp0v5 \
    zlib1g zlib1g-dev \
    libaio1 libaio-dev \
    sudo libssl-dev

echo "--- Downloading nginx and modules"
mkdir /tmp/nginx
cd /tmp/nginx
curl -O http://nginx.org/download/nginx-${NGINX_VERSION}.tar.gz
curl -JLO https://github.com/vozlt/nginx-module-vts/archive/v${VTS_VERSION}.tar.gz
curl -JLO https://github.com/opentracing-contrib/nginx-opentracing/releases/download/v${OPENTRACING_VERSION}/linux-amd64-nginx-${NGINX_VERSION}-ngx_http_module.so.tgz

nginx_tarball="nginx-${NGINX_VERSION}.tar.gz"
vts_tarball="nginx-module-vts-${VTS_VERSION}.tar.gz"
opentracing_tarball="linux-amd64-nginx-${NGINX_VERSION}-ngx_http_module.so.tgz"

touch hashes
echo "${NGINX_SHA256} ${nginx_tarball}" >> hashes
echo "${VTS_SHA256} ${vts_tarball}" >> hashes
echo "${OPENTRACING_SHA256} ${opentracing_tarball}" >> hashes
if ! sha256sum -c hashes; then
    echo "sha256 hashes do not match downloaded files"
    exit 1
fi

tar xzf ${nginx_tarball}
tar xzf ${vts_tarball}
tar xzf ${opentracing_tarball}
cd nginx-${NGINX_VERSION}

echo "--- Configuring nginx"
./configure \
    --prefix=/nginx \
    --sbin-path=/usr/sbin/nginx \
    --conf-path=/nginx/nginx.conf \
    --error-log-path=/dev/stderr \
    --http-log-path=/dev/stdout \
    --pid-path=/var/run/nginx.pid \
    --lock-path=/var/run/nginx.lock \
    --http-client-body-temp-path=/var/cache/nginx/client_temp \
    --http-proxy-temp-path=/var/cache/nginx/proxy_temp \
    --http-fastcgi-temp-path=/var/cache/nginx/fastcgi_temp \
    --http-uwsgi-temp-path=/var/cache/nginx/uwsgi_temp \
    --http-scgi-temp-path=/var/cache/nginx/scgi_temp \
    --user=nginx \
    --group=nginx \
    --with-http_realip_module \
    --with-http_stub_status_module \
    --with-threads \
    --with-file-aio \
    --with-http_v2_module \
    --with-ipv6 \
    --with-debug \
    --add-module=/tmp/nginx/nginx-module-vts-${VTS_VERSION}\
    --with-http_ssl_module

echo "--- Building nginx"
make
make install

echo "--- Installing Jaeger"
mkdir -p /nginx/modules/
cp /tmp/nginx/ngx_http_opentracing_module.so /nginx/modules/ngx_http_opentracing_module.so
cd /usr/local/lib
curl -JLO https://github.com/jaegertracing/jaeger-client-cpp/releases/download/v${JAEGER_VERSION}/libjaegertracing_plugin.linux_amd64.so
echo "${JAEGER_SHA256}  libjaegertracing_plugin.linux_amd64.so" > hashes
if ! sha256sum -c hashes; then
    echo "sha256 hashes do not match downloaded files"
    exit 1
fi
rm hashes

echo "--- Cleaning up"
apt-get purge -y build-essential ca-certificates libc6-dev libpcre3-dev zlib1g-dev libaio-dev gcc-5
apt-get clean -y
rm -rf /var/lib/apt/lists/* /tmp/*
