# SIP stress–style load testing and HTTP control

This guide describes **Gossipper** features used for **long-running SIP load**, **centralized control**, and **observability**—patterns common in dedicated SIP stress stacks (persistent listeners, HTTP APIs, multi-generator topologies). It complements [`gossipper-vs-sipp.md`](gossipper-vs-sipp.md) (SIPp-oriented XML/CLI) and [`run-profile.md`](run-profile.md) (JSON profiles).

Gossipper is **not** a line-by-line clone of any particular commercial tool. The workflow below is **how to use Gossipper** for the same class of jobs: soak tests, ramped load, hybrid lab rigs, and operator-driven rate changes without restarting the process.

## When to use which entry point

| Goal | Entry | Notes |
| --- | --- | --- |
| One-shot XML scenario from the shell (SIPp-style flags) | `gossipper sipp …` or `gossipper …` | Same parser; `sipp` is explicit in scripts. See [`cli.md`](cli.md). |
| **systemd** / long-run **management** + HTTP API + optional load engines | `gossipper server -config /path/to.json` | Flat JSON; roles inferred or set explicitly. See [`run-profile.md`](run-profile.md) § flat JSON. |
| Add Control UI users (internal auth) | `gossipper auth user-add -config … -username … -password …` | Requires `auth.type: internal` in the same JSON you pass to `server -config`. |

## 1. Flat JSON: management, load-only, and hybrid

Gossipper loads a **single JSON file** for `gossipper server -config`:

- **Management** — SIP UAS (built-in `management` / `uas` style scenarios) + **one HTTP listener** for `/api/v1/*` and the Control UI at `/` when the UI is embedded.
- **Load** — UAC-style profile (`role` load / `uac`, `remote_addr`, rates, …) **without** starting a separate HTTP API on that profile (composite layout uses the server’s API only).
- **Hybrid** — Top-level **`server`** plus **`clients[]`** in **one OS process**: several SIP engines, one API. See [`examples/gossipper-hybrid.json`](../examples/gossipper-hybrid.json).

Canonical samples (also shipped under `/usr/local/gossipper/etc/` in `.deb`/`.rpm`; see root [`README.md`](../README.md)):

| File | Role |
| --- | --- |
| [`examples/gossipper-management.json`](../examples/gossipper-management.json) | Management only + `api_addr` |
| [`examples/gossipper-uac.json`](../examples/gossipper-uac.json) | Flat UAC / load preset (edit `remote_addr`, `rate`, …) |
| [`examples/gossipper-hybrid.json`](../examples/gossipper-hybrid.json) | Multi-listener management + several client engines |

**Multi-engine stats:** `GET /api/v1/stats` returns a **`multi`** object when extra engines exist; `POST /api/v1/control` applies **rate** and **pause** to **all** engines. Scenario **PUT** / **apply** and SIP **transport** toggles still target the **primary** management engine only—see [`run-profile.md`](run-profile.md) and [`cli.md`](cli.md) § Management HTTP API.

## 2. Unlimited / soak mode (`-m 0`)

For open-ended stress until you stop the process (or hit `-timeout_global`), use **`-m 0`** on the CLI or **`"total_calls": 0`** in JSON. Gossipper requires this **explicitly** (omitting `-m` is not the same as unlimited). See [`run-profile.md`](run-profile.md) (rules for `total_calls`).

Example (CLI):

```bash
gossipper sipp -sn uac -rsa 127.0.0.1:5060 -m 0 -r 50
```

## 3. HTTP API: control plane for running load

With **`-api_addr`** (or **`api_addr`** in flat JSON), the same process exposes:

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/health` | Liveness |
| GET | `/api/v1/stats` | Engine snapshot(s); **`multi`** when hybrid / extras |
| GET/PUT | `/api/v1/scenario` | Read/write scenario XML (primary engine); `PUT` may use `?apply=true` |
| POST | `/api/v1/scenario/apply` | Hot-apply XML body or file-backed scenario |
| GET/POST | `/api/v1/control` | Read or set **rate** / **paused** (all engines on POST) |
| GET/POST | `/api/v1/transports` | Listener enable/disable (management) |
| GET/POST/DELETE | `/api/v1/clients` | List, start, or stop **dynamic** UAC snippets (when enabled) |
| GET | `/api/v1/auth/status` | Whether internal auth is active |
| POST | `/api/v1/auth/login` | Username/password → JWT (internal auth) |

Errors are JSON **`{"error":"…"}`** with an HTTP status code.

**Authentication:**

- **Legacy:** `api_token` in JSON and/or **`-api_token`**; pass token as **Bearer** or WebSocket query **`?token=`** (see Control UI).
- **Internal:** top-level **`auth`** with **`type": "internal"`**, **`sqlite_path`**, **`jwt_secret`** (≥16 chars). Users live in SQLite; create them with **`gossipper auth user-add`**. Packaged default path for the DB often lives under **`/usr/local/gossipper/data/`**—see [`examples/gossipper-management-auth.sample.json`](../examples/gossipper-management-auth.sample.json).

## 4. Live WebSocket (`/api/v1/live`)

**`GET /api/v1/live`** upgrades to WebSocket and pushes **JSON frames** (~750 ms) containing optional keys such as **`stats`**, **`control`**, **`transports`**, plus **`ts`**. This avoids polling `GET /stats` when building dashboards or the embedded Control UI.

- Same auth rules as REST: **Bearer** JWT (internal) or legacy token in the handshake query string.

Implementation detail: snapshot assembly lives in `internal/api/live_ws.go` (`snapshotForLive`).

## 5. Dynamic UAC clients (`POST /api/v1/clients`)

At runtime you can start **additional** UAC engines from a **JSON snippet** (transport, addresses, embedded or named scenario, …) without editing the on-disk profile. **DELETE** stops a client by id.

**When it works:** the management launcher registers handlers only when **`api_addr`** is set **and** the process is in **management server mode** (the same conditions that create a `LoadCoordinator` in `internal/launcher/sip_run.go` / `sip_run_multi.go`). A **load-only** flat JSON without management + API does not expose dynamic client APIs.

Typical use:

- Short-lived traffic mixes during a soak test.
- Ad-hoc generators pointed at the same SUT while the management UAS stays up.

Dynamic client ids are merged into **`GET /api/v1/stats`** and **`/api/v1/live`** payloads when active.

## 6. Control UI (browser)

The **Vite/React** app under [`web/control-ui/`](../web/control-ui/) talks to the same API: live WebSocket, stats tables, control (pause/rate), dynamic clients tab, scenario editor. Build with **`make frontend`** so the UI is embedded; packages may also ship static files under **`/usr/local/gossipper/dist/`**. Developer notes: [`web/control-ui/README.md`](../web/control-ui/README.md).

## 7. systemd quick map

| Unit | Config file (packaged default) |
| --- | --- |
| `gossipper-server.service` | `/usr/local/gossipper/etc/gossipper-server.json` |
| `gossipper-client.service` | `/usr/local/gossipper/etc/gossipper-client.json` |
| `gossipper-hybrid.service` | `/usr/local/gossipper/etc/gossipper-hybrid.json` |

All use **`ExecStart=… gossipper server -config …`**. Do not run two units that bind the **same SIP ports** on one host.

## 8. Related documentation

- [`run-profile.md`](run-profile.md) — flat JSON schema, merge order, **`clients`** rules, **`listeners`**, role inference.
- [`cli.md`](cli.md) — subcommands, `sipp` vs root, **`server`**, **`auth`**, management API section.
- [`event-logging.md`](event-logging.md) — OTLP / JSONL sinks for long runs.
- [`qos-reporting.md`](qos-reporting.md) — HEP media/QoS-style reporting alongside SIP.
- [`gossipper-vs-sipp.md`](gossipper-vs-sipp.md) — SIPp comparison and philosophy.
