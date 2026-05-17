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
  --listen :8080 \
  --auth-secret "$GOSSIPPER_JWT_SECRET"

# in another terminal: create the first admin user
gossipper auth user-add \
  --config /var/lib/gossipper/settings.sqlite \
  --username admin --password admin0000
```

Then open <http://localhost:8080> in your browser. The UI auto-detects whether
the admin console (`/api/v2`) is available and falls back to the legacy
`/api/v1` view when running against `gossipper server`.

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
| `GET /auth/status`, `POST /auth/login`, `GET /me` | JWT bootstrap |
| `GET/POST/PUT/DELETE /servers[/{id}]` | UAS profile CRUD |
| `GET/POST/PUT/DELETE /clients[/{id}]` | UAC profile CRUD |
| `GET/POST/PUT/DELETE /scenarios[/{id}]` | scenarios CRUD (XML + metadata) |
| `GET/POST/DELETE /media/{kind}[/{name}]` | WAV / PCAP library |
| `GET/POST/DELETE /jobs[/{id}]`, `POST /jobs/{id}/stop` | jobs lifecycle |
| `GET /jobs/{id}/recordings[/{name}]` | per-call WAV artifacts |
| `GET/POST/PUT/DELETE /users[/{id}]` | admin user management |
| `GET /audit` | last 100 mutating actions |

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
| `w1/wn/ws1/wsn` | beta | module ready in `internal/transport/ws.go`; engine wiring lands in Phase 4.1 |
| `webrtc` | beta | UI form field is reserved; pion integration scheduled for Phase 4.1 |

## Legacy CLI

The legacy CLI is unchanged and remains the right entry point for one-shot
test runs in CI. The UI exposes it via the "← legacy engine UI" link in the
sidebar when the operator wants to drive a single engine directly.
