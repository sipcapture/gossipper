# Gossipper Documentation

Gossipper is an open-source **SIP and WebRTC load-testing platform** for labs, CI,
and soak runs: SIPp-compatible XML scenarios, RTP/SRTP and PCAP media, long-lived
management mode with HTTP API and Control UI, multi-engine hybrid rigs, and
Homer/HEP observability.

## Quick links

| Topic | Guide |
|-------|--------|
| First run | [CLI reference](cli.md) |
| vs SIPp | [Gossipper vs SIPp](gossipper-vs-sipp.md) |
| XML & keywords | [Compatibility matrix](compatibility.md) |
| Long-run / HTTP API | [SIP stress style load testing](sipstress-style-load-testing.md) · [UI mode](ui-mode.md) |
| Media | [RTP in scenarios](rtp-in-scenarios.md) · [SRTP](srtp.md) |
| Stats & traces | [Statistics mapping](statistics-mapping.md) · [Trace schema](trace-schema-contract.md) |

## Repository

- Source: [github.com/sipcapture/gossipper](https://github.com/sipcapture/gossipper) (branch `main`)
- Releases: [GitHub Releases](https://github.com/sipcapture/gossipper/releases)

## Modes

```
┌──────────────┐   ┌──────────────┐   ┌─────────────────────┐
│ gossipper    │   │ gossipper    │   │ gossipper server    │
│ sipp [flags] │   │ tui / cli    │   │ -config … + HTTP API│
│ (load gen)   │   │ (interactive)│   │ (management UAS)    │
└──────────────┘   └──────────────┘   └─────────────────────┘
```

See the sidebar for the full documentation index.
