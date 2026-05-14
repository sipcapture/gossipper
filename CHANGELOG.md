# Changelog

All notable changes to this project are documented in this file.

## [Unreleased]

### Added

- **`-media_srtp`**: optional SDES SRTP for `rtp_stream start` / `rtp_stream mic` when the remote SDP contains `a=crypto` with `inline:` (AES_CM_128_HMAC_SHA1_80 / AES_CM_128_HMAC_SHA1_32). Outbound RTP is encrypted and inbound RTP decrypted via `github.com/pion/srtp/v3`. DTLS-SRTP (`a=fingerprint` without SDES) is rejected with a clear error.
- **Summary / HTML report**: `media.rtp_recv_max_cumulative_lost` and `media.rtp_recv_interarrival_jitter_peak_ts` aggregate local RTP receive path quality (RFC 3550-style loss and interarrival jitter estimator peaks).

### Changed

- When remote SDP suggests SRTP and neither **`-media_reject_srtp`** nor **`-media_srtp`** is set, `rtp_stream start` / `mic` fail with an explicit hint to choose one of these flags (avoids silent cleartext toward an SRTP endpoint).

### Documentation

- `docs/srtp.md`, `docs/summary-json.md`, and HTML report template aligned with SRTP and new media counters.
