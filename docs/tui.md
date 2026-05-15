# TUI Guide

`Gossipper` now includes an interactive terminal UI that lets an operator:

- choose launch parameters without building a long CLI command
- start client or server scenarios from built-in or XML-based profiles
- watch live SIPp-like runtime statistics
- change target CPS during a running client-side load test

## Launch

Run the TUI with either command (installed binary: **`gossipper tui`** / **`gossipper -interactive`**):

```bash
go run ./cmd/gossip tui
```

```bash
go run ./cmd/gossip -interactive
```

Do **not** use **`gossipper sipp tui`** — the **`sipp`** prefix is only for SIPp-style
scenario flags. Use **`gossipper tui`** (see [`cli.md`](cli.md)).

## Launch Screen

The first screen is the interactive launcher. It exposes the most useful runtime
fields from the current CLI:

- `Mode`
- `Profile`
- `Transport`
- `Remote addr`
- `Local IP` / `Local port`
- `Service`
- `CPS`
- `Calls`
- `Max concurrent`
- `Users`
- auth fields
- `Custom XML`
- `HEP addr`
- trace toggles such as `trace_stat`, `trace_rtt`, `trace_msg`, and `trace_err`

```text
Gossipper interactive launcher

┌──────────────────────────── Gossipper TUI ────────────────────────────┐
│ Mode:            client                                              │
│ Profile:         builtin-uac (uac)                                   │
│ Transport:       u1                                                  │
│ Remote addr:     127.0.0.1:5060                                      │
│ Local IP:        0.0.0.0                                             │
│ Local port:      0                                                   │
│ Service:         service                                             │
│ CPS:             10                                                  │
│ Calls:           100                                                 │
│ Max concurrent:  10                                                  │
│ Users:           1                                                   │
│ Auth user:       alice                                               │
│ Auth pass:       secret                                              │
│ Custom XML:                                                          │
│ HEP addr:        127.0.0.1:9060                                      │
│ [x] trace_stat   [ ] trace_rtt   [x] trace_msg   [ ] trace_err       │
├───────────────────────────────────────────────────────────────────────┤
│                           [Start]   [Quit]                           │
└───────────────────────────────────────────────────────────────────────┘
```

## Runtime Screen

After pressing `Start`, the TUI switches to the runtime dashboard. This screen
is designed to mirror the operator feel of SIPp and currently shows:

- selected profile, mode, and transport
- target CPS
- measured average CPS
- measured last-interval CPS
- total, active, successful, and failed calls
- average call duration
- average invite RTT
- failure counters including `timeout` and `cancelled`
- transport / parse / scenario failure classes
- RTP / RTCP counters
- current run status text

```text
┌──────────────────────────── Runtime Stats ────────────────────────────┐
│ Profile: builtin-uac                                                 │
│ Mode:    client                                                      │
│ Transport: u1                                                        │
│ State:   running                                                     │
│                                                                       │
│ Target CPS:          15.00                                           │
│ Measured CPS(avg):   14.72                                           │
│ Measured CPS(1s):    15.00                                           │
│                                                                       │
│ Calls:    total=320 active=12 success=305 failed=3                   │
│ Latency:  avg_call=1.24s avg_invite=82ms                             │
│ Failures: timeout=1 cancelled=1 unexpected=0 transport=1 parse=0     │
│           scenario=0                                                 │
│ Media:    rtp_sent=6400 rtp_recv=6100 rtcp_sr=120 rtcp_rr=118        │
│           rtcp_in=118                                                │
│                                                                       │
│ Status: Target CPS set to 15.00                                      │
└───────────────────────────────────────────────────────────────────────┘
Keys: +/- change CPS, p pause/resume, q stop run, Esc back after finish
```

## Keyboard Controls

During a client-side run, the following hotkeys are supported:

- `+` / `=`: increase target CPS
- `-`: decrease target CPS
- `p`: pause or resume scheduling of new calls
- `q`: stop scheduling new calls and let active calls drain
- `Esc`: return to the launcher after the run has finished

For server-mode runs, the dashboard remains useful for live stats, but dynamic
CPS control is naturally only meaningful for client-side traffic generation.

## Notes

- `Profile` is currently an operator preset layer on top of existing built-in
  scenarios and repository XML fixtures.
- The runtime dashboard is driven by the same `stats.Snapshot()` data that the
  non-interactive CLI uses for summary output.
- Dynamic CPS changes are applied without restarting `Gossipper`.
