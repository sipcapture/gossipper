# RTP roadmap

`gossIpper` keeps RTP as a separate milestone layered on top of the SIP engine.

## Why it is separate

- SIP XML compatibility and dialog timing are the hard dependencies for a useful MVP.
- RTP support becomes much easier once SIP call lifecycle and media address extraction are stable.
- The media layer should remain intentionally narrow and avoid pulling in a full WebRTC stack.

## Current state

- `internal/media` already depends on `github.com/pion/rtp`.
- The package can build raw RTP packets, including a simple silent PCMU payload generator.
- `exec rtp_stream` now starts a real RTP sender, derives the remote audio endpoint from SDP, and supports `pause` / `resume` / `stop`.
- WAV input is supported for PCM mono 8kHz and is packetized as PCMU or PCMA RTP depending on payload settings.
- `rtp_stream` understands SIPp-style parameters: `file,loopcount,payloadtype,payloadparam`.
- `exec rtp_stream="echo"` starts a local RTP echo helper bound to the scenario media port.
- The media session now emits periodic RTCP sender reports.
- Incoming RTCP packets are parsed and exposed through basic session counters.
- Aggregated RTP/RTCP counters are now surfaced through the engine summary and JSON export.
- `exec play_pcap_audio="capture.pcap"` is implemented for audio PCAP replay with preserved inter-packet timing from the capture.
- Bundled fixtures and demo scenarios now cover both basic audio replay and DTMF-style RTP event replay through PCAP.

## Supported media scope today

- Audio RTP only
- SDP-driven remote audio endpoint discovery
- WAV-driven RTP generation for practical SIP call flows
- RTP echo for quick loopback-style validation
- Basic RTCP observability
- Audio PCAP replay for pre-recorded RTP streams and telephone-event style captures

## Known limits

- No SRTP
- No `play_pcap_video`
- No `play_pcap_image`
- No dedicated video media pipeline
- No advanced RTCP analytics in the summary yet
- No claim of full SIPp media parity

The current implementation is intentionally "useful first" rather than "complete
first": enough for practical SIP + audio testing, but still narrower than SIPp's
overall media surface.

## Planned milestones

1. Richer RTCP reporting surfaced into engine summary output
2. Optional SRTP support through a dedicated package
3. Decide whether video/image PCAP replay is worth implementing
4. Expand media reporting if real scenarios need jitter/loss-style visibility

## Library choices

- `github.com/pion/rtp` for packet modeling and marshaling
- `github.com/pion/rtcp` for control traffic
- `github.com/google/gopacket` and `github.com/google/gopacket/pcapgo` for audio PCAP replay input
- `github.com/pion/srtp/v3` only when SRTP becomes a real requirement
