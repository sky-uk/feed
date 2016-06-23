#!/usr/bin/env bash

echo $0 $@
if [[ "$1" == "-t" ]]; then
    echo "Config check failed" >&2
    exit 1
fi

sleep 0.5
