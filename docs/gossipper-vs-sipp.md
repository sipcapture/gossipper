# gossIpper vs SIPp

`gossIpper` is a Go-based SIP traffic generator and scenario runner inspired by
[`SIPp`](https://github.com/SIPp/sipp). The project does not try to be a
line-by-line port. Instead, it focuses on a practical subset of SIPp features,
cleaner internals, and incremental compatibility for real-world SIP and media
testing.

## What `gossIpper` already does well

- Runs SIP XML scenarios with `send`, `recv`, `pause`, `nop`, `timewait`,
  `label`, and `init`
- Supports a broad SIPp-like XML action subset including `ereg`, `assign`,
  `assignstr`, `todouble`, `add`, `subtract`, `multiply`, `divide`, `strcmp`,
  `test`, `log`, `warning`, `lookup`, `jump`, `gettimeofday`, `urlencode`,
  `urldecode`, `verifyauth`, and `exec`
- Handles UDP, TCP, TLS, and server-side UDP aliases `s1` / `sn`
- Supports 3PCC-style `sendCmd` / `recvCmd` flows, including external peer
  communication between multiple `gossIpper` instances
- Handles out-of-call SIP exchanges such as stateless `OPTIONS`
- Supports Digest auth retries with `[authentication]`, inline auth params,
  CLI credentials from `-au` / `-ap`, and server-side auth validation through
  `verifyauth`
- Supports Homer/HEP export for SIP signaling via `-hep_addr`,
  `-hep_capture_id`, and `-hep_password`
- Provides media helpers for `rtp_stream`, RTP echo, RTCP counters, and audio
  PCAP replay through `play_pcap_audio`
- Exposes JSON summary output, named RTD metrics, execution counters,
  failure-class reporting, latency repartition buckets, periodic CSV stats,
  RTD CSV dumps, and message / error / action tracing

## Comparison summary

| Area | `gossIpper` | `SIPp` |
| --- | --- | --- |
| Implementation language | Go | C++ |
| Goal | Clean Go rewrite with incremental compatibility | Mature original reference implementation |
| XML execution model | Supported subset with growing parity | Broad and battle-tested |
| SIP transports | UDP, TCP, TLS, server aliases | Broad and mature |
| 3PCC command flows | Supported | Supported |
| Digest auth | Supported for common `401` / `407` flows plus `verifyauth` | Supported |
| HEP / Homer export | SIP signaling export over HEP3 | Mature capture/export ecosystem |
| RTP helpers | `rtp_stream`, echo, RTCP counters, audio PCAP replay | More mature media feature set |
| Reporting | JSON summary, RTD, failure classes, CSV stats, RTT dumps, tracing | Rich CLI stats and long-standing reporting model |
| Compatibility scope | Explicit and documented | De facto full reference behavior |

## Where `gossIpper` is intentionally different

### 1. Smaller compatibility surface

`gossIpper` only implements the SIPp subset that is currently useful and tested in
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
- More complete media coverage beyond the currently implemented `gossIpper`
  subset
- Longer production history and more community examples
- Better parity for obscure or legacy scenario semantics

If you need exact behavior from a complex existing SIPp scenario, SIPp is still
the safer default. If you want a simpler Go-native codebase with enough SIPp
surface for modern integration tests and incremental extension, `gossIpper` is the
better fit.

## Current practical position

Today, `gossIpper` is a strong fit for:

- targeted SIP regression scenarios
- 3PCC and command-orchestrated test flows
- auth challenge / response coverage
- Homer-compatible SIP mirroring over HEP3
- lightweight media and RTP validation
- CI-friendly scenario execution with structured JSON/CSV output

It is not yet a drop-in replacement for every SIPp installation or every SIPp
scenario ever written.

## Related docs

- `docs/compatibility.md`: exact support matrix
- `docs/architecture.md`: package-level design
- `docs/media-roadmap.md`: media-related direction and gaps
- `README.md`: quick-start examples and runnable demos
