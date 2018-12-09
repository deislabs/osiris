#!/usr/bin/env bash

# AVOID INVOKING THIS SCRIPT DIRECTLY -- USE `make publish-rc-chart`

set -euxo pipefail

GIT_VERSION=$1

DATE=$(date -u +"%Y.%m.%d.%H.%M.%S")

rm -rf /tmp/osiris-edge
cp -r -L chart/osiris /tmp/osiris-edge
cd /tmp/osiris-edge

sed -i "s/^name:.*/name: osiris-edge/g" Chart.yaml
sed -i "s/^version:.*/version: 0.0.1-$DATE-$GIT_VERSION/g" Chart.yaml
sed -i "s/^appVersion:.*/appVersion: $GIT_VERSION/g" Chart.yaml
sed -i "s/^  tag:.*/  tag: $GIT_VERSION/g" values.yaml
sed -i "s/^  pullPolicy:.*/  pullPolicy: IfNotPresent/g" values.yaml

helm dep build .
helm package .

az acr helm push -n osiris osiris-edge-0.0.1-$DATE-$GIT_VERSION.tgz
