#!/usr/bin/env bash
# End-to-end smoke for gossipper ui + /api/v2 (real process, not httptest).
#
# Usage:
#   make build-go && bash scripts/smoke-api-v2.sh
#   SMOKE_NO_AUTH=1 bash scripts/smoke-api-v2.sh   # skip login/RBAC checks
#   SMOKE_UI=0 bash scripts/smoke-api-v2.sh        # skip GET /
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

BIN="${GOSSIPPER_BIN:-$ROOT/dist/gossipper}"
PORT="${SMOKE_PORT:-0}"
SMOKE_UI="${SMOKE_UI:-1}"
SMOKE_NO_AUTH="${SMOKE_NO_AUTH:-0}"
BOOT_USER="${GOSSIPPER_BOOTSTRAP_USERNAME:-admin}"
BOOT_PASS="${GOSSIPPER_BOOTSTRAP_PASSWORD:-sipcapture}"

if [[ ! -x "$BIN" ]]; then
	echo "smoke: missing executable $BIN (run: make build-go)" >&2
	exit 1
fi
if ! command -v curl >/dev/null 2>&1; then
	echo "smoke: curl is required" >&2
	exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
	echo "smoke: jq is required" >&2
	exit 1
fi

if [[ "$PORT" == "0" ]]; then
	PORT="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
fi

DATA="$(mktemp -d "${TMPDIR:-/tmp}/gossipper-smoke.XXXXXX")"
BASE="http://127.0.0.1:${PORT}"
PID=""

cleanup() {
	if [[ -n "$PID" ]]; then
		kill "$PID" 2>/dev/null || true
		wait "$PID" 2>/dev/null || true
	fi
	rm -rf "$DATA"
}
trap cleanup EXIT

ui_args=(ui --listen "127.0.0.1:${PORT}" --data-dir "$DATA")
if [[ "$SMOKE_NO_AUTH" == "1" ]]; then
	ui_args+=(--no-auth)
fi

"$BIN" "${ui_args[@]}" >/dev/null 2>&1 &
PID=$!

wait_ready() {
	local i
	for i in $(seq 1 60); do
		if curl -sf "${BASE}/healthz" 2>/dev/null | grep -q '^ok'; then
			return 0
		fi
		if ! kill -0 "$PID" 2>/dev/null; then
			echo "smoke: gossipper ui exited before ready" >&2
			return 1
		fi
		sleep 0.2
	done
	echo "smoke: timeout waiting for ${BASE}/healthz" >&2
	return 1
}

curl_json() {
	local method="$1" url="$2"
	shift 2
	curl -sf -X "$method" "$url" "$@"
}

wait_ready

echo "smoke: healthz ok (port=${PORT})"

if [[ "$SMOKE_NO_AUTH" == "1" ]]; then
	curl_json GET "${BASE}/api/v2/health" | jq -e '.status == "ok" and .auth == "none"' >/dev/null
	curl_json GET "${BASE}/api/v2/me" | jq -e '.username == "anonymous" and .role == "admin"' >/dev/null
	curl_json GET "${BASE}/api/v2/builtin-scenarios" | jq -e '(.scenarios | length) >= 1 and ([.scenarios[].id] | index("webrtc_uac")) != null' >/dev/null
	curl_json GET "${BASE}/api/v2/scenarios" | jq -e 'has("scenarios")' >/dev/null
else
	curl_json GET "${BASE}/api/v2/health" | jq -e '.status == "ok" and .auth == "internal"' >/dev/null
	curl_json GET "${BASE}/api/v2/auth/status" | jq -e '.auth == "internal"' >/dev/null

	if curl -sf "${BASE}/api/v2/scenarios" >/dev/null 2>&1; then
		echo "smoke: expected 401 on unauthenticated /scenarios" >&2
		exit 1
	fi
	if ! curl -s -o /dev/null -w '%{http_code}' "${BASE}/api/v2/scenarios" | grep -q '^401$'; then
		echo "smoke: expected HTTP 401 on unauthenticated /scenarios" >&2
		exit 1
	fi

	TOKEN="$(curl_json POST "${BASE}/api/v2/auth/login" \
		-H 'Content-Type: application/json' \
		-d "{\"username\":\"${BOOT_USER}\",\"password\":\"${BOOT_PASS}\"}" \
		| jq -r '.token')"
	if [[ -z "$TOKEN" || "$TOKEN" == "null" ]]; then
		echo "smoke: login failed for ${BOOT_USER}" >&2
		exit 1
	fi

	auth=(-H "Authorization: Bearer ${TOKEN}")
	curl_json GET "${BASE}/api/v2/me" "${auth[@]}" | jq -e ".username == \"${BOOT_USER}\" and .role == \"admin\"" >/dev/null
	curl_json GET "${BASE}/api/v2/builtin-scenarios" "${auth[@]}" | jq -e '(.scenarios | length) >= 1 and ([.scenarios[].id] | index("webrtc_uac")) != null' >/dev/null
	curl_json GET "${BASE}/api/v2/scenarios" "${auth[@]}" | jq -e 'has("scenarios")' >/dev/null
	curl_json GET "${BASE}/api/v2/settings" "${auth[@]}" | jq -e '.ui_data_dir != ""' >/dev/null
	curl_json GET "${BASE}/api/v2/load-test" "${auth[@]}" | jq -e 'has("defaults") or has("schema") or type == "object"' >/dev/null

	# RBAC: create operator, verify admin-only routes return 403.
	curl_json POST "${BASE}/api/v2/users" "${auth[@]}" \
		-H 'Content-Type: application/json' \
		-d '{"username":"smoke_op","password":"smokepass1","role":"operator"}' \
		| jq -e '.username == "smoke_op" and .role == "operator"' >/dev/null

	OP_TOKEN="$(curl_json POST "${BASE}/api/v2/auth/login" \
		-H 'Content-Type: application/json' \
		-d '{"username":"smoke_op","password":"smokepass1"}' \
		| jq -r '.token')"
	op_auth=(-H "Authorization: Bearer ${OP_TOKEN}")

	if curl -sf "${BASE}/api/v2/users" "${op_auth[@]}" >/dev/null 2>&1; then
		echo "smoke: operator must not list users" >&2
		exit 1
	fi
	if ! curl -s -o /dev/null -w '%{http_code}' "${BASE}/api/v2/users" "${op_auth[@]}" | grep -q '^403$'; then
		echo "smoke: expected HTTP 403 for operator GET /users" >&2
		exit 1
	fi
	curl_json GET "${BASE}/api/v2/scenarios" "${op_auth[@]}" | jq -e 'has("scenarios")' >/dev/null
fi

if [[ "$SMOKE_UI" == "1" ]]; then
	code="$(curl -s -o /dev/null -w '%{http_code}' "${BASE}/")"
	if [[ "$code" != "200" ]]; then
		echo "smoke: expected HTTP 200 for GET /, got ${code} (run make frontend?)" >&2
		exit 1
	fi
fi

echo "smoke: all checks passed"
