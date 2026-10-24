#!/usr/bin/env bash
# Install LinuxIO from this checkout: binaries from the build directory
# (make build-binaries), packaging assets from the repo. All the work happens
# in install-linuxio-binaries.sh --local.
set -euo pipefail

exec "$(dirname "${BASH_SOURCE[0]}")/install-linuxio-binaries.sh" --local "$@"
