#!/bin/bash

set -ex

useradd -s /sbin/nologin nginx
mkdir -p /nginx /var/cache/nginx
chown -R nginx:nginx /nginx /var/cache/nginx

apt-get update
apt-get install --no-install-suggests --no-install-recommends -y \
    build-essential \
    libc6 libc6-dev \
    libpcre3 libpcre3-dev libpcrecpp0v5 \
    zlib1g zlib1g-dev \
    libaio1 libaio-dev

echo "--- Downloading nginx and modules"
mkdir /tmp/nginx
cd /tmp/nginx
curl -O http://nginx.org/download/nginx-${NGINX_VERSION}.tar.gz
curl -JLO https://github.com/vozlt/nginx-module-vts/archive/v${VTS_VERSION}.tar.gz

nginx_tarball="nginx-${NGINX_VERSION}.tar.gz"
vts_tarball="nginx-module-vts-${VTS_VERSION}.tar.gz"

touch hashes
echo "${NGINX_SHA256} ${nginx_tarball}" >> hashes
echo "${VTS_SHA256} ${vts_tarball}" >> hashes
if ! sha256sum -c hashes; then
    echo "sha256 hashes do not match downloaded files"
    exit 1
fi

tar xzf nginx-${NGINX_VERSION}.tar.gz
tar xzf nginx-module-vts-${VTS_VERSION}.tar.gz
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
    --add-module=/tmp/nginx/nginx-module-vts-0.1.10

echo "--- Building nginx"
make
make install

echo "--- Cleaning up"
apt-get purge -y build-essential libc6-dev libpcre3-dev zlib1g-dev libaio-dev gcc-5 cpp-5
apt-get clean -y
rm -rf /var/lib/apt/lists/* /tmp/*
