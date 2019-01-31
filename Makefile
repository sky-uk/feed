pkgs := $(shell go list ./... | grep -v /vendor/)
files := $(shell find . -path ./vendor -prune -o -name '*.go' -print)

.PHONY: all format test build vet lint copy docker release checkformat check clean

all : format check build
check : vet lint test
travis : checkformat check docker

setup:
	@echo "== setup"
	go get -u github.com/golang/lint/golint
	go get -u golang.org/x/tools/cmd/goimports
	go get -u github.com/golang/dep/cmd/dep
	dep ensure

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
git_rev := $(shell git rev-parse --short HEAD)
git_tag := $(shell git tag --points-at=$(git_rev))
image_prefix := skycirrus/feed

docker : build
	@echo "== build docker images"
	cp $(GOPATH)/bin/feed-dns docker/dns
	cp $(GOPATH)/bin/feed-ingress docker/ingress
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
