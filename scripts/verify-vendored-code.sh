#!/usr/bin/env bash

# AVOID INVOKING THIS SCRIPT DIRECTLY -- USE `make verify-vendored-code`

set -euxo pipefail

echo "==> Running dep check <=="
dep check
