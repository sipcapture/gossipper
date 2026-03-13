# Trace CSV schema contract

This document defines the stable CSV schema contract for `gossipper` trace files
used by automation and CI integrations.

If any column name or order changes, the contract has changed and downstream
parsers may break.

## Scope

- `-trace_stat` sibling `_stats` CSV file
- `-trace_rtt` sibling `_rtt` CSV file
- `-trace_screen` sibling `_screen` CSV file

## `-trace_stat` header (exact order)

```text
timestamp,elapsed_ms,total_calls,success_calls,failed_calls,active_calls,success_ratio,calls_per_second,retransmits,timeouts,avg_call_ms,call_stddev_ms,avg_invite_ms,invite_stddev_ms,rtp_packets_sent,rtp_packets_received,rtcp_sender_reports,rtcp_receiver_reports,rtcp_packets_received,failure_timeout,failure_unexpected_sip,failure_transport_error,failure_parse_error,failure_scenario_error,failure_cancelled,interval_ms,interval_calls_per_second,delta_total_calls,delta_success_calls,delta_failed_calls,delta_retransmits,delta_timeouts,delta_rtp_packets_sent,delta_rtp_packets_received,delta_rtcp_sender_reports,delta_rtcp_receiver_reports,delta_rtcp_packets_received,delta_failure_timeout,delta_failure_unexpected_sip,delta_failure_transport_error,delta_failure_parse_error,delta_failure_scenario_error,delta_failure_cancelled
```

Notes:
- emission cadence is controlled by `-fd` (seconds)
- rows include both cumulative and per-interval (`delta_*`) values

## `-trace_rtt` header (exact order)

```text
timestamp,call,name,value_ms
```

Notes:
- flush cadence is controlled by `-rtt_freq` (completed calls)
- final buffered RTT rows are flushed on shutdown

## `-trace_screen` header (exact order)

```text
timestamp,total_calls,success_calls,failed_calls,active_calls,success_ratio,calls_per_second,interval_ms,interval_calls_per_second,avg_call_ms,avg_invite_ms,retransmits,timeouts,failure_timeout,failure_unexpected_sip
```

Notes:
- emission cadence is controlled by `-fd` (seconds)
- final snapshot row is flushed on shutdown
- rows include both cumulative and per-interval throughput fields

## Change policy

When changing these headers:

1. update this document
2. update compatibility docs (`docs/compatibility.md`, `docs/statistics-mapping.md`)
3. update/extend tests that enforce the schema contract
4. call out the change in release notes for parser users

## Backward-compatible alias policy

To keep parser behavior deterministic, `gossipper` does not emit parallel
"legacy alias" CSV columns for old field names/order.

Policy:

- one canonical header contract is emitted for each trace file
- if a breaking rename/reorder is ever required, treat it as a contract change
  and communicate it explicitly in release notes
- parser migration should use a small external mapping/adapter layer rather than
  dual-writing multiple header variants in runtime traces
