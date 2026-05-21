# 🤫 Gossipper

[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)

`Gossipper` is a Go rewrite of SIPp focused on SIP signaling load generation,
incremental XML scenario compatibility, and a cleaner engine architecture.

## Current scope

The current MVP implements:

- XML scenarios with `send`, `recv`, `pause`, `nop`, `label`, `timewait`, and `init`
- `<pause>` command with `distribution` attribute for `fixed`, `uniform`, and `normal` distributions
- XML command handoff via `sendCmd` / `recvCmd`, both inside one `Gossipper` process and across multiple instances
- SIPp-style 3PCC CLI aliases `-master`, `-slave`, and `-slave_cfg` on top of the external command transport
- Out-of-call SIP scenarios such as stateless `OPTIONS` ping/pong
- Command-only 3PCC/out-of-call flows can run without a SIP remote address
- Expanded XML actions: `ereg`, `assign`, `assignstr`, `todouble`, `add`, `subtract`, `multiply`, `divide`, `strcmp`, `test`, `log`, `warning`, `lookup`, `jump`, `gettimeofday`, `urlencode`, `urldecode`, `verifyauth`, `setdest`, and `exec`
- Basic SIPp-style keywords such as `[call_id]`, `[cseq]`, `[branch]`,
  `[remote_ip]`, `[local_ip]`, `[server_ip]`, `[len]`, `[last_*]`, `[last_Request_URI]`, `[users]`, `[userid]`, `[$var]`, `[file ...]`, `[fieldN ...]`, and Digest `[authentication]`
- UDP transports `u1`, `un`, and `ui` (pragmatic client+server multi-IP mode)
- Server-side UDP aliases `s1` and `sn` for UAS-style scenarios (they map to `u1` / `un`; they do **not** enable TLS)
- Server-side TLS alias `sl` for UAS (maps to `l1`; requires `-tls_cert` and `-tls_key`)
- Client-side TLS aliases `cl` and `cln` for UAC (map to `l1` and `ln`)
- TCP transports `t1` and `tn`
- TLS transports `l1` and `ln` (UAC or UAS; same codes as for TCP, plus TLS files as needed)
- Global and per-user variable scopes
- SIPp-style auth credentials via `-au` / `-ap` for challenged `401` / `407` request retries, inline `[authentication username=... password=...]`, and server-side `verifyauth`
- Optional SIP identity for built-in UAC / `invite_media`: `-sip_from` (From before `;tag=`), `-sip_pai`, `-sip_provider`, repeatable `-sip_extra_header` (see `docs/compatibility.md` `[trunk_*]` keywords)
- Concurrent call generation with rate limiting
- Interactive UI via **`gossipper tui`** (full-screen launcher) or **`gossipper cli`** (line-oriented shell: `set`, `wizard`, `run`, …)
- **`gossipper sipp`** — optional **SIPp-style CLI** prefix for scenario flags only (not `tui` / `cli` / `server`); same engine as root **`gossipper [flags…]`**; bare **`gossipper sipp`** prints a short entry summary; **`gossipper sipp -h`** lists the **full** SIPp-compatible scenario flags (grouped HEP / OTLP / PPROF / SIPP) and **without** **`-api_addr`** / **`-api_token`**, or **`-rtp_send`** / **`-rtp_*`**; **`gossipper server -h`** lists **management-oriented** groups (bind, API, TLS, HEP, …); **`gossipper -h`** lists **subcommands** and pointers to **`sipp -h`**, **`server -h`**, **`profile -h`** — not the long flag list; see `docs/gossipper-vs-sipp.md`
- **`gossipper server`** — management / long-run SIP UAS + HTTP API: use **`gossipper server -config …`** in systemd for flat JSON, or **`gossipper server`** with CLI flags (see `docs/cli.md` and `internal/cli/server_subcmd.go`)
- Basic statistics and JSON summary export (`-summary_json`), optional standalone **HTML** report (`-summary_html` or `gossipper report-html -in … -out …`; see `docs/summary-json.md`)
- Named per-step RTD timers via `start_rtd` / `rtd`, aggregated into summary JSON
- XML `counter` / `display` attributes aggregated into summary JSON as execution counts
- Exported stats now include failure-class counters such as `timeout`, `unexpected_sip`, `transport_error`, `parse_error`, `scenario_error`, and `cancelled`
- Summary JSON now includes latency repartition data and standard deviation for call length, invite RTT, and named RTD timers
- Full message tracing via `-trace_msg` / `-message_file`, compact CSV tracing via `-trace_shortmsg`, per-scenario SIP command counters via `-trace_counts`, periodic CSV stats snapshots with cumulative and interval delta fields via `-trace_stat` + `-fd`, RTD CSV dumps via `-trace_rtt` + `-rtt_freq`, non-interactive runtime screen snapshots via `-trace_screen` / `-screen_file`, error tracing via `-trace_err`, compact unexpected-response code tracing via `-trace_error_codes`, and action log tracing via `-trace_logs`
- SIP mirroring to Homer over HEP3 via `-hep_addr`, `-hep_capture_id`, and optional `-hep_password`
- Summary output now includes aggregated RTP/RTCP counters
- RTP streaming over `pion/rtp`, including `exec rtp_stream` with SIPp-style params and `start` / `pause` / `resume` / `stop`
- Audio PCAP replay via `exec play_pcap_audio="capture.pcap"` with preserved inter-packet timing and SDP-driven remote endpoint discovery
- RFC 2833 DTMF generation via `exec send_dtmf="123"` (digits: 0-9, *, #, A-D) and `[dtmf_digits]` keyword for variable-driven strings
- Pragmatic video/image PCAP replay via `exec play_pcap_video="capture.pcap"` and `exec play_pcap_image="capture.pcap"` using SDP media endpoint discovery (`m=video` / `m=image`)
- Pragmatic RTP activity checks via `exec rtpcheck="..."` with configurable `min_packets`, `timeout_ms`, and `direction=any|send|recv|both` (legacy `bidirectional` alias is also supported)
- RTP echo helper mode via `exec rtp_stream="echo"`
- Periodic RTCP sender reports plus basic incoming RTCP counters via `pion/rtcp`
- Optional HTTP API (`-api_addr`) and Control UI at **`/`** when the web UI is built (`make frontend`; embedded in the binary). Packages also ship static assets under **`/usr/local/gossipper/dist/`** (optional nginx `root`). JSON routes live under **`/api/v1/`** (health, stats, scenario get/put/apply, control, transports, clients, **`live`** WebSocket, and auth when **auth.type** is **internal** — SQLite + JWT; create users with **`gossipper auth user-add -config /path/to.json`**). Without internal auth, **`api_token`** in JSON or **`-api_token`** on the CLI still gates the API. **Packaged sample configs** live under **`/usr/local/gossipper/etc/`** (see table below); matching **systemd** units are **`gossipper-server`**, **`gossipper-client`**, and **`gossipper-hybrid`** — all use **`gossipper server -config …`** with the corresponding JSON.

## Project layout

- `cmd/gossip`: CLI entry point
- `hepcodec`: public HEP3 wire encode/decode (SIP + media payloads)
- `mediasink`: public `MediaExporter` API; built-in raw RTCP and Homer-Lake paths; optional compile-time extension for short JSON reports
- `internal/cli`: argument parsing
- `internal/scenario`: XML parser and built-in scenarios
- `internal/template`: SIPp keyword rendering
- `internal/sip`: SIP message parsing helpers
- `internal/transport`: UDP, TCP, and TLS transports
- `internal/api`: optional HTTP management API (`-api_addr`): stats, scenario, apply, control, transports, dynamic clients, WebSocket **`/api/v1/live`**, optional internal auth (**`/api/v1/auth/status`**, **`/api/v1/auth/login`**) when `auth.type` is `internal` in flat server JSON; optional embedded Control UI (`go:embed` webdist)
- `web/control-ui`: optional Vite/React control panel for that HTTP API (see `web/control-ui/README.md`); production build is written to `internal/api/webdist/` and embedded into the binary (`go:embed`). A tiny `web/control-ui/go.mod` exists so `go test ./...` does not scan `node_modules`.
- `internal/scheduler`: timing abstraction
- `internal/stats`: counters and summaries
- `internal/media`: RTP helpers backed by Pion
- `docs`: [`cli.md`](docs/cli.md) (process CLI), compatibility matrix, media roadmap, testing strategy

## Documentation

**Published docs (GitHub Pages):** [https://sipcapture.github.io/gossipper/](https://sipcapture.github.io/gossipper/) — built from `docs/` via MkDocs on push to `main`. Enable **Settings → Pages → Build and deployment: GitHub Actions** once after the first workflow run.

```bash
python3 -m venv .venv-docs && .venv-docs/bin/pip install -r docs-requirements.txt
.venv-docs/bin/mkdocs serve   # http://127.0.0.1:8000
```

- `docs/cli.md`: **subcommands** (`sipp`, `cli`, `tui`, `server`, …), run-profile path vs helpers
- `docs/gossipper-vs-sipp.md`: high-level overview of what `Gossipper` can do and how it compares to SIPp
- `docs/compatibility.md`: XML, keyword, action, transport, and scenario-time CLI compatibility matrix (including [TLS socket modes](docs/compatibility.md#tls-socket-modes-sipp-style) vs SIPp / [issue #7](https://github.com/sipcapture/gossipper/issues/7)); process-level CLI in `docs/cli.md`
- `docs/architecture.md`: package-level architecture and execution model
- `docs/media-roadmap.md`: media-related scope, next steps, and deferred items
- `docs/compatibility-testing.md`: testing approach for compatibility work and regression coverage
- `docs/statistics-mapping.md`: mapping between current `Gossipper` stats exports and SIPp-style counters
- `docs/trace-schema-contract.md`: stable CSV header/order contract for `-trace_stat`, `-trace_rtt`, and `-trace_screen`
- `docs/tui.md`: interactive TUI usage guide with launcher and runtime screen examples
- `docs/interactive-shell.md`: line CLI (`gossipper cli`) with `set`, `wizard`, `hint`, and `run`
- `docs/run-profile.md`: JSON run profiles (`-config`, `-run-alias`, `-list-aliases`), flat JSON via **`gossipper server -config`**; bundled `testdata/run-profiles/*.json` including HEP script presets
- `docs/sipstress-style-load-testing.md`: long-run SIP load, HTTP control, hybrid JSON, live WebSocket, dynamic clients, internal auth (stress-style operations guide)
- `docs/licensing.md`: license choice and SPDX header guidance for future source files
- `docs/rtp-in-scenarios.md`: `exec rtp_stream`, PCAP replay, **SRTP / DTLS-SRTP** (`-media_srtp`), RTCP, `rtpcheck`
- `docs/srtp.md`: SRTP (SDES + DTLS-SRTP), ICE, TURN, BUNDLE, trickle JSON — full media security contract
- `docs/pcap2scenario.md`: PCAP → paired UAC/UAS XML scenarios
- `docs/cli-gap-list.md`: SIPp CLI parity tracking and priorities

## Quick start

Production-style binary (includes embedded Control UI for `-api_addr`, like Homer’s `make all`):

```bash
make build
# binary: ./dist/gossipper
```

Quick local compile **without** rebuilding the web UI (API only, no `GET /` panel until you run `make frontend`):

```bash
go build -o gossipper ./cmd/gossip
```

Run the built-in UAC scenario against a SIP endpoint:

```bash
./gossipper -sn uac -rsa 127.0.0.1:5060 -m 1 -r 1
```

Same flags with an explicit **`sipp`** prefix (recommended in scripts and in the examples below; **`gossipper sipp -h`** uses a SIPp-oriented help preamble and omits **`-api_*`**, and **`-rtp_send`** / **`-rtp_*`**; **`gossipper server -h`** shows management-oriented sections, not the full SIPp dump):

```bash
./gossipper sipp -sn uac -rsa 127.0.0.1:5060 -m 1 -r 1
```

Run SIPp-style rate period scheduling (`-r n -rp m`):

```bash
./gossipper sipp -sn uac -rsa 127.0.0.1:5060 -r 7 -rp 2000 -m 50
```

Run with periodic CPS ramping:

```bash
./gossipper sipp -sn uac -rsa 127.0.0.1:5060 -r 10 -rate_increase 2 -rate_interval 1000 -rate_max 50 -m 1000
```

Run with a deterministic global timeout (CI-friendly):

```bash
./gossipper sipp -sn uac -rsa 127.0.0.1:5060 -m 10000 -r 50 -timeout_global 30
```

Benchmark gossipper vs SIPp (requires UAS or `-start-uas`):

```bash
./scripts/benchmark-sipp-vs-gossipper.sh -start-uas -calls 500 -rate 50
make benchmark   # same, via Makefile
```

Generate CSV lookup index for faster `lookup` action resolution:

```bash
./gossipper sipp -infindex ./testdata/injection/inject.csv 0
```

Run M3 `ui` transport MVP (source IP selected per call from CSV):

```bash
./gossipper sipp -sn uac -rsa 127.0.0.1:5060 -t ui -inf ./testdata/injection/ui_ips.csv -ip_field 0 -m 20 -r 10
```

Print build version information:

```bash
./gossipper -version
```

### Profiling

Enable pprof HTTP server for live CPU/memory/goroutine profiling:

```bash
./gossipper sipp -sn uac -rsa 127.0.0.1:5060 -m 1000 -r 50 -pprof :6060
# In another terminal: go tool pprof -http=:8080 http://localhost:6060/debug/pprof/profile?seconds=30
```

Write CPU and memory profiles to files at exit:

```bash
./gossipper sipp -sn uac -rsa 127.0.0.1:5060 -m 500 -r 20 -cpuprofile cpu.prof -memprofile mem.prof
go tool pprof -http=:8080 cpu.prof   # or mem.prof for heap
```

Launch the full-screen TUI or the line CLI:

```bash
./gossipper tui
./gossipper cli
```

Run a custom XML scenario:

```bash
./gossipper sipp -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -m 10 -r 5
```

Write a JSON summary:

```bash
./gossipper sipp -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -summary_json summary.json
```

Write full and short message traces:

```bash
./gossipper sipp -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -trace_msg -trace_shortmsg -message_file ./messages.log
```

Write unexpected responses and action logs to dedicated files:

```bash
./gossipper sipp -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -trace_err -trace_error_codes -error_file ./errors.log -trace_logs -log_file ./actions.log
```

Write periodic CSV stats snapshots:

```bash
./gossipper sipp -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -trace_stat -fd 2 -message_file ./messages.log
```

Write periodic per-command SIP message counters:

```bash
./gossipper sipp -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -trace_counts -fd 2 -message_file ./messages.log
```

Write periodic non-interactive runtime screen snapshots:

```bash
./gossipper sipp -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -trace_screen -fd 2 -screen_file ./screen.log
```

Interactive controls during a TUI run:

- `+` / `-`: increase or decrease target CPS
- `*` / `/`: increase or decrease target CPS by 10x step (`-rate_scale` based)
- `p`: pause or resume new call scheduling
- `q`: stop the run like SIPp by draining active calls in client mode
- `Esc`: return to the launch screen after the run finishes

Write RTD CSV samples:

```bash
./gossipper sipp -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -trace_rtt -rtt_freq 50 -message_file ./messages.log
```

Mirror SIP messages to Homer over HEP3:

```bash
./gossipper sipp -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -hep_addr 127.0.0.1:9060 -hep_capture_id 2001 -hep_password secret
```

### Structured event logging (OTel)

Gossipper ships with a non-blocking structured event logger that fans the SIP
dialog (and other engine events) out to one or more sinks: stdout, JSON-Lines
file, and OpenTelemetry collectors over OTLP/gRPC or OTLP/HTTP. All sinks are
fed by a single ring buffer, so emitting events never blocks the SIP hot path —
on overflow the oldest events are dropped and counted.

Resource attributes are attached at the OTel `LoggerProvider` level. By default
Gossipper injects `service.name=gossipper`, `service.version=<build>`, and
`gossipper.role=client|server`. Any extra `-log_attr key=value` pair (repeatable)
is also added — typical use is `self_tag` / `peer_tag` to label nodes such as
`NYC01` / `NYC02`.

Client `NYC02` shipping logs to a collector via OTLP/gRPC:

```bash
./gossipper sipp -sn uac -rsa 10.0.0.3:5060 \
  -log_otel_endpoint otel-collector:4317 -log_otel_proto grpc -log_otel_insecure \
  -log_attr self_tag=NYC02 -log_attr peer_tag=NYC01 -log_stdout
```

Server `NYC01` shipping logs over OTLP/HTTP (typical when the collector sits
behind a TLS-terminating proxy):

```bash
./gossipper sipp -sn uas -t s1 -i 0.0.0.0 -p 5060 \
  -log_otel_endpoint http://otel-collector:4318 -log_otel_proto http \
  -log_attr self_tag=NYC01 -log_attr peer_tag=NYC02 \
  -log_file_jsonl /tmp/gossipper-events.jsonl
```

The full set of flags is described in `docs/event-logging.md`, including event
schema, the list of `Kind` values, ring-buffer sizing guidance, and tips for
high-CPS deployments. The legacy `-trace_*` files keep working in parallel and
are unaffected by event logging.

Run over TLS (UAC; `-t cl` is the same as `-t l1` after startup):

```bash
./gossipper sipp -t cl -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5061 -tls_skip_verify
```

Same with explicit `l1`:

```bash
./gossipper sipp -t l1 -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5061 -tls_skip_verify
```

**SIP TLS (answers to common support questions):** signaling over TLS **is supported**. For **UAC** use `-t l1` or `-t ln`, or the aliases **`-t cl` / `-t cln`** (same as `l1` / `ln`). For a **UAS**, `-t s1` / `-t sn` are **UDP-only** aliases; use `-t l1` / `-t ln` or the TLS server shortcut `-t sl` (same as `l1`). A TLS listener needs `-tls_cert` and `-tls_key`; clients often use `-tls_ca` and `-tls_skip_verify=false` when validating the peer. The default `-tls_skip_verify=true` is convenient for lab setups only.

Run the built-in **UAS over TLS** (replace certificate paths with real files):

```bash
./gossipper sipp -sn uas -t sl -i 0.0.0.0 -p 5061 \
  -tls_cert ./server.crt -tls_key ./server.key -m 1
```

Run the built-in UAS in SIPp-style server transport mode:

```bash
./gossipper sipp -sn uas -t s1 -i 0.0.0.0 -p 5060 -m 1
```

Run out-of-call `OPTIONS` ping/pong between two `Gossipper` instances:

```bash
./gossipper sipp -sf ./testdata/scenarios/options_server.xml -t s1 -i 0.0.0.0 -p 5060 -m 1
./gossipper sipp -sf ./testdata/scenarios/options_client.xml -rsa 127.0.0.1:5060 -s options -m 1 -r 1
```

Run a challenged Digest auth scenario with SIPp-style credentials:

```bash
./gossipper sipp -sf ./testdata/scenarios/auth_uac.xml -rsa 127.0.0.1:5060 -s alice -au alice -ap secret -m 1 -r 1
```

Run with an explicit base CSeq for `[cseq]` tokens:

```bash
./gossipper sipp -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -base_cseq 42 -m 1 -r 1
```

Run a SIPp-style audio PCAP replay action from a scenario:

```bash
./gossipper sipp -sf ./testdata/scenarios/uac_pcap.xml -rsa 127.0.0.1:5060 -m 1 -r 1
```

Run the bundled local two-sided PCAP demo between two `Gossipper` instances:

```bash
./gossipper sipp -sf ./testdata/scenarios/uas_pcap.xml -t s1 -i 0.0.0.0 -p 5060 -s pcap -m 1
./gossipper sipp -sf ./testdata/scenarios/uac_pcap.xml -rsa 127.0.0.1:5060 -s pcap -m 1 -r 1
```

Run the bundled DTMF-over-PCAP demo against the same local UAS:

```bash
./gossipper sipp -sf ./testdata/scenarios/uas_pcap.xml -t s1 -i 0.0.0.0 -p 5060 -s pcap -m 1
./gossipper sipp -sf ./testdata/scenarios/uac_dtmf_pcap.xml -rsa 127.0.0.1:5060 -s pcap -m 1 -r 1
```

Run 3PCC-style command exchange between instances with the low-level transport flags:

```bash
./gossipper sipp -sf ./scenario.xml -rsa 127.0.0.1:5060 -cmd_name m -cmd_peers ./peers.cfg
```

Run the same flow with SIPp-style master/slave aliases:

```bash
./gossipper sipp -sf ./testdata/scenarios/3pcc_slave.xml -slave s1 -slave_cfg ./peers.cfg
./gossipper sipp -sf ./testdata/scenarios/3pcc_master.xml -master m -slave_cfg ./peers.cfg -rsa 127.0.0.1:5060
```

## Packaging

Build the release binary:

```bash
make build
```

Build Linux packages with `nfpm`:

```bash
make package          # deb + rpm in one shot (recommended)
make package-deb
make package-rpm
```

Or call the packaging script directly (same layout as Homer `scripts/build_package.sh`):

```bash
VERSION=0.1.0 ARCH=amd64 OS=linux scripts/build_package.sh deb
VERSION=0.1.0 ARCH=amd64 OS=linux scripts/build_package.sh rpm
# or both in one run (one frontend + one compile):
VERSION=0.1.0 ARCH=amd64 OS=linux scripts/build_package.sh all
```

The script mirrors CI: checks Node for the Control UI, runs `npm ci` / `npm run build` in `web/control-ui`, compiles a static `gossipper` into `dist/`, downloads `nfpm` v2.41.1 into the repo root if missing, then runs `nfpm package`.

By default, package versions are taken from `cmd/gossip/version.go`. You can
override them by exporting `VERSION=...` for ad-hoc builds.

Package artifacts are written to `dist/` (build tree). The installed layout is under **`/usr/local/gossipper`**:

| Path | Contents |
|------|----------|
| `bin/gossipper` | main binary |
| `etc/gossipper-server.json` | management SIP UAS + HTTP API — from repo **`examples/gossipper-management.json`** (`gossipper server -config`); typical SIP bind **`0.0.0.0:5060`**, **`api_addr`** **`:8080`** unless overridden |
| `etc/gossipper-client.json` | UAC / load preset — from repo **`examples/gossipper-uac.json`** (`gossipper server -config`; edit **`remote_addr`**, rates, scenario) |
| `etc/gossipper-hybrid.json` | one process: management **`server`** + **`clients[]`** — from repo **`examples/gossipper-hybrid.json`** |
| `etc/gossipper-management-auth.sample.json` | template for **`auth.type`**: **`internal`** (SQLite users + JWT); copy or merge into your server JSON, set **`jwt_secret`**, then **`gossipper auth user-add -config …`**. Default **`sqlite_path`** in the sample is **`/usr/local/gossipper/data/gossipper-settings.sqlite`**. |
| `etc/doc/` | `README.md`, `LICENSE`, and `docs/` from this repository |
| `data/` | empty directory for persistent local state (e.g. internal-auth SQLite when **`auth.sqlite_path`** points here) |
| `logs/` | empty directory for runtime logs (operator-owned) |
| `dist/` | Control UI static files (same as embedded webdist) |

A symlink **`/usr/local/bin/gossipper`** → `/usr/local/gossipper/bin/gossipper` is created for `PATH`.

**Repo `examples/`** mirrors these JSON files and the **`*.service`** units (which reference the **`/usr/local/gossipper/etc/*.json`** paths above).

**`.deb` / `.rpm`** install **`/lib/systemd/system/gossipper-server.service`**, **`gossipper-client.service`**, and **`gossipper-hybrid.service`** (see **`examples/*.service`**) and run **`systemctl daemon-reload`** via **`scripts/nfpm-postinstall.sh`**. Enable the unit that matches your profile, e.g. **`sudo systemctl enable --now gossipper-server`**, **`gossipper-client`**, or **`gossipper-hybrid`** (avoid two units binding the same SIP ports on one host).

## Notes

- This is a behavior-oriented rewrite, not a literal source port of SIPp.
- For a narrative overview of current capabilities and trade-offs versus SIPp, see `docs/gossipper-vs-sipp.md`.
- XML compatibility is intentionally incremental. See `docs/compatibility.md`.
- For stats/export mapping against SIPp terminology, see `docs/statistics-mapping.md`.
- External `sendCmd` / `recvCmd` uses a simple TCP peer map in `name;host:port` format via either `-cmd_name` / `-cmd_peers` or the SIPp-like aliases `-master` / `-slave` / `-slave_cfg`.
- If a scenario only uses `sendCmd` / `recvCmd` plus control commands, it can run without `-rsa`.
- Regular out-of-call SIP request/response scenarios are supported too, for example `OPTIONS` healthcheck flows without dialog teardown.
- `-t s1` and `-t sn` are server-side UDP aliases and require a server scenario such as `-sn uas`.
- `-message_file` enables full message trace output to a file; `-trace_shortmsg` writes a sibling compact CSV log with per-message summaries.
- `-trace_err` writes unexpected SIP responses and runtime failures to `-error_file`.
- `-trace_error_codes` writes a sibling compact CSV file with unexpected SIP response codes and expected match criteria.
- `-trace_logs` writes XML `<log>` action output to `-log_file`.
- `-trace_stat` writes periodic and final CSV stats snapshots to a sibling `*_stats.log` file; when `-message_file` is set it uses that base path. The CSV includes cumulative totals, per-interval delta fields, and latency standard deviation columns. `-fd` controls the snapshot period in seconds.
- `-trace_counts` writes periodic CSV snapshots to a sibling `*_counts.log` file with per-scenario SIP command counters (`sent`, `recv`, `unexp`) using the same `-fd` cadence.
- `-trace_screen` writes periodic and final runtime summary snapshots to a sibling `*_screen.log` CSV file; fields include totals, CPS, interval CPS, success ratio, and key failure counters (`failure_timeout`, `failure_unexpected_sip`). `-screen_file` sets the explicit output path, and `-fd` controls snapshot cadence.
- send `SIGUSR1` to a running process to force an immediate `-trace_screen` snapshot dump without waiting for the next `-fd` tick.
- `-timeout_global` stops the run after N seconds of total process runtime while still emitting final summaries/artifacts.
- `-rate_scale` sets the interactive target CPS step used by TUI keys (`+/-` for 1x and `*`/`/` for 10x).
- Exported stats also include failure-class counters so automation can distinguish timeouts, unexpected SIP, transport errors, parse errors, scenario errors, and cancellations.
- `-trace_rtt` writes named RTD samples to a sibling `*_rtt.log` CSV file with timestamp, call, RTD name, and duration in milliseconds. `-rtt_freq` controls flush cadence in completed calls (default `200`).
- Stable CSV header contracts for `-trace_stat`, `-trace_rtt`, and `-trace_screen` are documented in `docs/trace-schema-contract.md`.
- Summary JSON now exports repartition buckets for `call_length`, `invite_rtt`, and named `rtd` timers so automation can consume latency distributions without parsing raw RTT dumps.
- `-hep_addr` mirrors SIP `send` / `recv` traffic to a Homer-compatible HEP3 collector over UDP.
- `-hep_capture_id` sets the HEP capture node ID; `-hep_password` sets the optional HEP auth key.
- The current HEP MVP exports SIP signaling only; RTP/RTCP mirroring is not included yet.
- `[authentication]` currently covers Digest `401` / `407` challenge responses with `MD5` and `qop=auth`; the scenario must explicitly place `[authentication]` into the retried request.
- `-base_cseq` sets the seed value used by `[cseq]` token rendering.
- `-rp` sets the SIPp-compatible rate period in milliseconds for `-r` (`n` calls every `rp` ms).
- `-rate_increase` adjusts target CPS every `-rate_interval` milliseconds during run; `-rate_max` sets an optional upper cap.
- `-max_socket` limits simultaneously open call sockets for per-call client transports (`un`, `tn`, `ln`, alias `cln`).
- `-max_reconnect` and `-reconnect_sleep` enable reconnect retries for shared client TCP/TLS transports (`t1`, `l1`, or the `cl` / `sl` aliases that normalize to `l1`) on transport failures.
- `-reconnect_close` in shared client `t1`/`l1` (or `cl` / `sl`) closes active calls on socket loss by skipping reconnect attempts.
- `-infindex <file> <field>` generates an index file next to the CSV (`.gossipper.idx.<field>.json`) so lookup-by-key can avoid full-file scans.
- `-t ui` currently provides an M3 client+server MVP: one shared UDP socket per configured IP (client: per-call source IP rotation, server: one listener per configured IP).
- `-inf <file>` is required with `-t ui`. Optional `-ip_field <idx>` selects bind/source IPs from that CSV column (zero-based); if omitted, bind uses **`-i`** for all UI sockets (same idea as `u1`/`un` with `-inf` for `[fieldN]` only).
- In `-t ui` client mode, `-inf` row order is preserved and duplicate IP rows intentionally affect round-robin weighting; in server mode listeners are created per unique IP.
- XML action `setdest` is supported in the pragmatic M3 scope for UDP shared-socket flows (`u1`, `ui`, and server-side UDP aliases) and enforces protocol compatibility checks.
- TUI launch form supports `-t ui` with `inf` and optional `ip_field` (empty uses Local IP).
- `start_rtd` and `rtd` now record named per-step timings into the summary model; they are especially useful for XML flows like `send INVITE` -> `recv 200`.
- `counter` and `display` are currently exposed as successful-command execution counters in the summary model, which is a practical first step toward richer SIPp-style reporting.
- In external 3PCC-style flows, the first incoming `recvCmd` can automatically adopt its `Call-ID` into `[call_id]` for later commands.
- `init` can also use `sendCmd` / `recvCmd`, so inter-instance setup data may be loaded into global scopes before SIP traffic starts.
- When launched with `-slave`, `Gossipper` validates that the scenario first enters the flow via `recvCmd` before it sends any `sendCmd`.
- RTP support is a separate milestone layered on top of the SIP engine.
- Current RTP support focuses on audio streaming from PCM mono 8kHz WAV input, audio PCAP replay, RTP echo, and basic RTCP observability.
- `play_pcap_audio` currently replays UDP payloads from the capture as RTP toward the negotiated audio endpoint; pragmatic `play_pcap_video` / `play_pcap_image` support reuses the same replay mechanism for SDP `m=video` / `m=image` endpoints.
- `rtpcheck` currently provides pragmatic RTP activity validation (`min_packets`, `timeout_ms`, `direction`; legacy `bidirectional` alias) and is not full SIPp `rtpcheck` quality parity. **SRTP / DTLS-SRTP** for scenario media uses **`-media_srtp`** (see `docs/srtp.md`, `docs/rtp-in-scenarios.md`).

## Releasing

When a new tag of the form `vX.Y.Z` is pushed, the `.github/workflows/version-sync.yml`
workflow automatically updates the `Version` constant in `cmd/gossip/version.go` on the
`main` branch to match the tag (stripping the leading `v`).  The release build workflow
(`.github/workflows/release.yml`) then picks up the new tag and publishes the release
assets.

To cut a release:

```bash
git tag v1.2.3
git push origin v1.2.3
```

The version sync workflow commits the updated `version.go` back to `main` automatically.

## License

AGPL-3.0. See `LICENSE`.
