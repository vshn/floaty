#!/bin/bash

set -u -o pipefail

if [ $# -eq 0 ]; then
  echo "Usage: $0 /path/to/floaty-config.yaml"
  exit 3
elif [ "$1" == "-h" ] || [ "$1" == "--help" ]; then
  echo "Usage: $0 /path/to/floaty-config.yaml"
  exit 3
fi

floaty_bin=/usr/bin/floaty
config_file="$1"

if [ ! -f "$config_file" ]; then
  echo "Floaty config file doesn't exist: ${config_file}"
  exit 3
fi

self_test_output=$("$floaty_bin" -T "$config_file" 2>&1)
self_test_exit=$?

if [ $self_test_exit -eq 0 ]; then
  echo "Floaty self-test passed"
  exit 0
else
  echo -e "Floaty self-test failed\n"
  echo -e "${self_test_output}"
  exit 2
fi
