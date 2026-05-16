# Media roadmap (RTP / SRTP)

Gossipper keeps RTP as a separate milestone layered on top of the SIP engine.

## Why it is separate

- SIP XML compatibility and dialog timing are the hard dependencies for a useful MVP.
- RTP support becomes much easier once SIP call lifecycle and media address extraction are stable.
- The media layer remains intentionally narrow: no full browser-style WebRTC stack, but enough hooks for **SDES SRTP**, **DTLS-SRTP**, and **ICE-lite-style** signalling common in SIP-to-WebRTC bridges.

## Current state (summary)

- **`internal/media`** uses **`github.com/pion/rtp`**, **`github.com/pion/rtcp`**, **`github.com/pion/srtp/v3`**, **`github.com/pion/dtls/v3`**, **`github.com/pion/stun/v3`**, **`github.com/pion/turn/v4`** where relevant.
- **`exec rtp_stream`** starts a real RTP sender (file, synthetic, or microphone), derives the remote endpoint from SDP, supports **`pause` / `resume` / `stop`**, and **`echo`** loopback.
- **SDP → remote media:** **`ParseAudioEndpoint`** / **`ParseMediaEndpoint`** read **`c=`** and **`m=audio` / `m=video` / `m=image`**. Bodies are normalized with **`EffectiveMediaSDPBody`**: **`Content-Type: application/trickle-ice+json`** is turned into SDP **`a=`** lines before parsing. **`a=group:BUNDLE`** / **`a=mid:`** are honored when placeholders require the bundle transport (first MID in the group). For WebRTC placeholders (**`0.0.0.0` / `::`**, **`m=`** port **9**, or missing **`m=`** for that type), they prefer **`a=candidate`** (UDP, RTP component 1) in the first matching media section, or from a **trickle-style fragment** (no **`m=<type>`** in the body). The chosen ICE **`typ`** is exposed on **`Endpoint.ICECandidateTyp`**. Hostnames in candidates are resolved with a short DNS lookup (prefer IPv4).
- **SRTP (`-media_srtp`):** **SDES** from **`a=crypto:`** inline, or **DTLS-SRTP** from **`a=fingerprint:`** (SHA-256 / SHA-384). DTLS role follows **`a=setup:`** in **`m=audio`**: **`active`** → gossipper runs **DTLS server**; otherwise **DTLS client**. Cleartext-only scenarios can use **`-media_reject_srtp`** to fail when SDP looks encrypted. See **`docs/srtp.md`**.
- **ICE (lightweight):** Per-call local **`a=ice-ufrag` / `a=ice-pwd`** (templates **`[ice_ufrag]`**, **`[ice_pwd]`**). Remote ICE from peer SDP; **STUN Binding** responses on the RTP/DTLS socket and outbound connectivity checks before DTLS when remote credentials are present. **`SetRemoteIceFromSDP`** merges credentials and **does not clear** them on candidate-only fragments.
- **Configure path:** If the last SIP body has **ICE attributes** but no SRTP hint, **`configureMediaSRTPForRTPStream`** refreshes ICE only and **does not** wipe an existing DTLS/SDES negotiation (supports trickle INFO after the main answer). The function uses **`EffectiveMediaSDPBody`** on the last message, so **JSON trickle** updates participate in the same path.
- **RTCP:** Sender reports; inbound parsing and counters; **RTCP-mux** when **`a=rtcp-mux`** is in remote SDP for SRTP paths.
- **QoS in engine summary (cleartext and SRTP):** the same **`internal/media`** **`Session.Snapshot()`** feeds **`internal/stats`**: **`MediaSummary`** exposes RR-derived fields (e.g. **`rtcp_reception_reports`**, **`rtcp_max_fraction_lost`**, **`rtcp_max_jitter_ts`**, min/avg jitter, **`rtp_recv_max_cumulative_lost`**, **`rtp_recv_interarrival_jitter_peak_ts`**, per-call inbound RTP minima). CLI **`-health_max_rtcp_fraction_lost`** / **`-health_max_rtcp_jitter_ts`** gate **`summary` / `health`**; HTML report and **JSON** stats include these. **Periodic `-trace_stat` CSV** still carries only RTP/RTCP **packet counts** (no QoS columns yet). See **`docs/qos-reporting.md`** for HEP mirroring.
- **PCAP replay** (`play_pcap_audio` / video / image), **WAV** send/receive, **microphone**, **RTP recording**, **rtpcheck**, engine stats — unchanged broad behaviour; see **`docs/rtp-in-scenarios.md`**.

## Supported media scope today

- Audio RTP as the primary path; pragmatic **m=video** / **m=image** for PCAP replay.
- **Cleartext RTP/AVP** and **SRTP** (SDES or DTLS) when **`-media_srtp`** is enabled and SDP matches.
- **Partial WebRTC parity:** **BUNDLE** (transport from bundled MID), **SDP + JSON trickle** bodies, and **TURN relay** (see **`-turn_server`** / **`-turn_user`** / **`-turn_pass`** / **`-turn_realm`** in **`docs/srtp.md`**) are supported for RTP addressing and media sockets. Still **not** a full browser stack: ICE **nomination**, rich **controlling/controlled** behaviour, and advanced TURN (TCP allocations, etc.) are not goals of the current layer.

## Known limits

- **TURN** is **UDP allocate** only, using the same **`host:port`** for STUN and TURN in the pion client; no dedicated **TCP TURN** or TURN-TLS mode in gossipper’s CLI yet.
- **Trickle** works from the **last SIP message** body: **SDP** (full or fragment) or **`application/trickle-ice+json`** after conversion to SDP lines. The scenario must surface that message before **`exec rtp_stream start`** / **`mic`** / PCAP actions, same as before.
- No dedicated **video encode/decode** pipeline; no full SIPp **`rtpcheck`** parity.
- **`docs/srtp.md`** is the detailed contract for SRTP/DTLS/ICE flags and limits.

## Closed (was on the roadmap)

1. **RTCP / QoS in engine summary** — **done** for both cleartext and **`-media_srtp`** paths: aggregation from inbound RR and local RTP loss/jitter estimates is in **`internal/stats.MediaSummary`**, surfaced in JSON, HTML, and health thresholds. **Not** extended to the legacy **periodic `-trace_stat` CSV** row format (still basic RTP/RTCP counters only).

## Planned milestones (media)

1. **Optional:** extend **periodic `-trace_stat` CSV** (and any other narrow text exports) with the same QoS columns as JSON, if operators need time-series without parsing JSON.
2. Richer **ICE/TURN** (TCP/TLS TURN, clearer error surfaces, optional channel semantics) if real deployments still hit gaps.
3. Video pipeline only if scenarios need codec-specific handling beyond PCAP replay.

## Library choices

- **`github.com/pion/rtp`**, **`github.com/pion/rtcp`**, **`github.com/pion/srtp/v3`**, **`github.com/pion/dtls/v3`**, **`github.com/pion/stun/v3`**, **`github.com/pion/turn/v4`**
- **`github.com/google/gopacket`** / **`pcapgo`** for PCAP replay
