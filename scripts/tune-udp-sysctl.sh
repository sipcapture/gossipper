#!/usr/bin/env bash
# Apply recommended UDP sysctl values for high-scale RTP generation (requires root).
set -euo pipefail

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run as root: sudo $0" >&2
  exit 1
fi

sysctl -w net.core.rmem_max=33554432
sysctl -w net.core.rmem_default=8388608
sysctl -w net.core.wmem_max=33554432
sysctl -w net.core.wmem_default=4194304
sysctl -w net.core.netdev_max_backlog=250000

echo "OK: rmem_max=$(sysctl -n net.core.rmem_max) wmem_max=$(sysctl -n net.core.wmem_max)"
