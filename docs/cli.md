# Command-line interface (CLI)

Gossipper is a single binary (`cmd/gossip`) with:

1. **Reserved first-token subcommands** — fixed verbs (`tui`, `shell`, …) handled before the main `flag` parser.
2. **Default path** — everything else is parsed as **SIP/XML scenario flags** (SIPp-style surface; see [`compatibility.md`](compatibility.md) and [`cli-gap-list.md`](cli-gap-list.md)).
3. **Optional `gossipper sipp` prefix** — same scenario flags as (2); **not** for subcommands (see below).

Run **`gossipper -h`** for **subcommands** and pointers to detailed help (**`gossipper sipp -h`** for the full SIPp-compatible scenario flags, **`gossipper server -h`** for server/management-oriented groups + API, **`gossipper profile -h`** for run-profile presets — see [`run-profile.md`](run-profile.md)).

## Subcommands (first argument)

These tokens must appear **first** (after any optional `sipp` strip — you should **not** combine them with `sipp`; use the root command).

| Command | Purpose |
| --- | --- |
| `gossipper shell` | Line-oriented interactive shell (`set`, `run`, `wizard`, …). See [`interactive-shell.md`](interactive-shell.md). |
| `gossipper cli` | Alias for `shell`. |
| `gossipper tui` | Full-screen launcher and runtime UI. See [`tui.md`](tui.md). |
| `gossipper -interactive` / `--interactive` | Same control UI as `tui` (handled as a flag on the root path). |
| `gossipper server …` | Long-run **management** mode: **`gossipper server`** runs the SIP UAS + HTTP API path. An internal marker is prepended before flags unless argv already contains it (see `internal/cli/server_subcmd.go`). Prefer **`gossipper server -config /path.json`** for flat JSON (management vs load is inferred). |
| `gossipper auth …` | Internal Control UI / API users: **`gossipper auth user-add -config <flat.json> -username … -password …`** (requires **`auth.type`**: **`internal`** + **`sqlite_path`** + **`jwt_secret`** in that JSON). |
| `gossipper pcap2scenario …` | PCAP → generated XML scenarios. See [`pcap2scenario.md`](pcap2scenario.md). |
| `gossipper report-html …` | Summary JSON → standalone HTML (separate small flag set). |
| `gossipper summary-to-pdf …` | HTML → PDF (optional embedded renderer when built with `-tags pdf`, else Chromium in `PATH`). |
| `gossipper profile` / `gossipper profile -h` | Prints the **run-profile** flag summary (**`-config`**, **`-run-alias`**, **`-list-aliases`**, plus notes on **`gossipper server -config`**). Full narrative in [`run-profile.md`](run-profile.md). |

**`-h` / `--help` per entry:** root **`gossipper -h`** lists **subcommands** only (no scenario flag dump); **`gossipper server -h`** lists **server / management–oriented** flags in sections (SIP bind, HTTP API, TLS, HEP, …); the **complete** SIPp-compatible list is **`gossipper sipp -h`**. **`gossipper sipp -h`** uses a **SIPp-oriented** preamble and groups scenario flags as **HEP** → **OTLP** → **PPROF** → **SIPP**, omitting **`-api_addr`**, **`-api_token`**, and **standalone RTP sender** flags (**`-rtp_send`**, **`-rtp_addr`**, …); **`gossipper tui -h`**, **`gossipper cli -h`**, **`gossipper shell -h`** print only that subcommand’s short help; **`gossipper pcap2scenario -h`**, **`report-html -h`**, **`summary-to-pdf -h`** show only their small flag sets.

**Version:** `-version` / `--version` anywhere in argv prints build info and exits (handled before subcommand routing).

## Scenario run (no subcommand)

```bash
gossipper sipp -sn uac -rsa 127.0.0.1:5060 -m 10 -r 5
gossipper sipp -sf ./scenario.xml -rsa 127.0.0.1:5060 …
```

Same parsing as **`gossipper sipp …`** for the flag tail. Run profiles (**`-config`**, **`-run-alias`**) apply on this path only — not after `shell` / `tui` / `pcap2scenario` (the **`gossipper profile`** subcommand only prints that flag summary; it does not load JSON). See [`run-profile.md`](run-profile.md).

## `gossipper sipp` (SIPp-style entry only)

**`gossipper sipp`** exists for **explicit SIPp-oriented documentation** and scripts that always prefix the binary name with `sipp`.

Rules (enforced in `internal/sipp`):

- **Allowed:** any argv that is valid **scenario / launcher flags** on the root command (e.g. **`-sn`**, **`-sf`**, **`-rsa`**, **`-rtp_send`**, …).
- **Rejected:** placing **Gossipper-only** subcommands or **`-interactive`** after `sipp` (e.g. `gossipper sipp tui`). Use **`gossipper tui`** without `sipp`. Error: `ErrRootSubcommandAfterSipp`.

Leading **`sipp`** tokens may be repeated and are stripped (e.g. shell aliases); empty argv prints a short SIPp-entry summary; **`-h` / `--help` / `help`** forwards to a **SIPp-oriented** preamble and scenario flags in sections **HEP** → **OTLP** → **PPROF** → **SIPP**, and **hides** **`-api_addr`**, **`-api_token`**, and **`-rtp_send`** (and related **`-rtp_*`**) so the help matches the SIPp entry. Use **`gossipper server -h`** for **management-oriented** groups (API, bind, TLS, …); use **`gossipper sipp -h`** for the **full** SIPp-compatible flag list.

Full narrative vs SIPp: [`gossipper-vs-sipp.md`](gossipper-vs-sipp.md).

## Management server quick reference

| Invocation | Notes |
| --- | --- |
| `gossipper server` | Management mode: SIP port **5060** when **`-p`** omitted, API **`:8080`** when configured, unless overridden by flags or flat **`-config`** JSON. |
| `gossipper server -config /path.json` | Loads flat JSON (same keys as one run-profile alias). Management vs load is inferred (or set via JSON `"role"`). |

Example unit file: [`examples/gossipper-server.service`](../examples/gossipper-server.service).

For **long-run load**, **hybrid `server`+`clients`**, **WebSocket `/api/v1/live`**, **dynamic UAC clients**, and **internal auth**, see [`sipstress-style-load-testing.md`](sipstress-style-load-testing.md).

## Management HTTP API (`-api_addr`): live scenario

For a **single** SIP engine, **`GET /api/v1/stats`** returns the stats object directly. With **composite** flat JSON (**`server`** / **`clients`** in **`gossipper server -config`**, see [`run-profile.md`](run-profile.md)), **`GET /api/v1/stats`** returns **`{"multi":true,"engines":[{"id":"…","stats":{…}},…]}`**, and **`POST /api/v1/control`** applies rate/pause to **every** engine; **`PUT`/`POST` scenario** and **SIP listener toggles** still target the **primary** management engine only.

When **`GET /api/v1/stats`** works, **`PUT /api/v1/scenario?apply=true`** and **`POST /api/v1/scenario/apply`** call **`Engine.TryReplaceLiveScenario`**:

- **In-flight calls** keep the scenario XML they started with (snapshot at call start).
- **New calls** (including the next **incoming** dialog on server transports) use the updated scenario. The **first `<recv>`** used to match new server-side dialogs is taken from the **live** scenario on each packet, so it tracks hot reloads (not the startup file alone).
- **`POST /apply`** is allowed even while **`active_calls` > 0**; there is no longer a global “idle only” gate.
- The new scenario’s **mode** (`client` vs `server`) must still match how the process was started; changing mode requires a process restart.
- Built-in **`-sn`** scenarios without **`-sf`** cannot be edited via **`PUT /scenario`** (no on-disk path); use **`-sf`** or **`POST /apply`** with an **XML body**.

### HTTP route summary (same listener as `/`)

| Method | Path | Purpose |
| --- | --- | --- |
| **GET** | `/api/v1/health` | Liveness |
| **GET** | `/api/v1/stats` | Stats; **`multi`** + **`engines`** when hybrid / extra engines |
| **GET** / **PUT** | `/api/v1/scenario` | Read / write scenario XML (primary engine); **`PUT ?apply=true`** hot-reloads |
| **POST** | `/api/v1/scenario/apply` | Apply XML body or file-backed scenario |
| **GET** / **POST** | `/api/v1/control` | Read / set **rate** and **paused** (**POST** applies to **all** engines in **`multi`** mode) |
| **GET** / **POST** / **DELETE** | `/api/v1/clients` | List / start / stop **dynamic** UAC engines (**management** + **`api_addr`** only) |
| **GET** | `/api/v1/auth/status` | Always mounted: **`{"auth":"none"}`** or **`{"auth":"internal"}`** |
| **POST** | `/api/v1/auth/login` | JSON credentials → JWT when **internal** auth is enabled; **404** when it is not |
| **GET** (WebSocket) | `/api/v1/live` | Periodic JSON frames (**stats**, **control**, **transports**, …) |

**Authorization:** routes above that go through **`wrap`** call **`authorizeRequest`**: **internal** auth validates **JWT** (**`Authorization: Bearer`** or WebSocket **`?token=`**); legacy **`api_token`** accepts the same **Bearer** value or matching query **`token`**. If neither internal auth nor **`api_token`** is configured, wrapped routes stay **open** (use only on trusted networks).

See also [`sipstress-style-load-testing.md`](sipstress-style-load-testing.md).

## SIP transports (server listeners)

Only in **management / server** mode. Sockets stay bound; toggling affects **acceptance of new dialogs** only (existing calls are unchanged).

| Method | Path | Body | Response |
| --- | --- | --- | --- |
| **GET** | `/api/v1/transports` | — | `{"listeners":[{"index", "transport", "local_ip", "local_port", "enabled"}, ...]}` |
| **POST** | `/api/v1/transports` | `{"listeners":[{"index":0,"enabled":false},...]}` **or** shorthand `{"index":0,"enabled":false}` | Same shape as **GET** (current state after apply) |

In **client** mode, **GET** returns `listeners: []`; **POST** returns **400** (no listener slots).

## See also

- [`run-profile.md`](run-profile.md) — JSON aliases and flat server/client configs  
- [`sipstress-style-load-testing.md`](sipstress-style-load-testing.md) — hybrid JSON, live WebSocket, dynamic clients, internal auth  
- [`rtp-in-scenarios.md`](rtp-in-scenarios.md), [`srtp.md`](srtp.md) — scenario media and SRTP/DTLS-SRTP  
- [`gossipper-vs-sipp.md`](gossipper-vs-sipp.md) — product comparison and CLI philosophy  
- [`interactive-shell.md`](interactive-shell.md), [`tui.md`](tui.md) — interactive entry points  
