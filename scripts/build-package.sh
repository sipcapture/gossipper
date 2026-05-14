#!/usr/bin/env bash
# Back-compat wrapper (historical name with a hyphen).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "${SCRIPT_DIR}/build_package.sh" "$@"
