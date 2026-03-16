#!/usr/bin/env bash
#
# Benchmark script: gossipper vs SIPp (UAC load generation)
#
# Requires: UAS (SIP server) running or use -start-uas to spawn gossipper as UAS.
# Optional: sipp must be in PATH for SIPp comparison.
#
# Usage:
#   ./scripts/benchmark-sipp-vs-gossipper.sh [options] [target]
#
# Options:
#   -start-uas     Start gossipper as UAS in background (default: expect UAS already running)
#   -calls N       Total calls (default: 500)
#   -rate N        Calls per second (default: 50)
#   -concurrent N   Max simultaneous calls (default: 100)
#   -timeout N     Global timeout seconds (default: 60)
#   -runs N        Number of runs per tool (default: 2)
#   -gossipper PATH Path to gossipper binary (default: ./dist/gossipper or go run)
#
# Example:
#   ./scripts/benchmark-sipp-vs-gossipper.sh -start-uas -calls 1000 -rate 100 127.0.0.1:5060
#
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

# Defaults
START_UAS=false
CALLS=500
RATE=50
CONCURRENT=100
TIMEOUT=60
RUNS=2
GOSSIPPER_BIN=""
TARGET="127.0.0.1:5060"

# Parse args
while [[ $# -gt 0 ]]; do
  case "$1" in
    -start-uas) START_UAS=true; shift ;;
    -calls)     CALLS="$2"; shift 2 ;;
    -rate)      RATE="$2"; shift 2 ;;
    -concurrent) CONCURRENT="$2"; shift 2 ;;
    -timeout)   TIMEOUT="$2"; shift 2 ;;
    -runs)      RUNS="$2"; shift 2 ;;
    -gossipper) GOSSIPPER_BIN="$2"; shift 2 ;;
    -h|--help)
      head -30 "$0" | grep -E '^#( |$)'
      exit 0
      ;;
    *)
      if [[ "$1" != -* ]]; then
        TARGET="$1"
      fi
      shift
      ;;
  esac
done

UAS_PID=""
TMP_JSON=""
cleanup() {
  if [[ -n "${UAS_PID}" ]] && kill -0 "${UAS_PID}" 2>/dev/null; then
    kill "${UAS_PID}" 2>/dev/null || true
  fi
  [[ -n "${TMP_JSON}" ]] && rm -f "${TMP_JSON}"
}
trap cleanup EXIT

# Resolve gossipper binary
if [[ -z "${GOSSIPPER_BIN}" ]]; then
  if [[ -x "${ROOT_DIR}/dist/gossipper" ]]; then
    GOSSIPPER_BIN="${ROOT_DIR}/dist/gossipper"
  else
    GOSSIPPER_BIN="go run ./cmd/gossip"
  fi
fi

# Check sipp
SIPP_CMD=""
if command -v sipp >/dev/null 2>&1; then
  SIPP_CMD="sipp"
elif command -v sipp_3.6 >/dev/null 2>&1; then
  SIPP_CMD="sipp_3.6"
fi

run_gossipper_uac() {
  local json_out
  json_out="$(mktemp)"
  TMP_JSON="${json_out}"

  if [[ "${GOSSIPPER_BIN}" == *"go run"* ]]; then
    go run ./cmd/gossip -sn uac -rsa "${TARGET}" -m "${CALLS}" -r "${RATE}" -l "${CONCURRENT}" \
      -timeout_global "${TIMEOUT}" -summary_json "${json_out}" 2>/dev/null || true
  else
    "${GOSSIPPER_BIN}" -sn uac -rsa "${TARGET}" -m "${CALLS}" -r "${RATE}" -l "${CONCURRENT}" \
      -timeout_global "${TIMEOUT}" -summary_json "${json_out}" 2>/dev/null || true
  fi

  if [[ -f "${json_out}" ]] && command -v jq >/dev/null 2>&1; then
    jq -r '
      "success=\(.success_calls) failed=\(.failed_calls) cps=\(.calls_per_second) duration=\(.duration / 1000000000)"
    ' "${json_out}" 2>/dev/null || echo "success=0 failed=0 cps=0 duration=0"
  elif [[ -f "${json_out}" ]]; then
    # Fallback: parse JSON with grep/sed if jq not available
    local s f c d
    s=$(grep -o '"success_calls":[0-9]*' "${json_out}" 2>/dev/null | cut -d: -f2 || echo 0)
    f=$(grep -o '"failed_calls":[0-9]*' "${json_out}" 2>/dev/null | cut -d: -f2 || echo 0)
    c=$(grep -o '"calls_per_second":[0-9.]*' "${json_out}" 2>/dev/null | cut -d: -f2 || echo 0)
    d=$(grep -o '"duration":[0-9]*' "${json_out}" 2>/dev/null | cut -d: -f2 | awk '{print $1/1e9}' || echo 0)
    echo "success=${s} failed=${f} cps=${c} duration=${d}"
  else
    echo "success=0 failed=0 cps=0 duration=0"
  fi
}

run_sipp_uac() {
  local out
  out=$(mktemp)
  # SIPp: -m calls, -r rate (cps), -l max concurrent, -timeout in seconds
  "${SIPP_CMD}" -sn uac "${TARGET}" -m "${CALLS}" -r "${RATE}" -l "${CONCURRENT}" \
    -timeout "${TIMEOUT}" >"${out}" 2>&1 || true

  # Parse SIPp stats block (format varies by version; look for "Successful call" etc.)
  local success=0 failed=0 cps=0 duration=0
  if grep -q "Successful call" "${out}" 2>/dev/null; then
    success=$(grep "Successful call" "${out}" | tail -1 | grep -oE '[0-9]+' | head -1 || echo 0)
  fi
  if grep -q "Failed call" "${out}" 2>/dev/null; then
    failed=$(grep "Failed call" "${out}" | tail -1 | grep -oE '[0-9]+' | head -1 || echo 0)
  fi
  if grep -q "Call rate" "${out}" 2>/dev/null; then
    cps=$(grep "Call rate" "${out}" | tail -1 | grep -oE '[0-9.]+' | head -1 || echo 0)
  fi
  if grep -q "Run time" "${out}" 2>/dev/null; then
    duration=$(grep "Run time" "${out}" | tail -1 | grep -oE '[0-9.]+' | head -1 || echo 0)
  fi

  rm -f "${out}"
  echo "success=${success} failed=${failed} cps=${cps} duration=${duration}"
}

parse_result() {
  local line="$1"
  local success failed cps duration
  success=$(echo "${line}" | sed -n 's/.*success=\([0-9]*\).*/\1/p')
  failed=$(echo "${line}" | sed -n 's/.*failed=\([0-9]*\).*/\1/p')
  cps=$(echo "${line}" | sed -n 's/.*cps=\([0-9.]*\).*/\1/p')
  duration=$(echo "${line}" | sed -n 's/.*duration=\([0-9.]*\).*/\1/p')
  echo "${success:-0} ${failed:-0} ${cps:-0} ${duration:-0}"
}

# Start UAS if requested
if [[ "${START_UAS}" == "true" ]]; then
  echo "==> Starting gossipper UAS on ${TARGET}..."
  if [[ "${GOSSIPPER_BIN}" == *"go run"* ]]; then
    go run ./cmd/gossip -sn uas -p "${TARGET##*:}" -i "127.0.0.1" &
  else
    "${GOSSIPPER_BIN}" -sn uas -p "${TARGET##*:}" -i "127.0.0.1" &
  fi
  UAS_PID=$!
  sleep 2
  if ! kill -0 "${UAS_PID}" 2>/dev/null; then
    echo "Failed to start UAS" >&2
    exit 1
  fi
  echo "    UAS PID ${UAS_PID}"
fi

echo ""
echo "=== Benchmark: gossipper vs SIPp (UAC) ==="
echo "Target: ${TARGET} | Calls: ${CALLS} | Rate: ${RATE} cps | Concurrent: ${CONCURRENT} | Timeout: ${TIMEOUT}s"
echo ""

# Run gossipper
echo "--- gossipper (${RUNS} run(s)) ---"
G_SUM_SUCCESS=0 G_SUM_FAILED=0 G_SUM_CPS=0 G_SUM_DUR=0 G_COUNT=0
for ((i=1; i<=RUNS; i++)); do
  printf "  Run %d: " "${i}"
  result=$(run_gossipper_uac)
  read -r s f c d < <(parse_result "${result}")
  G_SUM_SUCCESS=$((G_SUM_SUCCESS + s))
  G_SUM_FAILED=$((G_SUM_FAILED + f))
  G_SUM_CPS=$(echo "${G_SUM_CPS} + ${c}" | bc 2>/dev/null || echo "${G_SUM_CPS}")
  G_SUM_DUR=$(echo "${G_SUM_DUR} + ${d}" | bc 2>/dev/null || echo "${G_SUM_DUR}")
  G_COUNT=$((G_COUNT + 1))
  echo "success=${s} failed=${f} cps=${c} duration=${d}s"
done
G_AVG_CPS=$(echo "scale=2; ${G_SUM_CPS} / ${G_COUNT}" | bc 2>/dev/null || echo "0")
G_AVG_DUR=$(echo "scale=2; ${G_SUM_DUR} / ${G_COUNT}" | bc 2>/dev/null || echo "0")
echo "  Average: cps=${G_AVG_CPS} duration=${G_AVG_DUR}s"
echo ""

# Run SIPp if available
if [[ -n "${SIPP_CMD}" ]]; then
  echo "--- SIPp (${RUNS} run(s)) ---"
  S_SUM_SUCCESS=0 S_SUM_FAILED=0 S_SUM_CPS=0 S_SUM_DUR=0 S_COUNT=0
  for ((i=1; i<=RUNS; i++)); do
    printf "  Run %d: " "${i}"
    result=$(run_sipp_uac)
    read -r s f c d < <(parse_result "${result}")
    S_SUM_SUCCESS=$((S_SUM_SUCCESS + s))
    S_SUM_FAILED=$((S_SUM_FAILED + f))
    S_SUM_CPS=$(echo "${S_SUM_CPS} + ${c}" | bc 2>/dev/null || echo "${S_SUM_CPS}")
    S_SUM_DUR=$(echo "${S_SUM_DUR} + ${d}" | bc 2>/dev/null || echo "${S_SUM_DUR}")
    S_COUNT=$((S_COUNT + 1))
    echo "success=${s} failed=${f} cps=${c} duration=${d}s"
  done
  S_AVG_CPS=$(echo "scale=2; ${S_SUM_CPS} / ${S_COUNT}" | bc 2>/dev/null || echo "0")
  S_AVG_DUR=$(echo "scale=2; ${S_SUM_DUR} / ${S_COUNT}" | bc 2>/dev/null || echo "0")
  echo "  Average: cps=${S_AVG_CPS} duration=${S_AVG_DUR}s"
  echo ""

  echo "=== Summary ==="
  printf "%-12s %10s %10s %10s\n" "Tool" "Success" "CPS (avg)" "Duration"
  printf "%-12s %10d %10s %10ss\n" "gossipper" "${G_SUM_SUCCESS}" "${G_AVG_CPS}" "${G_AVG_DUR}"
  printf "%-12s %10d %10s %10ss\n" "sipp" "${S_SUM_SUCCESS}" "${S_AVG_CPS}" "${S_AVG_DUR}"
else
  echo "SIPp not found (sipp or sipp_3.6 in PATH). Skipping SIPp benchmark."
  echo ""
  echo "=== Summary ==="
  printf "%-12s %10s %10s\n" "Tool" "Success" "CPS (avg)"
  printf "%-12s %10d %10s\n" "gossipper" "${G_SUM_SUCCESS}" "${G_AVG_CPS}"
fi

echo ""
echo "Done."
