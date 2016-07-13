FROM phusion/baseimage:0.9.19

RUN apt-get update \
    && apt-get upgrade -y -o Dpkg::Options::="--force-confnew" \
	&& apt-get install --no-install-recommends --no-install-suggests -y \
						ca-certificates \
						curl \
						dnsutils \
						vim-tiny \
						lsof \
	&& rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/* # 2016-07-13

COPY feed-dns /

ENTRYPOINT ["/sbin/my_init", "--quiet", "--", "/feed-dns"]
