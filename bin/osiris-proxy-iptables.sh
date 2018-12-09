#!/bin/sh

set -o nounset

# shellcheck disable=SC2153
if echo "$PORT_MAPPINGS" | grep -E '^(\d+\:\d+,)*(\d+\:\d+)$' 1>/dev/null
then
  port_mappings=$(echo "$PORT_MAPPINGS" | tr "," "\\n")
else
  echo "invalid port mappings: $PORT_MAPPINGS"
  exit 1
fi

set -x

# Clear existing rules
iptables -t nat -D PREROUTING -p tcp -j OSIRIS_PROXY_INBOUND 2>/dev/null

# Flush and delete the old chain
iptables -t nat -F OSIRIS_PROXY_INBOUND 2>/dev/null
iptables -t nat -X OSIRIS_PROXY_INBOUND 2>/dev/null

set -o errexit
set -o pipefail

iptables -t nat -N OSIRIS_PROXY_INBOUND

set +x
for port_mapping in $port_mappings
do
  proxy_port=$(echo "$port_mapping" | cut -f1 -d ":")
  app_port=$(echo "$port_mapping" | cut -f2 -d ":")
  set -x
  iptables -t nat -A OSIRIS_PROXY_INBOUND -p tcp --dport "$app_port" -j REDIRECT --to-port "$proxy_port"
  set +x
done

set -x

iptables -t nat -A PREROUTING -p tcp -j OSIRIS_PROXY_INBOUND

iptables-save
