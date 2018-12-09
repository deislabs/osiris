#!/usr/bin/env bash

# AVOID INVOKING THIS SCRIPT DIRECTLY -- USE `make test-unit`

set -euxo pipefail

go test -timeout 30s -race -coverprofile=coverage.txt -covermode=atomic \
  ./cmd/... \
  ./pkg/...
