ifdef VERSION
	version := $(VERSION)
else
	git_rev := $(shell git rev-parse --short HEAD)
	git_tag := $(shell git tag --points-at=$(git_rev))
	version := $(if $(git_tag),$(git_tag),dev-$(git_rev))
endif

pkgs := $(shell go list ./... | grep -v /vendor/)
files := $(shell find . -path ./vendor -prune -o -name '*.go' -print)
build_time := $(shell date -u)
ldflags := -X "github.com/sky-uk/feed/feed-ingress/cmd.version=$(version)" -X "github.com/sky-uk/feed/feed-ingress/cmd.buildTime=$(build_time)"

os := $(shell uname)
ifeq ("$(os)", "Linux")
	GOOS = linux
else ifeq ("$(os)", "Darwin")
	GOOS = darwin
endif
GOARCH ?= amd64

.PHONY: all format test build vet lint copy docker release checkformat check clean fakenginx check-vulnerabilities

all : format check build
check : vet lint test
travis : checkformat check docker check-vulnerabilities

format :
	@echo "== format"
	@go run golang.org/x/tools/cmd/goimports@latest -w $(files)
	@sync

dist/feed-ingress : $(files)
	mkdir -p dist
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o dist/feed-ingress ./feed-ingress

dist/feed-dns : $(files)
	mkdir -p dist
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o dist/feed-dns ./feed-dns

build : dist/feed-ingress dist/feed-dns

checkformat :
	@echo "== check formatting"
	@unformatted=`go run golang.org/x/tools/cmd/goimports@latest -l $(files)`; if [ "$$unformatted" != "" ]; then \
	    echo "needs formatting: $$unformatted"; \
	    exit 1; \
	fi

vet :
	@echo "== vet"
	@go vet $(pkgs)

lint :
	@echo "== lint"
	@go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.45 run

fakenginx:
	@echo "== build fake nginx for tests"
	@go build -o nginx/fake/fake_graceful_nginx nginx/fake/fake_graceful_nginx.go

test : build fakenginx
	@echo "== run tests"
	@go test -race $(pkgs)

# Docker build
git_rev := $(shell git rev-parse --short HEAD)
git_tag := $(shell git tag --points-at=$(git_rev))
REGISTRY ?= skycirrus
image_prefix := $(REGISTRY)/feed

docker : test
	@echo "== build docker images"
	cp dist/feed-dns docker/dns
	cp dist/feed-ingress docker/ingress
	cp nginx/nginx.tmpl docker/ingress
	docker build -t $(image_prefix)-ingress:latest docker/ingress/
	docker build -t $(image_prefix)-dns:latest docker/dns/
	rm -f docker/dns/feed-dns
	rm -f docker/ingress/feed-ingress
	rm -f docker/ingress/nginx.tmpl

release : docker
	@echo "== release docker images"
ifeq ($(strip $(git_tag)),)
	@echo "no tag on $(git_rev), skipping release"
else
	@echo "releasing $(image)-(dns|ingress):$(git_tag)"
	@docker login -u $(DOCKER_USERNAME) -p $(DOCKER_PASSWORD)
	docker tag $(image_prefix)-ingress:latest $(image_prefix)-ingress:$(git_tag)
	docker tag $(image_prefix)-dns:latest $(image_prefix)-dns:$(git_tag)
	docker push $(image_prefix)-ingress:$(git_tag)
	docker push $(image_prefix)-ingress:latest
	docker push $(image_prefix)-dns:$(git_tag)
	docker push $(image_prefix)-dns:latest
endif

check-vulnerabilities:
	@echo "== Checking for vulnerabilities in the docker image"
	trivy image --exit-code=1 --severity="HIGH,CRITICAL" --ignorefile=trivy-ignore-file.txt $(image_prefix)-ingress:latest
