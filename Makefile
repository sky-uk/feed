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

vet :
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

ingress_binary := $(GOPATH)/bin/feed-ingress
template := ./ingress/nginx.tmpl
docker_repo := skycirrus/feed-ingress
git_rev := $(shell git rev-parse --short HEAD)
docker_tag := "$(docker_repo):$(git_rev)"
docker_latest := "$(docker_repo):latest"

clean:
	@echo "== cleaning"
	rm -rf build

copy : build
	@echo "== copy docker files to build/"
	@mkdir -p build
	cp Dockerfile build/
	cp $(ingress_binary) build/
	cp $(template) build/

docker : copy
	@echo "== build docker image"
	docker build -t $(docker_tag) build/.
	@echo "Built $(docker_tag)"

release : docker
	@echo "== release docker image"
	@docker login -e $(DOCKER_EMAIL) -u $(DOCKER_USERNAME) -p $(DOCKER_PASSWORD)
	docker tag $(docker_tag) $(docker_latest)
	docker push $(docker_tag)
	docker push $(docker_latest)
