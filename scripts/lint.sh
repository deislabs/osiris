#!/usr/bin/env bash

# AVOID INVOKING THIS SCRIPT DIRECTLY -- USE `make lint`

set -euxo pipefail

golangci-lint run \
  ./cmd/... \
  ./pkg/...
