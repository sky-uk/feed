#!/usr/bin/env bash
set -e

function run_local_registry() {
  local registry_name=$1
  local registry_port=$2

  # local registry so we can build images locally
  runningRegistry=$(docker ps --filter=name="${registry_name}" --format="{{.Names}}")
  if [[ "$runningRegistry" == "" ]]; then
      echo "Running local registry on port ${registry_port}"
      docker run -d --name=${registry_name} --restart=always -p ${registry_port}:${registry_port} registry:2
      echo "Started registry: ${registry_name}"
  fi

  # connect the registry to the cluster network
  # (the network may already be connected)
  docker network connect "kind" "${registry_name}" >/dev/null 2>&1 || true
}

function ensure_kind_exists() {
  # pre-requisite
  if ! [ -x "$(command -v kind)" ]; then
    echo 'Error: kind is not installed. Try "curl -L https://github.com/kubernetes-sigs/kind/releases/download/v0.8.1/kind-linux-amd64 --output kind"' >&2
    exit 1
  fi
}

function delete_cluster() {
  # clean previous cluster if present
  kind delete cluster || true
  kubectl config delete-context kind || true
}

function create_cluster() {
  local registry_name=$1
  local registry_port=$2

  # create a cluster
  tmpDir=$(mktemp -d)
  trap '{ CODE=$?; rm -rf ${tmpDir} ; exit ${CODE}; }' EXIT
  cat << EOF > ${tmpDir}/kind-cluster.yml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:${registry_port}"]
    endpoint = ["http://${registry_name}:${registry_port}"]
nodes:
  - role: control-plane
  - role: worker
EOF

  # node image officially supported for v0.8.1 - see https://github.com/kubernetes-sigs/kind/releases/tag/v0.8.0 for list of supported
  KIND_NODE_IMAGE=${KIND_NODE_IMAGE:-"kindest/node:v1.12.10@sha256:faeb82453af2f9373447bb63f50bae02b8020968e0889c7fa308e19b348916cb"}
  kind create cluster --config ${tmpDir}/kind-cluster.yml --image ${KIND_NODE_IMAGE}
  kubectl config rename-context "kind-kind" "kind"
}

registry_name="kind-registry"
registry_port=5000

ensure_kind_exists
delete_cluster
create_cluster ${registry_name} ${registry_port}
run_local_registry ${registry_name} ${registry_port}
