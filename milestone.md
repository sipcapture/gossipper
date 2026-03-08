# gossipper milestones toward broader SIPp parity

This file tracks the next practical development milestones for `gossipper`,
focusing on features that exist in `SIPp` and are still missing or only
partially covered here.

It is intentionally prioritized. The goal is not to clone every historical
SIPp feature at once, but to close the highest-value parity gaps in a controlled
order.

## Baseline already implemented

`gossipper` already covers a useful subset:

- SIP XML scenarios with `send`, `recv`, `pause`, `nop`, `label`, `timewait`,
  and `init`
- external and local `sendCmd` / `recvCmd`
- UDP, TCP, TLS, and server-side UDP aliases
- digest auth via `[authentication]`
- RTD, `counter`, `display`, tracing, and JSON summary output
- periodic CSV stats snapshots via `-trace_stat`
- `rtp_stream`, RTP echo, RTCP counters, and `play_pcap_audio`

## Source references used for this plan

The milestones below are based on the gaps visible in:

- `../sipp/docs/transport.rst`
- `../sipp/docs/media.rst`
- `../sipp/docs/statistics.rst`
- `../sipp/docs/scenarios/actions.rst`
- `../sipp/docs/scenarios/keywords.rst`

## Milestone 1: Statistics and reporting parity

Priority: high

Status: in progress

Why first:

- `gossipper` already has JSON summaries and basic RTD/counter support
- SIPp still offers richer built-in reporting flows that are useful in CI and
  long-running load tests

Target gaps from SIPp:

- richer cumulative vs periodic counters
- broader call-failure classification in exported stats
- SIPp-like latency / repartition style reporting where it adds real value

Completed so far in this milestone:

- `-trace_stat` style periodic CSV snapshots via sibling `*_stats.log` output
- `-trace_rtt` style RTD dump files via sibling `*_rtt.log` CSV output

Deliverables:

- documented field mapping between current `gossipper` JSON and SIPp-style counters

## Milestone 2: XML action and scenario parity

Priority: high

Target gaps from SIPp:

- `warning` action
- `lookup` action and adjacent data-lookup behavior from SIPp scenario docs
- broader action surface and legacy scenario semantics beyond the currently
  implemented `ereg` / `assignstr` / `test` / `log` / `exec`
- wider keyword catalog where SIPp still exposes scenario helpers not yet in
  `gossipper`

Deliverables:

- action compatibility matrix extended beyond current subset
- tests for newly added actions
- explicit "supported / partial / deferred" notes for each action family

## Milestone 3: Transport and addressing parity

Priority: medium-high

Target gaps from SIPp:

- `-t ui` ("UDP with one socket per IP address")
- `-inf` + `-ip_field` workflow for per-call local IP selection
- `[server_ip]` keyword semantics used with multi-IP server binding
- broader transport-related parity after the current `u1` / `un` / `s1` / `sn`
  / `t1` / `tn` / `l1` / `ln` coverage

Deliverables:

- multi-IP transport design
- CSV-backed per-call IP selection
- client and server tests for `ui` behavior

## Milestone 4: Media parity beyond current audio-first scope

Priority: medium-high

Target gaps from SIPp:

- `play_pcap_video`
- `play_pcap_image`
- broader generic PCAP replay coverage like SIPp's "play any RTP stream"
  positioning
- bidirectional RTP / SRTP checking (`rtpcheck`)
- wider codec/file handling beyond the currently practical `gossipper` subset

Deliverables:

- media endpoint discovery for non-audio streams where appropriate
- explicit video/image replay decisions instead of leaving them implicit
- a separate decision point for SRTP and `rtpcheck`

## Milestone 5: CLI parity and operational UX

Priority: medium

Target gaps from SIPp:

- broader CLI flag parity beyond the currently implemented core
- better compatibility for SIPp-style statistics / tracing switches
- improved operator-facing runtime visibility comparable to SIPp's mature UX

Examples of likely candidates:

- additional trace/export flags
- parity for more rate / scheduling knobs
- screen/report behaviors that matter in automation or troubleshooting

Deliverables:

- documented CLI gap list
- grouped implementation by "high-value in automation" vs "legacy / optional"

## Milestone 6: Close the long tail of XML and keyword semantics

Priority: medium-low

This milestone is intentionally later because it carries the highest blast
radius and the lowest short-term payoff.

Target gaps from SIPp:

- obscure keyword variants
- edge-case branching semantics
- older scenario constructs that are valid in SIPp but not yet important for
  current `gossipper` users

Deliverables:

- compatibility backlog driven by real failing scenarios
- regression fixtures for each newly adopted XML edge case

## Explicit backlog checklist

These are concrete SIPp-side features that are not currently present in
`gossipper`, or are only partially covered:

- `-t ui`
- `-inf` + `-ip_field` multi-IP local address workflow
- `[server_ip]`
- `warning` action
- `lookup` action family
- richer periodic/cumulative statistics exports
- `play_pcap_video`
- `play_pcap_image`
- bidirectional RTP/SRTP checking (`rtpcheck`)
- wider media parity beyond current audio-first behavior
- fuller CLI parity with SIPp

## Development rule for future work

When choosing the next milestone, prefer:

1. features that unlock existing SIPp scenarios with minimal engine risk
2. features with strong testability and fixture coverage
3. features that improve practical automation value, not just checkbox parity
