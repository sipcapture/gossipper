# SRTP and Gossipper

Gossipper targets **plain RTP/RTCP** by default (UDP, unencrypted). SDP parsing for `rtp_stream` / `mic` and `ParseAudioEndpoint` assumes cleartext **`RTP/AVP`** unless SRTP is explicitly enabled.

## Failing fast on SRTP in SDP

The **`-media_reject_srtp`** flag makes `rtp_stream start` and `rtp_stream mic` fail when the last SIP message body indicates SRTP:

- an `m=` line with **RTP/SAVP** or **RTP/SAVPF**;
- **`a=crypto:`** or **`a=fingerprint:`** attributes (DTLS-SRTP).

This protects scenarios that expect cleartext RTP from silent failures or useless traffic toward an encrypted media path.

## SDES SRTP (`-media_srtp`)

When the peer SDP suggests SRTP and you pass **`-media_srtp`**, gossipper negotiates **SDES** from the first **`m=audio`** section:

- **`a=crypto:`** with **`inline:`** base64 material (RFC 4568);
- suites **AES_CM_128_HMAC_SHA1_80** and **AES_CM_128_HMAC_SHA1_32** only.

Outbound RTP is encrypted and inbound RTP decrypted using **`github.com/pion/srtp/v3`**. RTCP is still sent as plain RTCP on the paired port (SRTCP is not implemented).

If the SDP hints SRTP but you pass **neither** `-media_reject_srtp` **nor** `-media_srtp`, `rtp_stream start` / `mic` **fail** with a message that tells you to pick one of these modes.

**DTLS-SRTP** (`a=fingerprint` without a usable **`a=crypto:`** in **`m=audio`**) is **not** supported yet; the error explains that SDES keys are required today.

## Roadmap

1. **SRTCP** and richer key negotiation (MKI, lifetime, DTLS-SRTP) as follow-ups.
2. Symmetric **SAVPF** support in SDP generation for UAC scenarios (not required today).
3. Extend **HEP / QoS** for encrypted streams (metadata without raw payload).

A full **video pipeline** and richer **RTCP analytics** stay partially scoped; see `docs/media-roadmap.md`.
