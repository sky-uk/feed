pkgs = $(shell go list ./... | grep -v /vendor/)
files = $(shell find . -path ./vendor -prune -o -name '*.go' -print)
ingress_binary = $(GOPATH)/bin/feed-ingress
template = ./ingress/nginx.tmpl
docker_repo = skycirrus/feed-ingress
git_rev = $(shell git rev-parse --short HEAD)
docker_tag = "$(docker_repo):$(git_rev)"
docker_latest = "$(docker_repo):latest"

all : format vet lint test build

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

copy : build
	@echo "== copying binaries to docker/"
	cp $(ingress_binary) ./docker/
	cp $(template) ./docker/

docker : copy
	@echo "== building docker image"
	docker build -t $(docker_tag) docker/.
	@echo "Built $(docker_tag)"

release : docker
	@echo "== releasing docker image"
	docker login -e $(DOCKER_EMAIL) -u $(DOCKER_USERNAME) -p $(DOCKER_PASSWORD)
	docker tag $(docker_tag) $(docker_latest)
	docker push $(docker_tag)
	docker push $(docker_latest)

.PHONY: all format test build vet lint copy docker release
