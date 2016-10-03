pkgs := $(shell go list ./... | grep -v /vendor/)
files := $(shell find . -path ./vendor -prune -o -name '*.go' -print)

.PHONY: all format test build vet lint copy docker release checkformat check clean

all : format check build
check : vet lint test
travis : checkformat check docker

format :
	@echo "== format"
	@goimports -w $(files)
	@sync

build :
	@echo "== build"
	@go install -v ./cmd/...

unformatted = $(shell goimports -l $(files))

checkformat :
	@echo "== check formatting"
ifneq "$(unformatted)" ""
	@echo "needs formatting: $(unformatted)"
	@echo "run make format"
	@exit 1
endif

vet : build
	@echo "== vet"
	@go vet $(pkgs)

lint :
	@echo "== lint"
	@for pkg in $(pkgs); do \
		golint -set_exit_status $$pkg || exit 1; \
	done;

test :
	@echo "== run tests"
	@go test -race $(pkgs)

# Docker build 
ingress_build_dir := build/ingress
template := ./nginx/nginx.tmpl
git_rev := $(shell git rev-parse --short HEAD)
builds := ingress dns

clean :
	@echo "== cleaning"
	rm -rf build

copy : build
	@echo "== copy docker files to build/"
	
	@for build in $(builds) ; do \
	  set -e; \
	  build_dir="build/$$build"; \
	  mkdir -p $$build_dir; \
	  cp build-nginx.sh $$build_dir; \
	  cp Dockerfile_$$build $${build_dir}/Dockerfile; \
	  cp $(GOPATH)/bin/feed-$$build $$build_dir; \
	done

	cp ${template} ${ingress_build_dir}

docker : copy
	@echo "== build docker images"
	@for build in $(builds) ; do \
	  set -e; \
	  tag=skycirrus/feed-$$build:$(git_rev) ; \
	  docker build -t $$tag build/$${build}/. ; \
	  echo "Built $$tag" ; \
	done

release : docker
	@echo "== release docker image"
	@docker login -e $(DOCKER_EMAIL) -u $(DOCKER_USERNAME) -p $(DOCKER_PASSWORD)
	@for build in $(builds) ; do \
	  set -e; \
	  tag=skycirrus/feed-$$build:$(git_rev) ; \
	  latest_tag=skycirrus/feed-$$build:latest ; \
	  docker tag $$tag $$latest_tag ; \
	  docker push $$tag ; \
	  docker push $$latest_tag ; \
	done
