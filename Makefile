pkgs = $(shell go list ./... | grep -v /vendor/)

all : format vet test build

format :
	@echo "== formatting"
	goimports -w $(shell find . -path ./vendor -prune -o -name '*.go' -print)

test :
	@echo "== running tests"
	go test $(pkgs)

build :
	@echo "== building"
	go install -v ./cmd/...

vet :
	@echo "== vetting code"
	go vet $(pkgs)

.PHONY: all format test build vet
