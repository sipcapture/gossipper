# Statistics Mapping

This document maps the current `gossIpper` statistics model to the SIPp-style
statistics described in `../sipp/docs/statistics.rst`.

The goal is practical interoperability for automation and reporting, not a
claim of byte-for-byte SIPp output parity.

## Summary

`gossIpper` currently exposes statistics through three surfaces:

- final JSON summary via `-summary_json`
- periodic/final CSV snapshots via `-trace_stat`
- raw RTD sample CSV via `-trace_rtt`

SIPp exposes overlapping information, but some counters are more detailed or
more tightly coupled to its legacy UI and transport internals.

## Mapping Table

| SIPp counter / concept | `gossIpper` surface | Mapping status | Notes |
| --- | --- | --- | --- |
| `StartTime` | `started_at` | approximate | JSON timestamp of collector start |
| `CurrentTime` | `finished_at` / `timestamp` | approximate | JSON uses final snapshot time; `-trace_stat` uses per-row snapshot timestamp |
| `ElapsedTime` | `duration` / `elapsed_ms` | supported | Same operational meaning |
| `CallRate` | `calls_per_second` / `interval_calls_per_second` | approximate | `calls_per_second` is cumulative average; `interval_calls_per_second` is the nearest equivalent to periodic rate |
| `IncomingCall` | none | deferred | No separate incoming-call counter yet |
| `OutgoingCall` | none | deferred | No separate outgoing-call counter yet |
| `TotalCallCreated` | `total_calls` | supported | Total calls started by the engine |
| `CurrentCall` | `active_calls` | supported | Ongoing calls |
| `SuccessfulCall` | `success_calls` | supported | Successful completed calls |
| `FailedCall` | `failed_calls` | supported | Total failed calls |
| `FailedCannotSendMessage` | `failure_classes.transport_error` | approximate | Groups transport-level send/read failures under one class |
| `FailedMaxUDPRetrans` | none | deferred | Retransmits are counted, but max-retrans failure is not split out yet |
| `FailedUnexpectedMessage` | `failure_classes.unexpected_sip` | supported | Failed because received SIP did not match the scenario |
| `FailedCallRejected` | `failure_classes.scenario_error` | approximate | Covers generic scenario/action/internal execution failure |
| `FailedCmdNotSent` | `failure_classes.scenario_error` | approximate | Not split into a dedicated external-command error counter yet |
| `FailedRegexpDoesntMatch` | `failure_classes.scenario_error` | approximate | Currently folded into scenario failure class |
| `FailedRegexpShouldntMatch` | `failure_classes.scenario_error` | approximate | Currently folded into scenario failure class |
| `FailedRegexpHdrNotFound` | `failure_classes.parse_error` or `failure_classes.scenario_error` | approximate | Depends on where the failure is detected |
| `FailedOutboundCongestion` | none | deferred | No dedicated TCP congestion class yet |
| `FailedTimeoutOnRecv` | `timeouts` / `failure_classes.timeout` | supported | Timeout counter plus explicit failure-class split |
| `FailedTimeoutOnSend` | none | deferred | No dedicated send-timeout counter yet |
| `OutOfCallMsgs` | none | deferred | Not exported as a dedicated counter yet |
| `Retransmissions` | `retransmits` / `delta_retransmits` | supported | Cumulative and periodic delta available in `-trace_stat` |
| scenario-defined counters | `counters` | supported | Exported in final JSON summary |
| scenario-defined displays | `displays` | supported | Exported in final JSON summary |
| `ResponseTime` | `rtd` / `-trace_rtt` | supported | Named RTD aggregates in JSON, raw RTD samples in `-trace_rtt` |
| `CallLength` | `call_length` | supported | Includes avg/min/max/last/stddev and repartition buckets |
| RTD repartition | `rtd.<name>.buckets` | supported | Bucketed latency distribution per RTD name |
| `ResponseTime` STDev | `rtd.<name>.stddev` | supported | Exported in summary JSON |
| `CallLength` STDev | `call_length.stddev` / `call_stddev_ms` | supported | JSON plus compact periodic CSV column |

## `gossIpper`-only exports

These fields do not have a clean one-to-one SIPp counter, but are intentionally
useful in automation:

- `success_ratio`
- `average_call_latency`
- `average_invite_rtt`
- `media.*`
- `failure_classes.*`
- `delta_*` periodic columns in `-trace_stat`
- latency `buckets` for `call_length`, `invite_rtt`, and named `rtd`

## Practical guidance

- Prefer `-summary_json` when you need a final machine-readable report.
- Prefer `-trace_stat` when you need time-series snapshots for dashboards or CI.
- Prefer `-trace_rtt` when you need raw per-measure latency samples.
- Treat `docs/trace-schema-contract.md` as the source of truth for stable
  `-trace_stat` / `-trace_rtt` CSV header and column order guarantees.
- When porting SIPp-based automation, treat `interval_calls_per_second`,
  `delta_*`, and `failure_classes.*` as the closest modern equivalents to SIPp's
  periodic and split failure counters.

## Known gaps

The following SIPp-side statistics are still not exported one-for-one:

- separate incoming vs outgoing call counters
- dedicated max-UDP-retrans failure counter
- dedicated command-transport failure split
- dedicated regexp failure splits
- dedicated outbound congestion counter
- dedicated send-timeout counter
- out-of-call message counter
