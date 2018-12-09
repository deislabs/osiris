#!/usr/bin/env bash

# AVOID INVOKING THIS SCRIPT DIRECTLY -- USE `make lint-chart`

set -euxo pipefail

helm lint chart/osiris
