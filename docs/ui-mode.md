# UI mode (`gossipper ui`)

`gossipper ui` is a long-running master process that exposes an admin console
(`/api/v2`) on top of the existing SIP engine. It is the recommended way to
run gossipper as a service for testers and SRE teams. The legacy CLI
(`gossipper -sf scenario.xml -tr_t1 ...`) keeps working and is exposed in the
same UI through a "legacy engine" toggle when the master is not available.

## Quick start

```bash
gossipper ui \
  --data-dir /var/lib/gossipper \
  --listen :8080
```

Then open <http://localhost:8080> and sign in with the default admin user
(`admin` / `sipcapture`, or values from `GOSSIPPER_BOOTSTRAP_USERNAME` /
`GOSSIPPER_BOOTSTRAP_PASSWORD`). On first start the settings DB is created
automatically with users and JWT settings.

After building (`make frontend && make build-go`), run **`make smoke`** to
exercise a real `gossipper ui` process against `/healthz`, login, scenarios,
RBAC, and the embedded Control UI (`scripts/smoke-api-v2.sh`).

**`make smoke-webrtc`** runs a loopback WebRTC end-to-end check: supervisor
UAS + UAC jobs with builtin `webrtc_uas` / `webrtc_uac`, then validates
`call_records.jsonl` (offer/answer flags and RTP counters). Release CI runs
`make smoke` and `make smoke-webrtc` after the production build (see
`.github/workflows/release.yml`).

The UI talks to **`/api/v2`** on the same listener. There is no automatic fallback to a legacy v1-only React shell — build with **`make frontend`** so `GET /` serves the embedded admin console.

## Data layout

The `--data-dir` flag (default `./gossipper-data`) controls where every UI
artefact lives. The layout is stable; nothing else writes into the directory.

```
<data-dir>/
  settings.sqlite                 # users, jobs, job_artifacts, audit_log
  profiles/
    servers/<id>.json             # UAS profiles managed via /api/v2/servers
    clients/<id>.json             # UAC profiles managed via /api/v2/clients
  scenarios/
    <id>.xml                      # SIP scenario body (preprocessed for media)
    <id>.json                     # sidecar metadata
  media/
    wav/<name>.wav                # uploaded WAV assets
    pcap/<name>.pcap              # uploaded PCAP assets
  artifacts/
    jobs/<job-id>/
      stats.jsonl                 # JSON-lines stats stream from the worker
      worker.log                  # mirrored stderr of the worker
      summary.json                # final engine summary
      recordings/<call-id>.wav    # per-call WAV recordings (when enabled)
  tmp/                            # supervisor spec files + temp scenarios
```

## Architecture

```
+-----------------------+        +-------------------------+
| gossipper ui (master) |  fork  | gossipper worker (job)  |
|  /api/v2/* HTTP API   +------> | reads spec, runs engine |
|  embedded React UI    |  pipe  | streams stats.jsonl     |
|  SQLite users/jobs    | <----- | exits 0/!=0             |
+-----------+-----------+        +-------------------------+
            |
   uistore (JSON files)
```

* **Master** owns the UI, REST API and supervisor.
* **Workers** are spawned through `gossipper worker --spec <path>` and reuse
  the existing `internal/launcher` code path; profiles are converted into
  `cli.Config` via `supervisor.BuildConfigFromSpec`.
* **Stats** are emitted as JSON lines on the worker's stdout and persisted to
  `artifacts/jobs/<id>/stats.jsonl`.

## REST API (`/api/v2`)

| Method / path | Description |
| --- | --- |
| `GET /health` | liveness + auth mode |
| `GET /auth/status`, `POST /auth/login`, `GET /me`, `POST /me/password` | JWT bootstrap + change own password |
| `GET /load-test`, `POST /load-test/run` | sipstress-style background load jobs |
| `GET /tools`, `POST /tools/{id}/run` | prep/tool jobs (`pcap2scenario`, `report-html`, …) |
| `GET /reports`, `GET /jobs/{id}/artifacts/{kind}` | report catalog + artifact download |
| `GET /live`, `GET /jobs/{id}/events` | WebSocket job counts + JSONL worker events |
| `GET/POST/PUT/DELETE /servers[/{id}]` | UAS profile CRUD + `POST …/start` / `stop` shortcuts |
| `GET/POST/PUT/DELETE /clients[/{id}]` | UAC profile CRUD + start/stop shortcuts |
| `GET/POST/PUT/DELETE /scenarios[/{id}]` | scenarios CRUD (XML + metadata) + history/fork |
| `GET /builtin-scenarios[/{id}]` | read-only built-in scenario catalog |
| `GET/POST/DELETE /media/{kind}[/{name}]` | WAV / PCAP library |
| `GET/POST/DELETE /jobs[/{id}]`, `POST /jobs/{id}/stop` | jobs lifecycle (`engine` overrides on POST) |
| `GET /jobs/{id}/recordings[/{name}]` | per-call WAV artifacts |
| `GET/POST/PUT/DELETE /users[/{id}]` | admin user management (**admin role** required) |
| `GET /audit`, `GET /settings`, `POST /settings/rotate-jwt-secret` | audit log (**admin** for audit/rotate) + runtime info |
| `POST /scenarios/import-from-pcap-job` | import `scenario_uac.xml` / `scenario_uas.xml` from a succeeded pcap2scenario job |

Hash routes in the browser: `#/dashboard`, `#/load`, `#/jobs/{id}`, `#/reports?report={id}`, etc.

When **`gossipper server`** also exposes **`/api/v1/*`** (hybrid management), the Dashboard shows a **v1 control panel** (rate/pause per engine, dynamic clients) alongside v2 jobs.

### Roles (internal auth)

| Role | Access |
| --- | --- |
| `admin` | Full v2 API including users, audit log, JWT secret rotation |
| `operator` | Profiles, scenarios, jobs, load tests, tools, media, own password change |

The JWT issued by `POST /auth/login` includes a `role` claim. UI nav hides admin pages for non-admin users; the API returns **403 Forbidden** on admin-only routes when the token role is not `admin`.

New users created without an explicit role default to **`operator`**.

### Hybrid v1 control (`POST /api/v1/control`)

When the live engine API is mounted (e.g. `gossipper server -api_addr`), rate and pause can target a single engine:

```bash
curl -X POST http://localhost:8080/api/v1/control \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"engine_id":"side","rate":10.0}'
```

Omit `engine_id` to apply to all engines. Response mirrors `GET /api/v1/control` (multi-engine list when extras are configured).

All mutating endpoints append to `audit_log` (when `auth.type: internal`).

## Scenarios + media linkage

Inside scenario XML you can reference uploaded assets with `[[media:...]]`
placeholders; the worker rewrites them to absolute paths before parsing:

```xml
<play_pcap_audio>[[media:wav/ringback]]</play_pcap_audio>
<send><![CDATA[
  INVITE sip:bob@x SIP/2.0
  Content-Type: application/sdp

  m=audio 6000 RTP/AVP 0
  a=PCAP:[[media:pcap/dtmf]]
]]></send>
```

Allowed names match `[A-Za-z0-9._-]+` — path traversal attempts are rejected.

## Transports roadmap

| code | status | notes |
| --- | --- | --- |
| `u1/un/t1/tn/l1/ln` | stable | shared / per-call UDP, TCP, TLS |
| `w1/wn/ws1/wsn` | stable | WS/WSS SIP transport wired in engine |
| `webrtc` | beta | pion bridge per-call; profile ICE + built-in `webrtc_uac` / `webrtc_uas` scenarios |

## Legacy CLI

The legacy CLI is unchanged and remains the right entry point for one-shot
test runs in CI and scripted `-m`/`-r` sweeps without the supervisor/UI.
