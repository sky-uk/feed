#!/usr/bin/env bash

echo $0 $@
if [[ "$1" == "-v" ]]; then
    echo "Nginx version message"
else
    echo "Exiting immediately - failed!"
    exit -1
fi
