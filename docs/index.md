# Gossipper Documentation

Gossipper is a Go rewrite of SIPp focused on SIP signaling load generation,
incremental XML scenario compatibility, media (RTP/RTCP/SRTP), and a cleaner engine architecture.

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
