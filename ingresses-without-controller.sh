#!/bin/bash -e

ingress_name="sky.uk/ingress-controller-name"

if [[ $# -eq 0 ]]; then
    cat >&2 <<EOF
Lists ingress resources that are missing the "$ingress_name" annotation.
Usage: $(basename $0) <k8s-context>
EOF
    exit 1
fi

context="${1}"

kubectl --context "${context}" get ingress --all-namespaces -o json | \
    jq --raw-output '
        ["NAMESPACE","INGRESS"],
        ( .items[]
          | select(.metadata.annotations["'${ingress_name}'"] | length == 0)
          | [.metadata.namespace, .metadata.name]
        ) | @tsv'
