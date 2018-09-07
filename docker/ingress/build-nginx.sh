#!/bin/bash

set -ex

apt-get update
apt-get install --no-install-suggests --no-install-recommends -y \
    build-essential \
    cmake automake autogen autoconf libtool \
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
curl -JLO https://github.com/opentracing-contrib/nginx-opentracing/archive/v${OPENTRACING_NGINX_VERSION}.tar.gz
curl -JLO https://github.com/opentracing/opentracing-cpp/archive/v${OPENTRACING_CPP_VERSION}.tar.gz
curl -JLO https://github.com/jaegertracing/jaeger-client-cpp/archive/v${JAEGER_VERSION}.tar.gz

nginx_tarball="nginx-${NGINX_VERSION}.tar.gz"
vts_tarball="nginx-module-vts-${VTS_VERSION}.tar.gz"
opentracing_nginx_tarball="nginx-opentracing-${OPENTRACING_NGINX_VERSION}.tar.gz"
opentracing_cpp_tarball="opentracing-cpp-${OPENTRACING_CPP_VERSION}.tar.gz"
jaeger_tarball="jaeger-client-cpp-${JAEGER_VERSION}.tar.gz"

touch hashes
echo "${NGINX_SHA256} ${nginx_tarball}" >> hashes
echo "${VTS_SHA256} ${vts_tarball}" >> hashes
echo "${OPENTRACING_NGINX_SHA256} ${opentracing_nginx_tarball}" >> hashes
echo "${OPENTRACING_CPP_SHA256} ${opentracing_cpp_tarball}" >> hashes
echo "${JAEGER_SHA256} ${jaeger_tarball}" >> hashes
if ! sha256sum -c hashes; then
    echo "sha256 hashes do not match downloaded files"
    exit 1
fi

tar xzf ${nginx_tarball}
tar xzf ${vts_tarball}
tar xzf ${opentracing_nginx_tarball}
tar xzf ${opentracing_cpp_tarball}
tar xzf ${jaeger_tarball}

echo "--- Build OpenTracing dependencies"
cd /tmp/nginx/opentracing-cpp-${OPENTRACING_CPP_VERSION}
mkdir .build && cd .build
cmake -DCMAKE_BUILD_TYPE=Release \
      -DBUILD_TESTING=OFF ..
make && make install

cd /tmp/nginx/jaeger-client-cpp-${JAEGER_VERSION}
mkdir .build && cd .build
cmake -DCMAKE_BUILD_TYPE=Release \
      -DBUILD_TESTING=OFF \
      -DJAEGERTRACING_WITH_YAML_CPP=ON ..
make && make install
HUNTER_INSTALL_DIR=$(cat _3rdParty/Hunter/install-root-dir)

cd /tmp/nginx/nginx-${NGINX_VERSION}

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
    --with-http_ssl_module

echo "--- Building nginx"
make
make install

echo "--- Building dynamic modules"
./configure \
    --with-compat \
    --add-dynamic-module=/tmp/nginx/nginx-opentracing-${OPENTRACING_NGINX_VERSION}/opentracing \
    --with-cc-opt="-I$HUNTER_INSTALL_DIR/include" \
    --with-ld-opt="-L$HUNTER_INSTALL_DIR/lib" \
    --with-debug
make modules

mkdir -p /nginx/modules
cp objs/ngx_http_opentracing_module.so /nginx/modules/

echo "--- Cleaning up"
apt-get purge -y build-essential ca-certificates libc6-dev libpcre3-dev zlib1g-dev libaio-dev gcc-5 cmake automake autogen autoconf libtool
apt-get clean -y
rm -rf /var/lib/apt/lists/* /tmp/* /root/.hunter
