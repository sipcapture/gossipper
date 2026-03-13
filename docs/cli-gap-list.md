# CLI gap list for Milestone 5

This document tracks `gossIpper` CLI parity gaps against SIPp for Milestone 5.

It is intentionally pragmatic:

- prioritize automation value and operability over historical completeness
- close high-value gaps first (`P0`)
- explicitly defer legacy or low-payoff switches (`P2`)

## Priority model

| Priority | Meaning | Typical use |
| --- | --- | --- |
| `P0` | must-have for CI/load automation | repeatable unattended test runs |
| `P1` | high operator value | runtime diagnostics and incident triage |
| `P2` | legacy/optional parity | niche or historical SIPp compatibility |

## Status model

| Status | Meaning |
| --- | --- |
| `supported` | implemented and documented in `gossIpper` |
| `partial` | partly implemented; behavior differs from SIPp |
| `missing` | not implemented yet |
| `deferred` | intentionally postponed (outside current milestone scope) |

## Ground truth used for this matrix

- `internal/cli/config.go`: currently parsed CLI flags and validation rules
- `cmd/gossip/main.go`: top-level `-interactive` / `tui` / `-version` handling
- `docs/compatibility.md`: project-level status for CLI/transport semantics
- SIPp reference docs:
  - `../sipp/docs/statistics.rst`
  - `../sipp/docs/transport.rst`
  - `../sipp/docs/scenarios/inject_from_csv.rst`
  - `../sipp/docs/scenarios/keywords.rst`

## Implemented CLI surface in `gossIpper` (current)

| CLI surface | Status | Priority | Notes |
| --- | --- | --- | --- |
| `-sf`, `-sn`, `-rsa` (or positional remote address), `-s`, `-i`, `-p` | `supported` | `P0` | core scenario and addressing startup flags |
| `-t u1`, `-t un`, `-t t1`, `-t tn`, `-t l1`, `-t ln` | `supported` | `P0` | core transport coverage for UDP/TCP/TLS modes |
| `-t s1`, `-t sn` | `partial` | `P1` | accepted as server-side UDP aliases, not SIPp SCTP semantics |
| `-r`, `-rp`, `-rate_scale`, `-rate_increase`, `-rate_interval`, `-rate_max`, `-max_socket`, `-max_reconnect`, `-reconnect_sleep`, `-reconnect_close`, `-m`, `-l`, `-users`, `-pause_ms`, `-recv_timeout_ms`, `-timeout_global` | `supported` | `P0` | basic load/scheduling controls are present, including SIPp-style rate periods, runtime rate ramp controls, socket cap for per-call modes, shared TCP/TLS reconnect knobs, interactive rate scale, and global run timeout |
| `-summary_json` | `supported` | `P0` | structured final stats for automation |
| `-trace_msg`, `-message_file`, `-trace_shortmsg` | `supported` | `P0` | full and compact message traces |
| `-trace_err`, `-error_file`, `-trace_error_codes` | `supported` | `P0` | error trace and compact response-code diagnostics |
| `-trace_logs`, `-log_file`, `-trace_stat`, `-trace_rtt`, `-trace_screen`, `-screen_file` | `supported` | `P0` | action logs, periodic stats/RTD exports, and non-interactive runtime screen snapshots |
| `-hep_addr`, `-hep_capture_id`, `-hep_password` | `supported` | `P1` | SIP signaling mirroring to HEP3 collector |
| `-au`, `-ap` | `supported` | `P0` | auth defaults and explicit credentials |
| `-tls_cert`, `-tls_key`, `-tls_ca`, `-tls_skip_verify` | `supported` | `P1` | TLS runtime configuration |
| `-cmd_name`, `-cmd_peers`, `-master`, `-slave`, `-slave_cfg` | `supported` | `P1` | external command transport and SIPp-style 3PCC aliases |
| `-interactive`, `tui`, `-version`, `--version` | `supported` | `P1` | runtime UI and version surface |

## SIPp parity gaps for Milestone 5

| SIPp flag / behavior | SIPp purpose | Current status in `gossIpper` | Priority | Next action | Notes |
| --- | --- | --- | --- | --- | --- |
| `-trace_stat` CSV contract parity | stable periodic statistics schema | `supported` | `P0` | keep contract stable and version changes explicitly | exact header contract is documented and test-guarded |
| `-trace_rtt` contract parity | RTD sample export compatible with existing tooling | `supported` | `P0` | keep contract stable and version changes explicitly | exact header contract is documented and test-guarded |
| `-fd` (stats dump frequency) | explicit snapshot interval control | `supported` | `P0` | keep CSV contract stable for automation consumers | implemented as `-fd` seconds for `-trace_stat` snapshots |
| `-rtt_freq` | control RTD dump cadence | `supported` | `P0` | keep behavior documented and stable for CI tools | implemented as flush every N completed calls (default `200`) |
| `-trace_counts` | per-message counters CSV export | `supported` | `P1` | keep schema stable and extend only with additive columns | current export provides per-scenario SIP command `sent` / `recv` / `unexp` counters |
| selected rate/scheduling knobs beyond `-r/-m/-l` | richer load profile control | `partial` | `P0` | define top missing knobs by automation value and add incrementally | `-rp`, `-rate_scale`, `-rate_increase`/`-rate_interval`/`-rate_max`, `-max_socket`, and `-timeout_global` now supported; keep deterministic behavior first |
| SIPp-style runtime reporting behavior (selected non-interactive subset) | operator visibility and triage during run | `supported` | `P1` | keep screen CSV contract stable and extend additively only | interactive TUI exists; non-interactive screen CSV snapshots are available via `-trace_screen` / `-screen_file`, plus on-demand dump trigger via `SIGUSR1` |
| `-base_cseq` | explicit initial CSeq seed | `supported` | `P1` | keep behavior documented and add coverage for offsets via `[cseq+N]` | base CSeq now controls `[cseq]` token rendering |
| `-t ui` | one UDP socket per source IP from injection data | `deferred` | `P2` | keep in Milestone 3 scope | transport/addressing redesign item |
| `-inf` + `-ip_field` for transport/IP selection | per-call local IP selection in `ui` workflow | `deferred` | `P2` | keep in Milestone 3 scope | related to `-t ui` and multi-IP behavior |
| `-infindex` | indexed CSV injection lookup acceleration | `missing` | `P2` | triage after P0/P1 CLI work | separate from current `lookup` action MVP |
| TCP reconnect controls (`-max_reconnect`, `-reconnect_close`, `-reconnect_sleep`) | reconnection policy tuning | `partial` | `P2` | extend semantics if full SIPp parity is required | supported for shared client TCP/TLS (`t1`, `l1`); `-reconnect_close=true` currently closes active calls by disabling reconnect attempts |
| `-max_socket` (multi-socket cap) | cap number of active sockets | `partial` | `P2` | evaluate server/shared transport alignment later | currently caps per-call client socket fanout (`un`, `tn`, `ln`) |

## Candidate P0 shortlist (execution order)

1. shipped one high-value load knob beyond `-r/-m/-l`: `-rp` (SIPp-compatible rate period)
2. added smoke checks for produced trace artifacts (`-trace_stat`, `-trace_rtt`, `-trace_error_codes`, `-trace_screen`)
3. aligned docs/examples with `-fd`, `-rtt_freq`, `-trace_counts`, and `-trace_screen` usage patterns
4. decided parser migration policy: no runtime legacy alias columns, use explicit adapter/mapping
5. evaluated optional per-message counter detail; deferred after Milestone 5 to keep schema stable (`sent` / `recv` / `unexp`)

## Deliverables checklist

- documented `supported / partial / missing / deferred` CLI map
- grouped implementation plan: `high-value in automation` vs `legacy/optional`
- at least one smoke test validating trace/stat artifact structure
- update `docs/compatibility.md` after each flag family lands

## Out-of-scope for Milestone 5

- transport/addressing redesign (`-t ui`, `-inf`, `-ip_field`) from Milestone 3
- media parity work (`play_pcap_video`, `play_pcap_image`, broader replay semantics) from Milestone 4
