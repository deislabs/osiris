#!/usr/bin/env bash

# AVOID INVOKING THIS SCRIPT DIRECTLY -- USE `make publish-release-chart`

set -euxo pipefail

REL_VERSION=$1

# Strip away the leading "v"
SIMPLE_REL_VERSION=$(echo $REL_VERSION | cut -c 2-)

rm -rf /tmp/osiris
cp -r -L chart/osiris /tmp
cd /tmp/osiris

sed -i "s/^version:.*/version: $SIMPLE_REL_VERSION/g" Chart.yaml
sed -i "s/^appVersion:.*/appVersion: $SIMPLE_REL_VERSION/g" Chart.yaml
sed -i "s/^  tag:.*/  tag: $REL_VERSION/g" values.yaml
sed -i "s/^  pullPolicy:.*/  pullPolicy: IfNotPresent/g" values.yaml

helm dep build .
helm package .

az acr helm push -n osiris osiris-$SIMPLE_REL_VERSION.tgz
