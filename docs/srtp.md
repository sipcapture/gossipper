# SRTP and Gossipper

Gossipper today targets **plain RTP/RTCP** (UDP, unencrypted). SDP parsing for `rtp_stream` and `ParseAudioEndpoint` assumes `m=audio … RTP/AVP`.

## Failing fast on SRTP in SDP

The **`-media_reject_srtp`** flag makes `rtp_stream start` and `rtp_stream mic` fail when the last SIP message body indicates SRTP:

- an `m=` line with **RTP/SAVP** or **RTP/SAVPF**;
- **`a=crypto:`** or **`a=fingerprint:`** attributes (DTLS-SRTP).

This protects scenarios that expect cleartext RTP from silent failures or useless traffic toward an encrypted media path.

## Roadmap

1. Integrate **`github.com/pion/srtp/v3`**: key negotiation from SDP (SDES or DTLS), dedicated receive/send paths.
2. Symmetric **SAVPF** support in SDP generation for UAC scenarios (not required today).
3. Extend **HEP / QoS** for encrypted streams (metadata without raw payload).

A full **video pipeline** and richer **RTCP analytics** stay out of this scope; see `docs/media-roadmap.md`.
