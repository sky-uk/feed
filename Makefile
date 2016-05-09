pkgs = $(shell go list ./... | grep -v /vendor/)
files = $(shell find . -path ./vendor -prune -o -name '*.go' -print)

all : format vet lint test build

format :
	@echo "== formatting"
	goimports -w $(files)

test :
	@echo "== running tests"
	go test $(pkgs)

build :
	@echo "== building"
	go install -v ./cmd/...

vet :
	@echo "== vetting code"
	go vet $(pkgs)

lint :
	@echo "== linting code"
	@for pkg in $(pkgs); do \
		golint -set_exit_status $$pkg; \
	done;

.PHONY: all format test build vet lint
