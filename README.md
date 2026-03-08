# 🤫 gossipper

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

`gossipper` is a Go rewrite of SIPp focused on SIP signaling load generation,
incremental XML scenario compatibility, and a cleaner engine architecture.

## Current scope

The current MVP implements:

- XML scenarios with `send`, `recv`, `pause`, `nop`, `label`, `timewait`, and `init`
- XML command handoff via `sendCmd` / `recvCmd`, both inside one `gossipper` process and across multiple instances
- SIPp-style 3PCC CLI aliases `-master`, `-slave`, and `-slave_cfg` on top of the external command transport
- Out-of-call SIP scenarios such as stateless `OPTIONS` ping/pong
- Command-only 3PCC/out-of-call flows can run without a SIP remote address
- Basic XML actions: `ereg`, `assignstr`, `test`, `log`, and `exec`
- Basic SIPp-style keywords such as `[call_id]`, `[cseq]`, `[branch]`,
  `[remote_ip]`, `[local_ip]`, `[len]`, `[last_*]`, `[$var]`, `[file ...]`, `[fieldN ...]`, and Digest `[authentication]`
- UDP transports `u1` and `un`
- Server-side UDP aliases `s1` and `sn` for UAS-style scenarios
- TCP transports `t1` and `tn`
- TLS transports `l1` and `ln`
- Global and per-user variable scopes
- SIPp-style auth credentials via `-au` / `-ap` for challenged `401` / `407` request retries
- Concurrent call generation with rate limiting
- Basic statistics and JSON summary export
- Named per-step RTD timers via `start_rtd` / `rtd`, aggregated into summary JSON
- XML `counter` / `display` attributes aggregated into summary JSON as execution counts
- Exported stats now include failure-class counters such as `timeout`, `unexpected_sip`, `transport_error`, `parse_error`, `scenario_error`, and `cancelled`
- Summary JSON now includes latency repartition data and standard deviation for call length, invite RTT, and named RTD timers
- Full message tracing via `-trace_msg` / `-message_file`, compact CSV tracing via `-trace_shortmsg`, periodic CSV stats snapshots with cumulative and interval delta fields via `-trace_stat`, RTD CSV dumps via `-trace_rtt`, error tracing via `-trace_err`, compact unexpected-response code tracing via `-trace_error_codes`, and action log tracing via `-trace_logs`
- SIP mirroring to Homer over HEP3 via `-hep_addr`, `-hep_capture_id`, and optional `-hep_password`
- Summary output now includes aggregated RTP/RTCP counters
- RTP streaming over `pion/rtp`, including `exec rtp_stream` with SIPp-style params and `start` / `pause` / `resume` / `stop`
- Audio PCAP replay via `exec play_pcap_audio="capture.pcap"` with preserved inter-packet timing and SDP-driven remote endpoint discovery
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

- `docs/gossipper-vs-sipp.md`: high-level overview of what `gossipper` can do and how it compares to SIPp
- `docs/compatibility.md`: current XML, keyword, action, transport, and CLI compatibility matrix
- `docs/architecture.md`: package-level architecture and execution model
- `docs/media-roadmap.md`: media-related scope, next steps, and deferred items
- `docs/compatibility-testing.md`: testing approach for compatibility work and regression coverage
- `docs/licensing.md`: license choice and SPDX header guidance for future source files
- `milestone.md`: prioritized roadmap for SIPp features that are still missing in `gossipper`

## Quick start

Run the built-in UAC scenario against a SIP endpoint:

```bash
go run ./cmd/gossip -sn uac -rsa 127.0.0.1:5060 -m 1 -r 1
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
go run ./cmd/gossip -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -trace_stat -message_file ./messages.log
```

Write RTD CSV samples:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/basic_uac.xml -rsa 127.0.0.1:5060 -trace_rtt -message_file ./messages.log
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

Run out-of-call `OPTIONS` ping/pong between two `gossipper` instances:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/options_server.xml -t s1 -i 0.0.0.0 -p 5060 -m 1
go run ./cmd/gossip -sf ./testdata/scenarios/options_client.xml -rsa 127.0.0.1:5060 -s options -m 1 -r 1
```

Run a challenged Digest auth scenario with SIPp-style credentials:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/auth_uac.xml -rsa 127.0.0.1:5060 -s alice -au alice -ap secret -m 1 -r 1
```

Run a SIPp-style audio PCAP replay action from a scenario:

```bash
go run ./cmd/gossip -sf ./testdata/scenarios/uac_pcap.xml -rsa 127.0.0.1:5060 -m 1 -r 1
```

Run the bundled local two-sided PCAP demo between two `gossipper` instances:

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

Package artifacts are written to `dist/` and install the `gossipper` binary
at `/usr/bin/gossipper`.

## Notes

- This is a behavior-oriented rewrite, not a literal source port of SIPp.
- For a narrative overview of current capabilities and trade-offs versus SIPp, see `docs/gossipper-vs-sipp.md`.
- XML compatibility is intentionally incremental. See `docs/compatibility.md`.
- External `sendCmd` / `recvCmd` uses a simple TCP peer map in `name;host:port` format via either `-cmd_name` / `-cmd_peers` or the SIPp-like aliases `-master` / `-slave` / `-slave_cfg`.
- If a scenario only uses `sendCmd` / `recvCmd` plus control commands, it can run without `-rsa`.
- Regular out-of-call SIP request/response scenarios are supported too, for example `OPTIONS` healthcheck flows without dialog teardown.
- `-t s1` and `-t sn` are server-side UDP aliases and require a server scenario such as `-sn uas`.
- `-message_file` enables full message trace output to a file; `-trace_shortmsg` writes a sibling compact CSV log with per-message summaries.
- `-trace_err` writes unexpected SIP responses and runtime failures to `-error_file`.
- `-trace_error_codes` writes a sibling compact CSV file with unexpected SIP response codes and expected match criteria.
- `-trace_logs` writes XML `<log>` action output to `-log_file`.
- `-trace_stat` writes periodic and final CSV stats snapshots to a sibling `*_stats.log` file; when `-message_file` is set it uses that base path. The CSV now includes cumulative totals, per-interval delta fields, and latency standard deviation columns.
- Exported stats also include failure-class counters so automation can distinguish timeouts, unexpected SIP, transport errors, parse errors, scenario errors, and cancellations.
- `-trace_rtt` writes each completed named RTD sample to a sibling `*_rtt.log` CSV file with timestamp, call, RTD name, and duration in milliseconds.
- Summary JSON now exports repartition buckets for `call_length`, `invite_rtt`, and named `rtd` timers so automation can consume latency distributions without parsing raw RTT dumps.
- `-hep_addr` mirrors SIP `send` / `recv` traffic to a Homer-compatible HEP3 collector over UDP.
- `-hep_capture_id` sets the HEP capture node ID; `-hep_password` sets the optional HEP auth key.
- The current HEP MVP exports SIP signaling only; RTP/RTCP mirroring is not included yet.
- `[authentication]` currently covers Digest `401` / `407` challenge responses with `MD5` and `qop=auth`; the scenario must explicitly place `[authentication]` into the retried request.
- `start_rtd` and `rtd` now record named per-step timings into the summary model; they are especially useful for XML flows like `send INVITE` -> `recv 200`.
- `counter` and `display` are currently exposed as successful-command execution counters in the summary model, which is a practical first step toward richer SIPp-style reporting.
- In external 3PCC-style flows, the first incoming `recvCmd` can automatically adopt its `Call-ID` into `[call_id]` for later commands.
- `init` can also use `sendCmd` / `recvCmd`, so inter-instance setup data may be loaded into global scopes before SIP traffic starts.
- When launched with `-slave`, `gossipper` validates that the scenario first enters the flow via `recvCmd` before it sends any `sendCmd`.
- RTP support is a separate milestone layered on top of the SIP engine.
- Current RTP support focuses on audio streaming from PCM mono 8kHz WAV input, audio PCAP replay, RTP echo, and basic RTCP observability.
- `play_pcap_audio` currently replays UDP payloads from the capture as RTP toward the negotiated audio endpoint; `play_pcap_video` and `play_pcap_image` are still deferred.

## License

MIT. See `LICENSE`.
