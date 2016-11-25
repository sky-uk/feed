#!/usr/bin/env bash

echo $0 $@
if [[ "$1" == "-v" || "$1" == "-t" ]]; then
    sleep 0.1
else
    sleep 2
fi

