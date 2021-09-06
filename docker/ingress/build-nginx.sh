#!/usr/bin/env sh

set -ex

# From https://github.com/nginxinc/docker-nginx/blob/master/mainline/alpine/Dockerfile
# Build dependencies
apk add --no-cache --virtual .build-deps \
    gcc \
    libc-dev \
    make \
    openssl-dev \
    pcre-dev \
    zlib-dev \
    linux-headers \
    libxslt-dev \
    gd-dev \
    geoip-dev \
    perl-dev \
    libedit-dev \
    mercurial \
    bash \
    alpine-sdk \
    findutils \
    cmake

echo "--- Downloading NGINX and modules"
mkdir /tmp/nginx
cd /tmp/nginx
curl -O http://nginx.org/download/nginx-${NGINX_VERSION}.tar.gz
curl -JLO https://github.com/vozlt/nginx-module-vts/archive/v${VTS_VERSION}.tar.gz
curl -JLO https://github.com/opentracing-contrib/nginx-opentracing/archive/${OPENTRACING_NGINX_VERSION}.tar.gz
curl -JLO https://github.com/opentracing/opentracing-cpp/archive/v${OPENTRACING_CPP_VERSION}.tar.gz
curl -JLO https://github.com/jaegertracing/jaeger-client-cpp/archive/v${JAEGER_VERSION}.tar.gz

nginx_tarball="nginx-${NGINX_VERSION}.tar.gz"
vts_tarball="nginx-module-vts-${VTS_VERSION}.tar.gz"
opentracing_nginx_tarball="nginx-opentracing-${OPENTRACING_NGINX_VERSION}.tar.gz"
opentracing_cpp_tarball="opentracing-cpp-${OPENTRACING_CPP_VERSION}.tar.gz"
jaeger_tarball="jaeger-client-cpp-${JAEGER_VERSION}.tar.gz"

# 2 spaces required between hash and filename
touch hashes
echo "${NGINX_SHA256}  ${nginx_tarball}" >> hashes
echo "${VTS_SHA256}  ${vts_tarball}" >> hashes
echo "${OPENTRACING_NGINX_SHA256}  ${opentracing_nginx_tarball}" >> hashes
echo "${OPENTRACING_CPP_SHA256}  ${opentracing_cpp_tarball}" >> hashes
echo "${JAEGER_SHA256}  ${jaeger_tarball}" >> hashes
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

echo "--- Configuring NGINX"
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

echo "--- Building NGINX"
make
make install

echo "--- Building dynamic modules"
./configure \
    --with-http_realip_module \
    --with-http_stub_status_module \
    --with-threads \
    --with-file-aio \
    --with-http_v2_module \
    --with-ipv6 \
    --with-debug \
    --with-http_ssl_module \
    --add-dynamic-module=/tmp/nginx/nginx-opentracing-${OPENTRACING_NGINX_VERSION}/opentracing \
    --with-cc-opt="-I$HUNTER_INSTALL_DIR/include" \
    --with-ld-opt="-L$HUNTER_INSTALL_DIR/lib"
make modules

mkdir -p /nginx/modules
cp objs/ngx_http_opentracing_module.so /nginx/modules/
