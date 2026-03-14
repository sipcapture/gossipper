# gossIpper milestones toward broader SIPp parity

This file tracks the next practical development milestones for `gossIpper`,
focusing on features that exist in `SIPp` and are still missing or only
partially covered here.

It is intentionally prioritized. The goal is not to clone every historical
SIPp feature at once, but to close the highest-value parity gaps in a controlled
order.

## Baseline already implemented

`gossIpper` already covers a useful subset:

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

Status: completed

Why first:

- `gossIpper` already has JSON summaries and basic RTD/counter support
- SIPp still offers richer built-in reporting flows that are useful in CI and
  long-running load tests

Target gaps from SIPp:

This milestone is now complete for the currently intended MVP scope.

Completed so far in this milestone:

- `-trace_stat` style periodic CSV snapshots via sibling `*_stats.log` output
- `-trace_rtt` style RTD dump files via sibling `*_rtt.log` CSV output
- richer cumulative vs periodic counters in `-trace_stat` via interval delta columns
- broader call-failure classification in exported stats via summary and `-trace_stat` failure-class counters
- SIPp-like latency / repartition style reporting via latency stddev fields and bucketed distributions in summary JSON

Deliverables:

- documented field mapping between current `gossIpper` JSON and SIPp-style counters
  completed in `docs/statistics-mapping.md`

## Milestone 2: XML action and scenario parity

Priority: high

Status: completed

Completed so far in this milestone:

- `warning` action, mapped to the existing error trace flow with parser and engine coverage
- `lookup` action for CSV-backed injection data, plus variable-driven `[fieldN ... line=$var]`
- `strcmp` action plus broader `test` compare semantics with `variable2` support
- explicit parser/runtime failures for unsupported XML actions and scenario keywords
- arithmetic/action helpers: `assign`, `todouble`, `add`, `subtract`, `multiply`, `divide`
- control/helper actions: `jump`, `gettimeofday`, `urlencode`, `urldecode`, `verifyauth`
- broader scenario keyword support including `[server_ip]`, `[last_Request_URI]`, `[users]`, and `[userid]`

Target gaps from SIPp:

This milestone is now complete for the intended XML/helper scope.

Explicitly deferred beyond Milestone 2:

- advanced `sample` distributions beyond the current deterministic integer range subset, and `insert` / `replace` action families
- `setdest` because it overlaps transport/addressing work in Milestone 3
- `play_pcap_video` because it overlaps broader media parity in Milestone 4
- keyword families such as `[routes]`, `[dynamic_id]`, `[clock_tick]`, `[sipp_version]`, `[tdmmap]`, and `[fill]`
- full SIPp injection/CLI parity beyond the current CSV helper subset

Deliverables:

- action compatibility matrix extended beyond current subset
- tests for newly added actions
- explicit "supported / partial / deferred" notes for each action family

## Milestone 3: Transport and addressing parity

Priority: medium-high

Status: completed (pragmatic)

Completed so far in this milestone:

- `-t ui` initial client+server MVP with one shared UDP socket per configured source/listener IP
- `-inf` + `-ip_field` CLI workflow for per-call local IP selection in `ui` mode
- per-call source IP is now propagated into render context so `[local_ip]` and `[server_ip]` reflect selected `ui` source IP
- client-side regression coverage for `ui` source-IP socket behavior and keyword rendering semantics
- server-side multi-IP `ui` listener binding with per-session socket affinity and listener-based `[server_ip]` semantics
- hardening coverage for `ui` bind failures (conflicting port and malformed bind IP), with explicit runtime errors that include the failing `ip:port`
- pragmatic `setdest` support in parser/runtime with positive and negative coverage
- `[transport]` keyword parity for `ui` (`UDP`) with unit coverage
- deterministic `-inf` / `-ip_field` contract hardened with edge-case tests (empty CSV, empty cell, malformed CSV, out-of-range, IPv6)
- TUI launcher parity for `ui` via explicit `inf` / `ip_field` inputs and validation
- docs/compatibility alignment for M3 pragmatic scope and deferred tail

Target gaps from SIPp (deferred beyond pragmatic M3 close):

- broader SIPp parity for `-t ui` beyond current deterministic client+server workflow
- broader `setdest` semantics for non-UDP and per-call socket transports
- broader transport-related parity after the current `u1` / `un` / `ui` / `s1` / `sn` / `t1` / `tn` / `l1` / `ln` coverage

Deliverables:

- multi-IP transport design
- CSV-backed per-call IP selection
- client and server tests for `ui` behavior

## Milestone 4: Media parity beyond current audio-first scope

Priority: medium-high

Status: completed (pragmatic)

Completed so far in this milestone:

- pragmatic `play_pcap_video` support via `exec play_pcap_video="..."` with SDP `m=video` endpoint discovery
- pragmatic `play_pcap_image` support via `exec play_pcap_image="..."` with SDP `m=image` endpoint discovery
- pragmatic `exec rtpcheck="..."` support for RTP activity validation with configurable packet threshold, timeout, and direction mode (`any|send|recv|both`; legacy `bidirectional` alias)
- parser and engine regression coverage for new PCAP media action attributes and runtime replay behavior

Target gaps from SIPp (deferred beyond pragmatic M4 close):

- broader generic PCAP replay coverage like SIPp's "play any RTP stream" positioning
- full SIPp bidirectional RTP / SRTP checking (`rtpcheck`) parity beyond current pragmatic RTP-only activity checks
- wider codec/file handling beyond the currently practical `gossIpper` subset

Deliverables:

- media endpoint discovery for non-audio streams where appropriate
- explicit video/image replay decisions instead of leaving them implicit
- a separate decision point for SRTP and full SIPp `rtpcheck` parity

## Milestone 5: CLI parity and operational UX

Priority: medium

Status: completed

Completed so far in this milestone:

- built-in interactive terminal UI via `gossIpper tui` and `-interactive`
- launch-time parameter selection for mode/profile, transport, remote/local addressing, CPS, concurrency, auth, and trace toggles
- SIPp-style live runtime dashboard with total/active/success/failed calls, average call duration, invite RTT, timeout and cancellation counters
- operator controls for increasing or decreasing load during the run, plus pause/resume and graceful client-side stop
- `-fd` parity for `-trace_stat` snapshot frequency control (seconds)
- `-rtt_freq` parity for `-trace_rtt` flush cadence control (completed calls)
- stable `-trace_stat` / `-trace_rtt` CSV schema contract with explicit docs and test guardrails
- `-trace_counts` periodic per-scenario SIP command counter CSV export (`sent` / `recv` / `unexp`)
- `-base_cseq` parity for explicit `[cseq]` base value control
- `-rp` parity for SIPp-style call rate period control (`-r n -rp m`)
- parser migration policy for trace CSV: single canonical contract, no runtime legacy alias columns
- `-trace_counts` schema stabilization decision for M5: keep `sent` / `recv` / `unexp`; richer per-message detail deferred
- non-interactive runtime screen CSV snapshots via `-trace_screen` with optional `-screen_file`
- `-trace_screen` enriched with high-signal triage fields (`success_ratio`, `failure_timeout`, `failure_unexpected_sip`)
- on-demand runtime screen dump trigger via `SIGUSR1` for live troubleshooting
- `-trace_screen` now includes interval throughput fields (`interval_ms`, `interval_calls_per_second`) for better non-interactive run triage
- selected non-interactive runtime reporting subset in M5 is now treated as stable/supported via `-trace_screen` contract
- `-timeout_global` parity for deterministic global run timeout control
- `-rate_scale` parity for SIPp-style interactive rate step scaling (used by `+/-/*//` runtime controls)
- runtime rate ramp controls via `-rate_increase`, `-rate_interval`, and `-rate_max`
- initial `-max_socket` parity for per-call client transports (`un`, `tn`, `ln`)
- partial reconnect control parity via `-max_reconnect` and `-reconnect_sleep` for shared client TCP/TLS (`t1`, `l1`)
- `-reconnect_close` surfaced with initial close-on-reconnect behavior for shared client TCP/TLS (`t1`, `l1`)
- `-infindex` parity for indexed CSV injection lookup acceleration (`-infindex <file> <field>`)
- parser guardrails for transport-scoped runtime knobs: `-max_socket` restricted to `un`/`tn`/`ln`, reconnect knobs restricted to `t1`/`l1`
- usability parity for trace cadence flags: explicit `-fd` auto-enables `-trace_stat`, explicit `-rtt_freq` auto-enables `-trace_rtt`

Target gaps from SIPp:

This milestone is complete for the selected high-value CLI and operational UX subset.
Remaining transport/media/full-tail parity stays in Milestones 3/4/6.

Examples of likely candidates:

- additional trace/export flags
- parity for more rate / scheduling knobs
- screen/report behaviors that matter in automation or troubleshooting

Deliverables:

- documented CLI gap list
- grouped implementation by "high-value in automation" vs "legacy / optional"

## Milestone 6: Close the long tail of XML and keyword semantics

Priority: medium-low

Status: completed (pragmatic)

This milestone is intentionally later because it carries the highest blast
radius and the lowest short-term payoff.

Target gaps from SIPp:

- obscure keyword variants
- edge-case branching semantics
- older scenario constructs that are valid in SIPp but not yet important for
  current `gossIpper` users

Deliverables:

- compatibility backlog driven by real failing scenarios
- regression fixtures for each newly adopted XML edge case

Completed so far in this milestone:

- pragmatic keyword support for `[sipp_version]`, `[clock_tick]`, and `[dynamic_id]` in template rendering
- pragmatic `[fill]` keyword support for variable-driven filler generation with optional custom seed text
- pragmatic `[routes]` support backed by `recv rrs="true"` Record-Route capture and reverse-order Route replay
- pragmatic `_unexp.main` fallback flow for unexpected SIP with `$_unexp.retaddr` handoff
- pragmatic optional-`recv` short-circuit on first unexpected SIP with pending-stash handoff to following receives
- fixture coverage for mixed optional-`recv` branching with `_unexp.main` priority and stable `$_unexp.retaddr` handoff
- fixed pending-queue starvation edge-case where optional-mismatch leftovers could block subsequent mandatory `recv` matching
- regression fixture for multiple sequential pending mismatches (`183`/`484`) proving mandatory `recv 486` still resolves deterministically
- pragmatic `sample` action subset for deterministic integer range sampling (`min/max/step/seed`) with `assign_to`
- pragmatic `insert` / `replace` action subset for deterministic per-cell CSV in-memory mutation used by `[fieldN ...]` rendering
- unit coverage for strict and non-strict rendering paths, including arithmetic offsets for tick/id helpers
- compatibility matrix updates reflecting M6 P0 keyword status

Target gaps from SIPp (deferred beyond pragmatic M6 close):

- advanced `insert` / `replace` semantics beyond current deterministic per-cell CSV mutation subset
- advanced `sample` distributions and non-deterministic statistical profiles beyond current constrained subset
- older niche XML/keyword constructs (for example `[tdmmap]`) that are low-value for current automation flows

Execution plan (phased pragmatic close):

Phase 1 - backlog freeze (completed):

- define concrete `P0/P1/P2` scope for M6 with acceptance criteria and explicit non-goals
- keep M6 focused on XML/keyword semantics only; no transport/media redesign in this milestone
- gate every item by testability (unit + regression scenario fixture)

Phase 2 - P0 keyword parity (low-risk, high unlock):

- implement `[sipp_version]` keyword rendering (static build/version string path)
- implement `[clock_tick]` keyword rendering (monotonic runtime tick helper)
- implement `[dynamic_id]` keyword rendering (message-level incrementing id with deterministic tests)
- implement pragmatic `[fill]` keyword family (`variable=` length, optional `text=` seed) for message templating use-cases

Acceptance criteria for Phase 2:

- parser/render unit tests for each keyword, including malformed parameter negative cases
- at least one scenario-level regression test per keyword family
- `docs/compatibility.md` updated from `deferred` to `partial` / `supported` with scope notes

Phase 3 - P1 keyword + XML edge semantics:

- implement pragmatic `[routes]` replay semantics for flows using `rrs="true"` on receive commands
- harden edge-case branching behavior around label/index jumps for scenario parity cases seen in failing fixtures
- add fixture coverage for mixed optional receives + branching paths that currently diverge from SIPp behavior

Acceptance criteria for Phase 3:

- route capture/replay covered by UAC/UAS regression scenarios
- branching semantics validated by deterministic fixture outcomes, not only unit tests
- no regressions in existing M1-M5 suites (`go test ./...`)

Phase 4 - P2 deferred actions (only if still needed after fixtures):

- evaluate `sample` action in a constrained deterministic subset first
- evaluate in-memory injection mutation (`insert` / `replace`) with strict bounds and explicit limitations

Acceptance criteria for Phase 4:

- features stay explicitly marked `partial` unless full SIPp semantics are proven
- complexity/risk review documented before enabling by default

Planned P0 shortlist for next implementation cycle:

1. `[sipp_version]` + `[clock_tick]` + `[dynamic_id]` renderer support and tests
2. pragmatic `[fill]` keyword support and tests
3. documentation sync (`compatibility` + milestone status notes)

## Explicit backlog checklist

These are concrete SIPp-side features that are not currently present in
`gossIpper`, or are only partially covered:

- advanced `-t ui` parity and broader multi-IP transport semantics
- advanced `-inf` + `-ip_field` compatibility behavior
- broader `setdest` coverage beyond current pragmatic UDP scope
- full SIPp media parity beyond pragmatic `play_pcap_video` / `play_pcap_image` support
- bidirectional RTP/SRTP checking (`rtpcheck`) beyond current pragmatic RTP activity validation
- wider media parity beyond current audio-first behavior
- fuller CLI parity with SIPp beyond current pragmatic guardrails and trace/rate operational subset

## Development rule for future work

When choosing the next milestone, prefer:

1. features that unlock existing SIPp scenarios with minimal engine risk
2. features with strong testability and fixture coverage
3. features that improve practical automation value, not just checkbox parity
