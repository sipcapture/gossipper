# gossipper vs SIPp

`gossipper` is a Go-based SIP traffic generator and scenario runner inspired by
[`SIPp`](https://github.com/SIPp/sipp). The project does not try to be a
line-by-line port. Instead, it focuses on a practical subset of SIPp features,
cleaner internals, and incremental compatibility for real-world SIP and media
testing.

## What `gossipper` already does well

- Runs SIP XML scenarios with `send`, `recv`, `pause`, `nop`, `timewait`,
  `label`, and `init`
- Supports SIPp-like actions such as `ereg`, `assignstr`, `test`, `log`, and
  `exec`
- Handles UDP, TCP, TLS, and server-side UDP aliases `s1` / `sn`
- Supports 3PCC-style `sendCmd` / `recvCmd` flows, including external peer
  communication between multiple `gossipper` instances
- Handles out-of-call SIP exchanges such as stateless `OPTIONS`
- Supports Digest auth retries with `[authentication]` plus SIPp-style
  credentials from `-au` / `-ap`
- Provides media helpers for `rtp_stream`, RTP echo, RTCP counters, and audio
  PCAP replay through `play_pcap_audio`
- Exposes JSON summary output, named RTD metrics, execution counters, and
  message / error / action tracing

## Comparison summary

| Area | `gossipper` | `SIPp` |
| --- | --- | --- |
| Implementation language | Go | C++ |
| Goal | Clean Go rewrite with incremental compatibility | Mature original reference implementation |
| XML execution model | Supported subset with growing parity | Broad and battle-tested |
| SIP transports | UDP, TCP, TLS, server aliases | Broad and mature |
| 3PCC command flows | Supported | Supported |
| Digest auth | Supported for common `401` / `407` flows | Supported |
| RTP helpers | `rtp_stream`, echo, RTCP counters, audio PCAP replay | More mature media feature set |
| Reporting | JSON summary, RTD, counters, tracing | Rich CLI stats and long-standing reporting model |
| Compatibility scope | Explicit and documented | De facto full reference behavior |

## Where `gossipper` is intentionally different

### 1. Smaller compatibility surface

`gossipper` only implements the SIPp subset that is currently useful and tested in
this repository. The idea is to grow compatibility intentionally instead of
pretending to support every legacy corner case from day one.

### 2. Cleaner engine boundaries

The Go codebase is structured around explicit packages such as `engine`,
`scenario`, `transport`, `media`, `stats`, and `template`. This makes it easier
to test individual pieces, evolve features in isolation, and reason about the
runtime.

### 3. Automation-friendly outputs

The project leans toward machine-readable outputs such as JSON summaries and
dedicated trace files. This is useful for CI pipelines, scripted regression
tests, and reproducible compatibility work.

## Where `SIPp` is still ahead

- Wider XML and CLI compatibility
- More complete media coverage beyond the currently implemented `gossipper`
  subset
- Longer production history and more community examples
- Better parity for obscure or legacy scenario semantics

If you need exact behavior from a complex existing SIPp scenario, SIPp is still
the safer default. If you want a simpler Go-native codebase with enough SIPp
surface for modern integration tests and incremental extension, `gossipper` is the
better fit.

## Current practical position

Today, `gossipper` is a strong fit for:

- targeted SIP regression scenarios
- 3PCC and command-orchestrated test flows
- auth challenge / response coverage
- lightweight media and RTP validation
- CI-friendly scenario execution with structured output

It is not yet a drop-in replacement for every SIPp installation or every SIPp
scenario ever written.

## Related docs

- `docs/compatibility.md`: exact support matrix
- `docs/architecture.md`: package-level design
- `docs/media-roadmap.md`: media-related direction and gaps
- `README.md`: quick-start examples and runnable demos
