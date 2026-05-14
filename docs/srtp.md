# SRTP and Gossipper

Gossipper targets **plain RTP/RTCP** by default (UDP, unencrypted). SDP parsing for `rtp_stream` / `mic` and `ParseAudioEndpoint` assumes cleartext **`RTP/AVP`** unless SRTP is explicitly enabled.

## Failing fast on SRTP in SDP

The **`-media_reject_srtp`** flag makes `rtp_stream start` and `rtp_stream mic` fail when the last SIP message body indicates SRTP:

- an `m=` line with **RTP/SAVP** or **RTP/SAVPF**;
- **`a=crypto:`** or **`a=fingerprint:`** attributes (DTLS-SRTP).

This protects scenarios that expect cleartext RTP from silent failures or useless traffic toward an encrypted media path.

## Media SRTP (`-media_srtp`)

When the peer SDP suggests SRTP and you pass **`-media_srtp`**, gossipper configures keys from the first **`m=audio`** section using either:

- **SDES:** **`a=crypto:`** with **`inline:`** base64 material (RFC 4568); suites **AES_CM_128_HMAC_SHA1_80** and **AES_CM_128_HMAC_SHA1_32** only; or
- **DTLS-SRTP:** **`a=fingerprint:sha-256`** or **`sha-384`** when no usable SDES inline line is present (see below).

Outbound RTP is encrypted and inbound RTP decrypted using **`github.com/pion/srtp/v3`**. When SRTP contexts are active, **RTCP Sender Reports** are sent as **SRTCP** (on the RTCP socket, or on the RTP socket when **`a=rtcp-mux`** was negotiated). Inbound RTCP is decrypted when possible (cleartext RTCP is still accepted if decryption fails).

**PCAP replay** encrypts cleartext RTP from the capture with the same SRTP send context before writing to UDP when `-media_srtp` negotiated keys are present.

### DTLS-SRTP (fingerprint)

If **`m=audio`** has **`a=fingerprint:`** (SHA-256 or SHA-384) and no usable **`a=crypto:`** inline line, gossipper runs **DTLS 1.2** on the RTP path (**`net.PacketConn`**: local UDP, or the **TURN relay** socket when ICE chose **`typ relay`**), demultiplexing DTLS vs RTP using the first-byte rule (RFC 7983 style). The peer certificate must match the SDP fingerprint. Keys are derived per RFC 5764 / **`EXTRACTOR-dtls_srtp`**. Negotiated profiles: **AES_CM_128_HMAC_SHA1_80** and **AES_CM_128_HMAC_SHA1_32** only.

**DTLS role:** By default gossipper is the **DTLS client** (typical UAC toward a passive WebRTC answer). If the first **`m=audio`** contains **`a=setup:active`**, the peer starts DTLS as client, so gossipper runs **`dtls.Server`** with client certificate required and the same fingerprint check on the peer leaf.

**WebRTC / ICE:** Each call generates local **`a=ice-ufrag` / `a=ice-pwd`** material; templates can insert them with **`[ice_ufrag]`** and **`[ice_pwd]`** in outbound SDP (browser-style offers). When the peer answer includes **`a=ice-ufrag` / `a=ice-pwd`**, gossipper answers **STUN Binding** checks on the same path used for DTLS/RTP (**local UDP** or **TURN relay `PacketConn`** when ICE chose **`relay`**) and sends outbound connectivity checks before the DTLS handshake. Non-STUN packets are still demuxed as DTLS vs RTP as before.

**Endpoints from ICE candidates:** If **`c=`** is **`0.0.0.0` / `::`**, **`m=audio` / `m=video` / `m=image`** uses discarded port **9**, or the body has **no matching `m=`** line but contains **`a=candidate:`** (trickle-style fragment for that media), **`ParseAudioEndpoint`** / **`ParseMediaEndpoint`** pick IP and port from the best **UDP / RTP (component 1)** candidate in the first section for that media type: **`typ`** preference **host → srflx → prflx → relay**, then higher ICE **priority**. The chosen candidate **`typ`** is also stored on **`Endpoint.ICECandidateTyp`** (e.g. **`relay`**) when the address came from ICE. For **`typ relay`**, the **connection-address** on the candidate line is the **TURN relay transport** (where peers send RTP); it is **not** replaced by **`raddr` / `rport`**. If the candidate **connection-address** is a **hostname**, gossipper resolves it (short DNS timeout, prefers the first **IPv4**).

**BUNDLE (RFC 8843):** After reading **`c=`** and **`m=<media>`**, **`ParseMediaEndpoint`** applies **`a=group:BUNDLE`** when the first **`m=`** for that media has an **`a=mid:`** listed in the bundle group and the endpoint still looks like a placeholder (unspecified **`c=`**, discarded port **9**, or zero port). The IP/port are then taken from the **first MID** in the bundle line (the offer’s “main” transport), matching typical WebRTC bundling.

**JSON trickle (RFC 8839):** If the SIP message has **`Content-Type: application/trickle-ice+json`** (or contains **`trickle-ice+json`**), the body is converted to an SDP-line fragment (**`ParseTrickleICEJSONToSDPFragment`**) before any SDP/ICE parsing. The same effective text is used in **`configureMediaSRTPForRTPStream`**, so trickle-only SIP bodies still refresh **ICE** / **DTLS fingerprint** / **rtcp-mux** when present in JSON.

**Trickle and SRTP state:** A SIP body that has **ICE lines** (`a=candidate`, **`a=ice-ufrag`**, **`a=ice-pwd`**, **`a=ice-options`**) but **no** SRTP media hint does **not** call **`ClearSDESSRTP`**: remote ICE is updated when credentials appear in that body, and existing **ufrag/pwd** are **preserved** if the fragment has only candidates (so a later **`rtp_stream start`** on the same session can use an updated endpoint without losing ICE passwords).

**TURN (relay paths):** When the chosen ICE candidate type is **`relay`**, gossipper opens a **TURN** allocation with **`github.com/pion/turn/v4`** on the local UDP socket and sends/receives RTP (and SRTP/DTLS when negotiated) on the **relayed `net.PacketConn`**. Configure the same server the browser uses:

- **`-turn_server`** — STUN/TURN **`host:port`** (UDP; passed as both STUN and TURN server to the client).
- **`-turn_user`** / **`-turn_pass`** — long-term credentials.
- **`-turn_realm`** — optional realm string (empty is allowed if your server does not require it).

If ICE selects **`relay`** but these are not set, media start fails with an explicit error. This is **not** a full browser ICE stack: there is no separate nomination API, and roles beyond **STUN Binding** pings toward the remote candidate remain minimal.

**Limits:** **`a=setup:actpass`** in the peer answer is treated like **passive** (gossipper stays **DTLS client**). **ICE nomination** and full **controlling / controlled** behaviour beyond basic connectivity checks are not implemented.

If the SDP hints SRTP but you pass **neither** `-media_reject_srtp` **nor** `-media_srtp`, `rtp_stream start` / `mic` / PCAP replay **fail** with a message that tells you to pick one of these modes.

## Roadmap

1. Richer SRTP profiles; tighter ICE/TURN behaviour (e.g. realm discovery, TCP TURN) if deployments need it.
2. Extend **HEP / QoS** for encrypted streams (metadata without raw payload).

A full **video pipeline** and richer **RTCP analytics** stay partially scoped; see `docs/media-roadmap.md`.
