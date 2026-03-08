# RTP roadmap

`gossip` keeps RTP as a separate milestone layered on top of the SIP engine.

## Why it is separate

- SIP XML compatibility and dialog timing are the hard dependencies for a useful MVP.
- RTP support becomes much easier once SIP call lifecycle and media address extraction are stable.
- The media layer should remain intentionally narrow and avoid pulling in a full WebRTC stack.

## Current state

- `internal/media` already depends on `github.com/pion/rtp`.
- The package can build raw RTP packets, including a simple silent PCMU payload generator.
- `exec rtp_stream` now starts a real RTP sender, derives the remote audio endpoint from SDP, and supports `pause` / `resume`.
- WAV input is supported for PCM mono 8kHz and is packetized as PCMU or PCMA RTP, depending on payload settings.
- `rtp_stream` understands SIPp-style parameters: `file,loopcount,payloadtype,payloadparam`.
- The media session now emits periodic RTCP sender reports.
- `exec rtp_stream="echo"` starts a local RTP echo helper bound to the scenario media port.
- Incoming RTCP packets are parsed and exposed through basic session counters.
- Aggregated RTP/RTCP counters are now surfaced through the engine summary and JSON export.

## Planned milestones

1. Richer RTCP reporting surfaced into engine summary output
2. Optional SRTP support through a dedicated package
3. PCAP replay only if a real need appears

## Library choices

- `github.com/pion/rtp` for packet modeling and marshaling
- `github.com/pion/rtcp` later for control traffic
- `github.com/pion/srtp/v3` only when SRTP becomes a real requirement
