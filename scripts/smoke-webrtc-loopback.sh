#!/usr/bin/env bash
# End-to-end loopback WebRTC job: UAS + UAC supervisor workers on 127.0.0.1,
# builtin webrtc_uas / webrtc_uac scenarios, call_records.jsonl webrtc block.
#
# Usage:
#   make build-go && bash scripts/smoke-webrtc-loopback.sh
#   SMOKE_WEBRTC_TIMEOUT=120 bash scripts/smoke-webrtc-loopback.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

BIN="${GOSSIPPER_BIN:-$ROOT/dist/gossipper}"
PORT="${SMOKE_PORT:-0}"
SIP_PORT="${SMOKE_SIP_PORT:-0}"
CLIENT_PORT="${SMOKE_CLIENT_PORT:-0}"
TIMEOUT="${SMOKE_WEBRTC_TIMEOUT:-90}"

if [[ ! -x "$BIN" ]]; then
	echo "smoke-webrtc: missing executable $BIN (run: make build-go)" >&2
	exit 1
fi
if ! command -v curl >/dev/null 2>&1; then
	echo "smoke-webrtc: curl is required" >&2
	exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
	echo "smoke-webrtc: jq is required" >&2
	exit 1
fi

pick_port() {
	python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

if [[ "$PORT" == "0" ]]; then
	PORT="$(pick_port)"
fi
if [[ "$SIP_PORT" == "0" ]]; then
	SIP_PORT="$(pick_port)"
fi
if [[ "$CLIENT_PORT" == "0" ]]; then
	CLIENT_PORT="$(pick_port)"
fi

DATA="$(mktemp -d "${TMPDIR:-/tmp}/gossipper-webrtc-smoke.XXXXXX")"
BASE="http://127.0.0.1:${PORT}"
PID=""
SERVER_JOB=""
CLIENT_JOB=""

cleanup() {
	if [[ -n "$SERVER_JOB" && -n "$PID" ]]; then
		curl -sf -X POST "${BASE}/api/v2/jobs/${SERVER_JOB}/stop" >/dev/null 2>&1 || true
	fi
	if [[ -n "$PID" ]]; then
		kill "$PID" 2>/dev/null || true
		wait "$PID" 2>/dev/null || true
	fi
	rm -rf "$DATA"
}
trap cleanup EXIT

curl_json() {
	local method="$1" url="$2"
	shift 2
	curl -sf -X "$method" "$url" "$@"
}

wait_ready() {
	local i
	for i in $(seq 1 60); do
		if curl -sf "${BASE}/healthz" 2>/dev/null | grep -q '^ok'; then
			return 0
		fi
		if [[ -n "$PID" ]] && ! kill -0 "$PID" 2>/dev/null; then
			echo "smoke-webrtc: gossipper ui exited before ready" >&2
			return 1
		fi
		sleep 0.2
	done
	echo "smoke-webrtc: timeout waiting for ${BASE}/healthz" >&2
	return 1
}

wait_job() {
	local id="$1" want="$2"
	local deadline=$((SECONDS + TIMEOUT))
	local status=""
	while (( SECONDS < deadline )); do
		status="$(curl_json GET "${BASE}/api/v2/jobs/${id}" | jq -r '.job.status // empty')"
		case "$status" in
		"$want") return 0 ;;
		failed|stopped)
			echo "smoke-webrtc: job ${id} ended with status=${status}" >&2
			curl_json GET "${BASE}/api/v2/jobs/${id}/artifacts/log" -o "${DATA}/fail-${id}.log" 2>/dev/null || true
			if [[ -f "${DATA}/fail-${id}.log" ]]; then
				tail -40 "${DATA}/fail-${id}.log" >&2 || true
			fi
			return 1
			;;
		esac
		sleep 0.25
	done
	echo "smoke-webrtc: timeout waiting for job ${id} status=${want} (last=${status})" >&2
	return 1
}

"$BIN" ui --listen "127.0.0.1:${PORT}" --data-dir "$DATA" --no-auth >/dev/null 2>&1 &
PID=$!
wait_ready

echo "smoke-webrtc: ui ready (port=${PORT}, sip=${SIP_PORT}, client=${CLIENT_PORT})"

curl_json POST "${BASE}/api/v2/servers" \
	-H 'Content-Type: application/json' \
	-d "$(jq -n \
		--argjson sip "$SIP_PORT" \
		'{
			id: "rtc-uas",
			name: "WebRTC UAS loopback",
			scenario_ref: "webrtc_uas",
			max_concurrent: 4,
			transports: [
				{transport: "u1", local_ip: "127.0.0.1", local_port: $sip, enabled: true},
				{transport: "webrtc", enabled: true, prefers_pcma: true}
			]
		}')" >/dev/null

curl_json POST "${BASE}/api/v2/clients" \
	-H 'Content-Type: application/json' \
	-d "$(jq -n \
		--argjson sip "$SIP_PORT" \
		--argjson client "$CLIENT_PORT" \
		'{
			id: "rtc-uac",
			name: "WebRTC UAC loopback",
			scenario_ref: "webrtc_uac",
			remote_ip: "127.0.0.1",
			remote_port: $sip,
			rate: 1,
			max_concurrent: 1,
			transports: [
				{transport: "u1", local_ip: "127.0.0.1", local_port: $client, enabled: true},
				{transport: "webrtc", enabled: true, prefers_pcma: true}
			]
		}')" >/dev/null

SERVER_JOB="$(curl_json POST "${BASE}/api/v2/jobs" \
	-H 'Content-Type: application/json' \
	-d '{"id":"webrtc-uas-job","profile_id":"rtc-uas","profile_kind":"server","scenario_id":"webrtc_uas"}' \
	| jq -r '.id')"
CLIENT_JOB="$(curl_json POST "${BASE}/api/v2/jobs" \
	-H 'Content-Type: application/json' \
	-d '{"id":"webrtc-uac-job","profile_id":"rtc-uac","profile_kind":"client","scenario_id":"webrtc_uac","engine":{"total_calls":1,"max_concurrent":1,"rate":1}}' \
	| jq -r '.id')"

wait_job "$SERVER_JOB" running
echo "smoke-webrtc: UAS job running (${SERVER_JOB})"

wait_job "$CLIENT_JOB" succeeded
echo "smoke-webrtc: UAC job succeeded (${CLIENT_JOB})"

curl_json GET "${BASE}/api/v2/jobs/${CLIENT_JOB}/artifacts/call_records" -o "${DATA}/call_records.jsonl"
if [[ ! -s "${DATA}/call_records.jsonl" ]]; then
	echo "smoke-webrtc: call_records.jsonl missing or empty" >&2
	exit 1
fi

jq -s -e '
  [.[] | select(.webrtc != null)] as $recs
  | ($recs | length >= 1)
  and ([$recs[] | select(.webrtc.offer_created == true and .webrtc.answer_accepted == true)] | length >= 1)
  and ([$recs[] | select((.webrtc.rtp_packets_sent // 0) > 0 or (.webrtc.rtp_packets_recv // 0) > 0)] | length >= 1)
' "${DATA}/call_records.jsonl" >/dev/null || {
	echo "smoke-webrtc: call_records webrtc validation failed:" >&2
	cat "${DATA}/call_records.jsonl" >&2
	exit 1
}

curl -sf -X POST "${BASE}/api/v2/jobs/${SERVER_JOB}/stop" >/dev/null
wait_job "$SERVER_JOB" stopped || wait_job "$SERVER_JOB" succeeded

echo "smoke-webrtc: loopback WebRTC call OK"
