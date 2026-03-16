# 🤫 gossIpper

[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)

`gossIpper` is a Go rewrite of SIPp focused on SIP signaling load generation,
incremental XML scenario compatibility, and a cleaner engine architecture.

## Current scope

The current MVP implements:

- XML scenarios with `send`, `recv`, `pause`, `nop`, `label`, `timewait`, and `init`
- XML command handoff via `sendCmd` / `recvCmd`, both inside one `gossIpper` process and across multiple instances
- SIPp-style 3PCC CLI aliases `-master`, `-slave`, and `-slave_cfg` on top of the external command transport
- Out-of-call SIP scenarios such as stateless `OPTIONS` ping/pong
- Command-only 3PCC/out-of-call flows can run without a SIP remote address
- Expanded XML actions: `ereg`, `assign`, `assignstr`, `todouble`, `add`, `subtract`, `multiply`, `divide`, `strcmp`, `test`, `log`, `warning`, `lookup`, `jump`, `gettimeofday`, `urlencode`, `urldecode`, `verifyauth`, `setdest`, and `exec`
- Basic SIPp-style keywords such as `[call_id]`, `[cseq]`, `[branch]`,
  `[remote_ip]`, `[local_ip]`, `[server_ip]`, `[len]`, `[last_*]`, `[last_Request_URI]`, `[users]`, `[userid]`, `[$var]`, `[file ...]`, `[fieldN ...]`, and Digest `[authentication]`
- UDP transports `u1`, `un`, and `ui` (pragmatic client+server multi-IP mode)
- Server-side UDP aliases `s1` and `sn` for UAS-style scenarios
- TCP transports `t1` and `tn`
- TLS transports `l1` and `ln`
- Global and per-user variable scopes
- SIPp-style auth credentials via `-au` / `-ap` for challenged `401` / `407` request retries, inline `[authentication username=... password=...]`, and server-side `verifyauth`
- Concurrent call generation with rate limiting
- Interactive terminal UI via `gossIpper tui` / `gossIpper -interactive` for launch presets and live runtime control
- Basic statistics and JSON summary export
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

## Project layout

- `cmd/gossip`: CLI entry point
- `internal/cli`: argument parsing
- `internal/scenario`: XML parser and built-in scenarios
- `internal/template`: SIPp keyword rendering
- `internal/sip`: SIP message parsing helpers
- `internal/transport`: UDP, TCP, and TLS transports
- `internal/engine`: scenario execution engine
- `internal/scheduler`: timing abstraction
- `internal/stats`: counters and summaries
- `internal/media`: RTP helpers backed by Pion
- `docs`: compatibility matrix, media roadmap, testing strategy

## Documentation

- `docs/gossipper-vs-sipp.md`: high-level overview of what `gossIpper` can do and how it compares to SIPp
- `docs/compatibility.md`: current XML, keyword, action, transport, and CLI compatibility matrix
- `docs/architecture.md`: package-level architecture and execution model
- `docs/media-roadmap.md`: media-related scope, next steps, and deferred items
- `docs/compatibility-testing.md`: testing approach for compatibility work and regression coverage
- `docs/statistics-mapping.md`: mapping between current `gossIpper` stats exports and SIPp-style counters
- `docs/trace-schema-contract.md`: stable CSV header/order contract for `-trace_stat`, `-trace_rtt`, and `-trace_screen`
- `docs/tui.md`: interactive TUI usage guide with launcher and runtime screen examples
- `docs/licensing.md`: license choice and SPDX header guidance for future source files
- `milestone.md`: prioritized roadmap for SIPp features that are still missing in `gossIpper`

## Quick start

Run the built-in UAC scenario against a SIP endpoint:

```bash
go run ./cmd/gossip -sn uac -rsa 127.0.0.1:5060 -m 1 -r 1
```

Run SIPp-style rate period scheduling (`-r n -rp m`):

```bash
go run ./cmd/gossip -sn uac -rsa 127.0.0.1:5060 -r 7 -rp 2000 -m 50
```

Run with periodic CPS ramping:

```bash
go run ./cmd/gossip -sn uac -rsa 127.0.0.1:5060 -r 10 -rate_increase 2 -rate_interval 1000 -rate_max 50 -m 1000
```

Run with a deterministic global timeout (CI-friendly):

```bash
go run ./cmd/gossip -sn uac -rsa 127.0.0.1:5060 -m 10000 -r 50 -timeout_global 30
```

Benchmark gossipper vs SIPp (requires UAS or `-start-uas`):

```bash
./scripts/benchmark-sipp-vs-gossipper.sh -start-uas -calls 500 -rate 50
make benchmark   # same, via Makefile
```

Generate CSV lookup index for faster `lookup` action resolution:

```bash
go run ./cmd/gossip -infindex ./testdata/injection/inject.csv 0
```

Run M3 `ui` transport MVP (source IP selected per call from CSV):

```bash
go run ./cmd/gossip -sn uac -rsa 127.0.0.1:5060 -t ui -inf ./testdata/injection/ui_ips.csv -ip_field 0 -m 20 -r 10
```

Print build version information:

```bash
go run ./cmd/gossip -version
```

### Profiling

Enable pprof HTTP server for live CPU/memory/goroutine profiling:

```bash
go run ./cmd/gossip -sn uac -rsa 127.0.0.1:5060 -m 1000 -r 50 -pprof :6060
# In another terminal: go tool pprof -http=:8080 http://localhost:6060/debug/pprof/profile?seconds=30
```

Write CPU and memory profiles to files at exit:

```bash
go run ./cmd/gossip -sn uac -rsa 127.0.0.1:5060 -m 500 -r 20 -cpuprofile cpu.prof -memprofile mem.prof
go tool pprof -http=:8080 cpu.prof   # or mem.prof for heap
```

Launch the interactive TUI:

```bash
go run ./cmd/gossip tui
```

or:

```bash
go run ./cmd/gossip -interactive
```

Run a custom XML scenario:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -m 10 -r 5
```

Write a JSON summary:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -summary_json summary.json
```

Write full and short message traces:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -trace_msg -trace_shortmsg -message_file ./messages.log
```

Write unexpected responses and action logs to dedicated files:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -trace_err -trace_error_codes -error_file ./errors.log -trace_logs -log_file ./actions.log
```

Write periodic CSV stats snapshots:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -trace_stat -fd 2 -message_file ./messages.log
```

Write periodic per-command SIP message counters:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -trace_counts -fd 2 -message_file ./messages.log
```

Write periodic non-interactive runtime screen snapshots:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -trace_screen -fd 2 -screen_file ./screen.log
```

Interactive controls during a TUI run:

- `+` / `-`: increase or decrease target CPS
- `*` / `/`: increase or decrease target CPS by 10x step (`-rate_scale` based)
- `p`: pause or resume new call scheduling
- `q`: stop the run like SIPp by draining active calls in client mode
- `Esc`: return to the launch screen after the run finishes

Write RTD CSV samples:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -trace_rtt -rtt_freq 50 -message_file ./messages.log
```

Mirror SIP messages to Homer over HEP3:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -hep_addr 127.0.0.1:9060 -hep_capture_id 2001 -hep_password secret
```

Run over TLS:

```bash
go run ./cmd/gossip -t l1 -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5061 -tls_skip_verify
```

Run the built-in UAS in SIPp-style server transport mode:

```bash
go run ./cmd/gossip -sn uas -t s1 -i 0.0.0.0 -p 5060 -m 1
```

Run out-of-call `OPTIONS` ping/pong between two `gossIpper` instances:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/options_server.xml -t s1 -i 0.0.0.0 -p 5060 -m 1
go run ./cmd/gossip -sf ./testdata/scenarios/options_client.xml -rsa 127.0.0.1:5060 -s options -m 1 -r 1
```

Run a challenged Digest auth scenario with SIPp-style credentials:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/auth_uac.xml -rsa 127.0.0.1:5060 -s alice -au alice -ap secret -m 1 -r 1
```

Run with an explicit base CSeq for `[cseq]` tokens:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -base_cseq 42 -m 1 -r 1
```

Run a SIPp-style audio PCAP replay action from a scenario:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/uac_pcap.xml -rsa 127.0.0.1:5060 -m 1 -r 1
```

Run the bundled local two-sided PCAP demo between two `gossIpper` instances:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/uas_pcap.xml -t s1 -i 0.0.0.0 -p 5060 -s pcap -m 1
go run ./cmd/gossip -sf ./testdata/scenarios/uac_pcap.xml -rsa 127.0.0.1:5060 -s pcap -m 1 -r 1
```

Run the bundled DTMF-over-PCAP demo against the same local UAS:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/uas_pcap.xml -t s1 -i 0.0.0.0 -p 5060 -s pcap -m 1
go run ./cmd/gossip -sf ./testdata/scenarios/uac_dtmf_pcap.xml -rsa 127.0.0.1:5060 -s pcap -m 1 -r 1
```

Run 3PCC-style command exchange between instances with the low-level transport flags:

```bash
go run ./cmd/gossip -sf ./scenario.xml -rsa 127.0.0.1:5060 -cmd_name m -cmd_peers ./peers.cfg
```

Run the same flow with SIPp-style master/slave aliases:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/3pcc_slave.xml -slave s1 -slave_cfg ./peers.cfg
go run ./cmd/gossip -sf ./testdata/scenarios/3pcc_master.xml -master m -slave_cfg ./peers.cfg -rsa 127.0.0.1:5060
```

## Packaging

Build the release binary:

```bash
make build
```

Build Linux packages with `nfpm`:

```bash
make package-deb
make package-rpm
```

Or call the builder script directly:

```bash
VERSION=0.1.0 ARCH=amd64 scripts/build-package.sh deb
VERSION=0.1.0 ARCH=amd64 scripts/build-package.sh rpm
```

The builder uses local `nfpm` when available and falls back to the
`goreleaser/nfpm` Docker image otherwise.

By default, package versions are taken from `cmd/gossip/version.go`. You can
override them by exporting `VERSION=...` for ad-hoc builds.

Package artifacts are written to `dist/` and install the `gossIpper` binary
at `/usr/bin/gossIpper`.

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
- `-max_socket` limits simultaneously open call sockets for per-call client transports (`un`, `tn`, `ln`).
- `-max_reconnect` and `-reconnect_sleep` enable reconnect retries for shared client TCP/TLS transports (`t1`, `l1`) on transport failures.
- `-reconnect_close` in shared client `t1`/`l1` closes active calls on socket loss by skipping reconnect attempts.
- `-infindex <file> <field>` generates an index file next to the CSV (`.gossipper.idx.<field>.json`) so lookup-by-key can avoid full-file scans.
- `-t ui` currently provides an M3 client+server MVP: one shared UDP socket per configured IP (client: per-call source IP rotation, server: one listener per configured IP).
- `-inf <file>` + `-ip_field <idx>` are required with `-t ui` and select UI bind IPs from the CSV field (zero-based index).
- In `-t ui` client mode, `-inf` row order is preserved and duplicate IP rows intentionally affect round-robin weighting; in server mode listeners are created per unique IP.
- XML action `setdest` is supported in the pragmatic M3 scope for UDP shared-socket flows (`u1`, `ui`, and server-side UDP aliases) and enforces protocol compatibility checks.
- TUI launch form supports `-t ui` with explicit `inf` / `ip_field` inputs and validates them before run start.
- `start_rtd` and `rtd` now record named per-step timings into the summary model; they are especially useful for XML flows like `send INVITE` -> `recv 200`.
- `counter` and `display` are currently exposed as successful-command execution counters in the summary model, which is a practical first step toward richer SIPp-style reporting.
- In external 3PCC-style flows, the first incoming `recvCmd` can automatically adopt its `Call-ID` into `[call_id]` for later commands.
- `init` can also use `sendCmd` / `recvCmd`, so inter-instance setup data may be loaded into global scopes before SIP traffic starts.
- When launched with `-slave`, `gossIpper` validates that the scenario first enters the flow via `recvCmd` before it sends any `sendCmd`.
- RTP support is a separate milestone layered on top of the SIP engine.
- Current RTP support focuses on audio streaming from PCM mono 8kHz WAV input, audio PCAP replay, RTP echo, and basic RTCP observability.
- `play_pcap_audio` currently replays UDP payloads from the capture as RTP toward the negotiated audio endpoint; pragmatic `play_pcap_video` / `play_pcap_image` support reuses the same replay mechanism for SDP `m=video` / `m=image` endpoints.
- `rtpcheck` currently provides pragmatic RTP activity validation (`min_packets`, `timeout_ms`, `direction`; legacy `bidirectional` alias) and is not full SIPp SRTP/quality parity.

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
