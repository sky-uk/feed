pkgs = $(shell go list ./... | grep -v /vendor/)
files = $(shell find . -path ./vendor -prune -o -name '*.go' -print)
ingress_binary = $(GOPATH)/bin/feed-ingress
template = ./ingress/nginx.tmpl

all : format vet lint test build copy

format :
	@echo "== formatting"
	@goimports -w $(files)

test :
	@echo "== running tests"
	@go test -race $(pkgs)

build :
	@echo "== building"
	@go install -v ./cmd/...

vet :
	@echo "== vetting"
	@go vet $(pkgs)

lint :
	@echo "== linting"
	@for pkg in $(pkgs); do \
		golint -set_exit_status $$pkg || exit 1; \
	done;

copy :
	@echo "== coping files to docker"
	cp $(ingress_binary) ./docker/
	cp $(template) ./docker/

.PHONY: all format test build vet lint copy
