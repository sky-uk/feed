FROM debian:jessie

RUN apt-get update && apt-get upgrade -y -o Dpkg::Options::="--force-confnew" \
    && apt-get clean && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*  # 2016-06-01

ENV NGINX_VERSION 1.10.1-1~jessie

RUN apt-key adv --keyserver hkp://pgp.mit.edu:80 --recv-keys 573BFD6B3D8FBC641079A6ABABF5BD827BD9BF62 \
	&& echo "deb http://nginx.org/packages/debian/ jessie nginx" >> /etc/apt/sources.list \
	&& apt-get update \
	&& apt-get install --no-install-recommends --no-install-suggests -y \
						ca-certificates \
						nginx=${NGINX_VERSION} \
						gettext-base \
						curl \
						dnsutils \
						vim-tiny \
						lsof \
	&& rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

WORKDIR /
COPY feed-ingress /

RUN mkdir /nginx
COPY nginx.tmpl /nginx/
RUN chown -R nginx:nginx /nginx

USER nginx

ENTRYPOINT ["/feed-ingress", "-nginx-workdir", "/nginx"]
