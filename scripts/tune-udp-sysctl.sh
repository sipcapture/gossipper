#!/usr/bin/env bash
# Apply recommended sysctl values for high-scale cleartext RTP / SIP load (requires root).
# See docs/high-scale-cleartext-rtp.md and examples/sysctl/gossipper-high-scale.conf
set -euo pipefail

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run as root: sudo $0" >&2
  exit 1
fi

apply() {
  sysctl -w "$1=$2"
}

# Socket buffers (per-socket; scale mode opens one UDP socket per RTP stream)
apply net.core.rmem_max 33554432
apply net.core.rmem_default 8388608
apply net.core.wmem_max 33554432
apply net.core.wmem_default 4194304
apply net.core.netdev_max_backlog 250000

# UDP memory (pages; see Documentation/networking/ip-sysctl.txt)
apply net.ipv4.udp_mem "262144 524288 1048576"

# Ephemeral ports and file descriptors (also raise ulimit -n in the shell)
apply net.ipv4.ip_local_port_range "1024 65535"
apply fs.file-max 2097152

# Optional: spread softirq load across CPUs (uncomment if NIC has multiple queues)
# for f in /sys/class/net/*/queues/rx-*/rps_cpus; do
#   [[ -f "$f" ]] && echo ffffffff > "$f" 2>/dev/null || true
# done

echo "OK: rmem_max=$(sysctl -n net.core.rmem_max) wmem_max=$(sysctl -n net.core.wmem_max) udp_mem=$(sysctl -n net.ipv4.udp_mem) file-max=$(sysctl -n fs.file-max)"
