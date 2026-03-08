# XML compatibility matrix

This document defines the currently supported SIPp subset in `gossip`.

## Commands

| SIPp command | Status | Notes |
| --- | --- | --- |
| `send` | supported | UDP and TCP sends; `retrans` is honored for UDP |
| `recv` | supported | Match by `request` or `response`; optional receives supported |
| `pause` | supported | Uses `milliseconds`, falls back to CLI default |
| `nop` | supported | Structural no-op, still participates in branching |
| `label` | supported | Used as a target for `next` jumps |
| `timewait` | supported | Executed as a timed pause |
| `sendCmd` | supported | Supports local command bus and external peer delivery with `dest`, `Call-ID` correlation, and optional `From` sender identity |
| `recvCmd` | supported | Receives from the local bus or external peers, supports actions, timeout, optional `src`, and first-command correlation adoption for 3PCC-style flows so later `[call_id]` can reuse the adopted command context |
| `init` | supported | Supports initialization `nop`, `pause`, `label`, actions, and command exchange via `sendCmd` / `recvCmd` before traffic starts |

## Common attributes

| Attribute | Status | Notes |
| --- | --- | --- |
| `next` | supported | Jumps to a `label` |
| `test` | supported | Evaluated against in-memory variables and action results |
| `chance` | supported | Floating point `0..1` |
| `condexec` | supported | Works with in-memory variables and action results |
| `condexec_inverse` | supported | Same behavior as SIPp-style inverted conditional execution |
| `counter` | supported | Successful command execution increments the named counter in summary stats |
| `display` | supported | Successful command execution increments the named display label in summary stats |
| `start_rtd` | supported | Starts a named RTD timer when the command begins execution |
| `rtd` | supported | Stops the named RTD timer on successful command completion and aggregates it into summary stats |
| `timeout` | supported | Receive timeout in milliseconds |
| `milliseconds` | supported | `pause` and `timewait` |

## Scope declarations

| Declaration | Status | Notes |
| --- | --- | --- |
| `Global` | supported | Shared variables across calls |
| `User` | supported | Variables shared per logical user (`-users`) |
| `Reference` | parsed | Stored for compatibility and future validation |

## Actions

| Action | Status | Notes |
| --- | --- | --- |
| `ereg` | supported | `msg`, `hdr`, `body`, and `var` search scopes |
| `assignstr` | supported | Assigns rendered string values to variables |
| `test` | supported | Stores boolean result as `1` or `0` |
| `log` | supported | Emits message when tracing is enabled |
| `exec` | supported | Supports `command`, `int_cmd`, `rtp_stream` `start` / `pause` / `resume` / `stop` / `echo`, and `play_pcap_audio`; audio PCAP replay preserves packet timing and reuses the SDP audio endpoint |

## Keywords

| Keyword | Status | Notes |
| --- | --- | --- |
| `[service]` | supported | CLI `-s` |
| `[remote_host]` | supported | Remote host |
| `[remote_ip]` | supported | Resolved remote IP |
| `[remote_port]` | supported | Supports `+/-offset` |
| `[local_ip]` | supported | Local bind IP |
| `[local_ip_type]` | supported | `4` or `6` |
| `[local_port]` | supported | Supports `+/-offset` |
| `[transport]` | supported | Renders `UDP`, `TCP`, or `TLS` depending on transport |
| `[call_id]` | supported | Generated per call; in command-only/external 3PCC flows it is adopted from the first incoming `recvCmd` correlation message when needed |
| `[cseq]` | supported | Basic numeric rendering with offsets |
| `[branch]` | supported | Deterministic per message |
| `[len]` | supported | Two-pass body length calculation |
| `[call_number]` | supported | Monotonic call index |
| `[msg_index]` | supported | Scenario message index |
| `[pid]` | supported | Current process ID |
| `[last_message]` | supported | Last received SIP message |
| `[last_*]` | supported | Missing header drops the whole line, matching SIPp semantics |
| `[last_cseq_number]` | supported | Extracted from the last `CSeq` header |
| `[next_url]` | supported | Extracted from the last `Contact` header |
| `[peer_tag_param]` | supported | Extracted from the last `To` header |
| `[media_ip]` | supported | Mirrors local IP for now |
| `[media_ip_type]` | supported | Mirrors local IP type |
| `[media_port]` | supported | Derived from local SIP port with per-call offset |
| `[date]` | supported | Current UTC date in RFC1123 format |
| `[timestamp]` | supported | Current local timestamp |
| `[authentication]` | supported | Digest auth for `401`/`407` challenges with CLI credentials from `-au` / `-ap`; currently supports `MD5` and `qop=auth` |
| `[fieldN ...]` | supported | CSV injection with `file=` and optional `line=` |
| `[file ...]` | supported | Inlines file contents from scenario-relative or absolute path |
| `[$n]` / `[$name]` | supported | Action and string variables |

## Transport modes

| Mode | Status |
| --- | --- |
| `u1` | supported |
| `un` | supported |
| `s1` | supported as server-side UDP alias |
| `sn` | supported as server-side UDP alias |
| `t1` | supported |
| `tn` | supported |
| `l1` | supported |
| `ln` | supported |

## 3PCC CLI workflow

| CLI surface | Status | Notes |
| --- | --- | --- |
| `-cmd_name` + `-cmd_peers` | supported | Low-level external command transport configuration |
| `-master` + `-slave_cfg` | supported | SIPp-style alias for naming the local master instance and peer map |
| `-slave` + `-slave_cfg` | supported | SIPp-style alias for naming the local slave instance and peer map |
| slave first step validation | supported | `-slave` scenarios must enter via `recvCmd` before their first `sendCmd` |
| example scenarios | supported | `testdata/scenarios/3pcc_master.xml` and `testdata/scenarios/3pcc_slave.xml` |

## Out-of-call SIP workflow

| Workflow | Status | Notes |
| --- | --- | --- |
| stateless client `send` request + `recv` response | supported | Works for request/response exchanges such as `OPTIONS` ping |
| stateless server `recv request` + `send` response | supported | Works for responder scenarios such as `OPTIONS` pong |
| example scenarios | supported | `testdata/scenarios/options_client.xml` and `testdata/scenarios/options_server.xml` |

## Trace CLI workflow

| CLI surface | Status | Notes |
| --- | --- | --- |
| `-trace_msg` | supported | Writes full sent/received messages to the configured file |
| `-message_file` | supported | Explicit path for the full message trace log; also enables `-trace_msg` |
| `-trace_shortmsg` | supported | Writes a compact CSV sibling log with timestamp, direction, protocol, summary, and `Call-ID` |
| `-trace_err` | supported | Writes unexpected SIP messages and runtime failures to the configured error file |
| `-error_file` | supported | Explicit path for the error trace log; also enables `-trace_err` |
| `-trace_error_codes` | supported | Writes a compact sibling CSV file with unexpected SIP response codes, reasons, `Call-ID`, and expected match |
| `-trace_logs` | supported | Writes XML action `<log>` output to a dedicated file |
| `-log_file` | supported | Explicit path for the action log trace file; also enables `-trace_logs` |

## Authentication CLI workflow

| CLI surface | Status | Notes |
| --- | --- | --- |
| `-au` | supported | Authorization username; defaults to `-s` value like SIPp |
| `-ap` | supported | Authorization password; defaults to `password` like SIPp |
| challenged request retry via `[authentication]` | supported | Works for Digest `401` / `407` flows when the scenario explicitly places `[authentication]` in the retried request |

## Deliberately deferred

- `play_pcap_video` / `play_pcap_image`
- SRTP / rtpcheck
- full CLI parity with SIPp
