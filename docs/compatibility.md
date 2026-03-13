# XML compatibility matrix

This document defines the currently supported SIPp subset in `gossIpper`.

Unsupported XML actions now fail during scenario parsing, and unsupported
scenario keywords fail during template rendering instead of silently degrading
into no-ops or empty strings.

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
| `assign` | supported | Assigns a floating-point value from `value` or `variable` |
| `assignstr` | supported | Assigns rendered string values to variables |
| `todouble` | supported | Converts a string variable into a floating-point variable |
| `add` | supported | Floating-point arithmetic against `assign_to` using `value` or `variable` |
| `subtract` | supported | Floating-point arithmetic against `assign_to` using `value` or `variable` |
| `multiply` | supported | Floating-point arithmetic against `assign_to` using `value` or `variable` |
| `divide` | supported | Floating-point arithmetic against `assign_to` using `value` or `variable`; divide-by-zero fails the call |
| `strcmp` | supported | Lexicographic compare for `variable` against `value` or `variable2`; stores `-1`, `0`, or `1` |
| `test` | supported | Stores boolean result as `1` or `0`; supports `value` or `variable2` with `equal`, `not_equal`, `greater_than`, `less_than`, `greater_than_equal`, and `less_than_equal` |
| `log` | supported | Emits message when tracing is enabled |
| `warning` | supported | Emits message into the error trace when `-trace_err` / `-error_file` is enabled |
| `lookup` | partial | Looks up the first CSV column and stores a 1-based physical file line number; integrates with `[fieldN ... line=$var]` |
| `jump` | supported | Jumps to an absolute scenario command index via `value` or `variable` |
| `gettimeofday` | supported | Stores epoch seconds and microseconds in `assign_to` targets |
| `urlencode` | supported | URL-encodes the referenced variable in place |
| `urldecode` | supported | URL-decodes the referenced variable in place |
| `verifyauth` | supported | Validates incoming Digest `Authorization` / `Proxy-Authorization` headers for `MD5` and `SHA-256` with `qop=auth` |
| `exec` | supported | Supports `command`, `int_cmd`, `rtp_stream` `start` / `pause` / `resume` / `stop` / `echo`, and `play_pcap_audio`; audio PCAP replay preserves packet timing and reuses the SDP audio endpoint |
| `sample` | deferred | Statistical variable sampling is not implemented yet |
| `insert` | deferred | In-memory injection file mutation is not implemented yet |
| `replace` | deferred | In-memory injection file mutation is not implemented yet |
| `setdest` | deferred | Deferred to transport/addressing parity work in Milestone 3 |
| `play_pcap_video` | deferred | Deferred to broader media parity work |

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
| `[last_Request_URI]` | supported | Uses the last received request URI when available, otherwise falls back to the URI in the last `To` header |
| `[last_cseq_number]` | supported | Extracted from the last `CSeq` header |
| `[server_ip]` | supported | Currently resolves to the local/server bind IP; multi-IP semantics are deferred to Milestone 3 |
| `[next_url]` | supported | Extracted from the last `Contact` header |
| `[peer_tag_param]` | supported | Extracted from the last `To` header |
| `[media_ip]` | supported | Mirrors local IP for now |
| `[media_ip_type]` | supported | Mirrors local IP type |
| `[media_port]` | supported | Derived from local SIP port with per-call offset |
| `[date]` | supported | Current UTC date in RFC1123 format |
| `[timestamp]` | supported | Current local timestamp |
| `[authentication]` | supported | Digest auth for `401`/`407` challenges with CLI credentials from `-au` / `-ap` or inline `username=` / `password=` params; supports `MD5` and `SHA-256` with `qop=auth` |
| `[fieldN ...]` | partial | CSV injection with `file=` and optional `line=`; variable-driven form currently uses `line=$var`, and line numbers are physical 1-based CSV rows |
| `[file ...]` | supported | Inlines file contents from scenario-relative or absolute path |
| `[users]` | supported | Number of configured logical users from `-users` |
| `[userid]` | supported | Zero-based logical user identifier for the current call |
| `[$n]` / `[$name]` | supported | Action and string variables |
| `[dynamic_id]` | deferred | Not implemented yet |
| `[routes]` | deferred | Record-Route capture / replay semantics are not implemented yet |
| `[clock_tick]` | deferred | Internal SIPp clock tick helper is not implemented yet |
| `[sipp_version]` | deferred | Not implemented yet |
| `[tdmmap]` | deferred | Not implemented yet |
| `[fill]` | deferred | Not implemented yet |

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

## Run control CLI workflow

| CLI surface | Status | Notes |
| --- | --- | --- |
| `-timeout_global` | supported | Exits after N seconds of total runtime for deterministic CI/load runs |
| `-rate_scale` | supported | Sets interactive CPS step size used by runtime TUI controls (`+/-` for 1x step and `*`/`/` for 10x step) |
| `-rate_increase` + `-rate_interval` + `-rate_max` | supported | Applies periodic CPS ramp-up/ramp-down during run; `-rate_max` caps upper bound when set |
| `-max_socket` | partial | Caps per-call transport concurrency (`un`, `tn`, `ln`) by limiting simultaneously open call sockets |
| `-max_reconnect` + `-reconnect_sleep` + `-reconnect_close` | partial | Shared client TCP/TLS transports (`t1`, `l1`) support reconnect retries and close-on-reconnect behavior |

## Trace CLI workflow

| CLI surface | Status | Notes |
| --- | --- | --- |
| `-trace_msg` | supported | Writes full sent/received messages to the configured file |
| `-message_file` | supported | Explicit path for the full message trace log; also enables `-trace_msg` |
| `-trace_shortmsg` | supported | Writes a compact CSV sibling log with timestamp, direction, protocol, summary, and `Call-ID` |
| `-trace_counts` | supported | Writes periodic CSV snapshots with per-scenario SIP command counters (`sent`, `recv`, `unexp`); additional per-message detail is intentionally deferred to keep schema stable in M5 |
| `-trace_stat` | supported | Writes periodic and final CSV stats snapshots to a sibling `_stats` trace file with both cumulative totals and per-interval delta fields |
| `-fd` | supported | Controls `-trace_stat` snapshot frequency in seconds (SIPp-compatible naming) |
| `-trace_rtt` | supported | Writes each completed named RTD sample to a sibling `_rtt` CSV trace file |
| `-rtt_freq` | supported | Controls `-trace_rtt` flush cadence in completed calls (default `200`) |
| CSV schema contract | supported | Stable `-trace_stat` / `-trace_rtt` / `-trace_screen` header contract is documented in `docs/trace-schema-contract.md` |
| legacy trace CSV alias columns | deferred | Not emitted by design; migration should use explicit parser adapters, see `docs/trace-schema-contract.md` |
| `-trace_err` | supported | Writes unexpected SIP messages and runtime failures to the configured error file |
| `-error_file` | supported | Explicit path for the error trace log; also enables `-trace_err` |
| `-trace_error_codes` | supported | Writes a compact sibling CSV file with unexpected SIP response codes, reasons, `Call-ID`, and expected match |
| `-trace_screen` | supported | Writes periodic non-interactive runtime summary snapshots to a screen CSV log with success ratio, interval throughput, and key failure counters |
| `-screen_file` | supported | Explicit path for the screen snapshot log; also enables `-trace_screen` |
| `SIGUSR1` screen dump trigger | supported | Forces an immediate runtime screen snapshot when `-trace_screen` is enabled |
| `-trace_logs` | supported | Writes XML action `<log>` output to a dedicated file |
| `-log_file` | supported | Explicit path for the action log trace file; also enables `-trace_logs` |

## Statistics export

| Export surface | Status | Notes |
| --- | --- | --- |
| summary JSON failure classes | supported | Exports failure class counters such as `timeout`, `unexpected_sip`, `transport_error`, `parse_error`, `scenario_error`, and `cancelled` |
| `-trace_stat` failure class columns | supported | Periodic stats CSV includes cumulative and delta columns for the same failure classes |
| summary JSON latency repartition | supported | Exports `call_length`, `invite_rtt`, and named `rtd` summaries with stddev and repartition buckets |
| `-trace_stat` latency stddev columns | supported | Periodic stats CSV includes call and invite latency standard deviation columns |
| SIPp stats field mapping doc | supported | See `docs/statistics-mapping.md` for the current field-by-field correspondence and gaps |

## HEP CLI workflow

| CLI surface | Status | Notes |
| --- | --- | --- |
| `-hep_addr` | supported | Mirrors SIP `send` / `recv` messages to a Homer-compatible HEP3 UDP collector |
| `-hep_capture_id` | supported | Sets the HEP capture node ID |
| `-hep_password` | supported | Sets the optional HEP auth key |
| RTP/RTCP mirroring | deferred | Current HEP MVP exports SIP signaling only |

## Authentication CLI workflow

| CLI surface | Status | Notes |
| --- | --- | --- |
| `-au` | supported | Authorization username; defaults to `-s` value like SIPp |
| `-ap` | supported | Authorization password; defaults to `password` like SIPp |
| `-base_cseq` | supported | Sets the base value used by `[cseq]` token rendering |
| `-rp` | supported | SIPp-style rate period for `-r` (`n` calls per `rp` milliseconds) |
| runtime interactive rate keys (`+`, `-`, `*`, `/`) | supported | TUI adjusts target CPS by `-rate_scale` (`+/-`: `1x`, `*`/`/`: `10x`) |
| `-reconnect_close` | partial | In shared client `t1`/`l1`, closes active calls by disabling reconnect attempts when a socket loss is detected |
| challenged request retry via `[authentication]` | supported | Works for Digest `401` / `407` flows when the scenario explicitly places `[authentication]` in the retried request |

## Deliberately deferred

- `sample`, `insert`, and `replace` action families
- `setdest` and broader per-call addressing changes
- `play_pcap_video` / `play_pcap_image`
- full `-inf` / indexed injection parity beyond the current CSV helper subset
- keyword helpers such as `[routes]`, `[dynamic_id]`, `[clock_tick]`, `[sipp_version]`, `[tdmmap]`, and `[fill]`
- SRTP / rtpcheck
- full CLI parity with SIPp
