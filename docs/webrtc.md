# WebRTC media bridge (experimental)

Status: experimental — Phase 4.2 UAC/UAS offer/answer paths are wired behind opt-in flags.

The package `internal/webrtc` wraps [`pion/webrtc/v4`](https://github.com/pion/webrtc)
in a SIP-friendly `Bridge` type. A bridge owns one PeerConnection plus one
outbound audio track and lets the SIP scenario:

  - generate a WebRTC SDP offer (`Bridge.CreateOffer`),
  - or accept an inbound offer and return a complete answer
    (`Bridge.Answer`),
  - push raw G.711 PCMA / PCMU payloads to the peer (`Bridge.WritePCMA`),
  - register a callback for inbound RTP audio payloads (`Bridge.OnPCMA`).

## What is in place

- DTLS-SRTP, ICE candidate gathering and the standard pion media engine
  (PCMU/PCMA registered).
- ICE servers (STUN / TURN) provided per bridge via `Options.ICEServers`.
- **TURN auth modes** (`internal/webrtc/ice.go`):
  - **inline URL** — `turn:alice:secret@turn.example.com:3478?transport=udp`
  - **static** — profile `ice_username` / `ice_credential` applied to every TURN URL
  - **coturn REST** — profile `ice_auth_secret` (+ optional identity in `ice_username`, TTL in `ice_auth_ttl_sec`) mints ephemeral credentials per Bridge
- ICE diagnostics in call records: `ice_gathering`, `ice_connection`, `turn_auth`, selected candidate pair IDs.
- Standalone unit test (`internal/webrtc/bridge_test.go`) that wires two
  bridges back to back, performs the offer/answer dance, and asserts that
  PCMA samples written on one side arrive on the other.
- Transport selector in the admin console (`web/control-ui` →
  `TransportListEditor`) advertises `webrtc` as an experimental option.
- ICE servers / TURN credentials / codec preference are configurable per
  transport row in the Server / Client profile form and are threaded all the
  way to `engine.Config` (`WebRTCICEServers`, `WebRTCICEUsername`,
  `WebRTCICECredential`, `WebRTCPrefersPCMA`).
- `Engine.NewWebRTCBridge()` constructs a Bridge from the engine's runtime
  WebRTC settings; a sanity-check test (`webrtc_bridge_test.go`) keeps the
  plumbing honest.
- `webrtc` rows in a profile are no longer mistaken for SIP transports — the
  supervisor splits them out and the SIP listener still binds UDP/TCP/TLS/WS
  while ICE settings flow through `WebRTC*` fields
  (`internal/supervisor/profile_cfg.go::splitWebRTC`).

## What is in place (Phase 4.2)

- **Opt-in scenarios**: `<scenario webrtc="true">` spins up a per-call `internal/webrtc.Bridge` instead of `media.Session`.
- **Profile auto-enable**: an enabled `webrtc` transport row on the run profile sets `WebRTCMedia=true` — the bridge is used even without `webrtc="true"` on the scenario XML.
- **UAS answer path**: before `<send>` steps that contain `[webrtc_answer]`, the engine generates a WebRTC SDP answer from the last received INVITE offer and injects it via template keyword.
- **UAC offer path**: before `<send>` steps that contain `[webrtc_offer]`, the engine creates a WebRTC SDP offer via `Bridge.CreateOffer` and injects it via template keyword.
- **Accept answer (UAC)**: after receiving 200/183 with SDP body, the engine calls `Bridge.AcceptAnswer` when an offer was created earlier in the call.
- **Call records**: `call_records.jsonl` includes a `webrtc` diagnostics block (codec, ICE state, RTP counters, offer/answer flags). Jobs auto-write this file under the artifacts dir; the Control UI job monitor reads it via `GET .../artifacts/call_records`.
- **WAV capture**: `rtp_record` and profile `record_wav` auto-capture work on the WebRTC bridge path (G.711 decode to WAV).
- **`[media_transport]`** renders `webrtc` when the bridge is active.
- **`[contact_transport]`** renders `WSS` when the bridge is active — use in Contact headers (`transport=[contact_transport]`). Keep `[transport]` / `[sip_transport]` for Via (UDP/TCP/TLS signaling).
- **RTP/RTCP stats**: pion `GetStats()` feeds `media.Stats` and call-record `webrtc` block (`fraction_lost`, `jitter_ms`, `rtp_packets_lost`).
- **`rtp_stream` synthetic** over WebRTC sends silence frames through `Bridge.WritePCMA` / `WritePCMU`.
- **`Options.ICEGatherTimeout`** on the bridge (default 5s) when `ICETrickleFullGather` is enabled.
- **Trickle ICE (default)**: local offer/answer returns after first host candidates (~400ms grace); remote trickle SDP / `application/trickle-ice+json` ingested on SIP recv.
- **TURN REST refresh**: coturn ephemeral credentials refreshed mid-call before expiry (`turn_refresh_count` in diagnostics).
- **Control UI**: job monitor shows a WebRTC diagnostics strip (ICE servers from profile + recent worker log lines).
- PCAP replay, mic, DTMF, and classic RTP/RTCP stats return `media.ErrUnsupportedOverWebRTC` on the WebRTC path.

Example UAS snippet:

```xml
<scenario name="webrtc_uas" webrtc="true">
  <recv request="INVITE"/>
  <send retrans="500">
    <![CDATA[SIP/2.0 200 OK
Content-Type: application/sdp
Content-Length: [len]

[webrtc_answer]]]>
  </send>
  ...
</scenario>
```

Example UAC snippet (built-in `webrtc_uac`, or profile with enabled `webrtc` transport):

```xml
<scenario name="webrtc_uac">
  <send>
    <![CDATA[INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
...
Content-Type: application/sdp
Content-Length: [len]

[webrtc_offer]]]>
  </send>
  <recv response="200,183"/>
  ...
</scenario>
```

## ICE / TURN configuration

Configure ICE on the profile's **webrtc** transport row (Control UI → Server/Client profile → transports).

| Field | Purpose |
|---|---|
| `ice_servers` | One STUN/TURN URL per line |
| `ice_username` / `ice_credential` | Static TURN credentials (all TURN URLs) |
| `ice_auth_secret` | coturn `--use-auth-secret` shared key |
| `ice_auth_ttl_sec` | REST credential lifetime (default 86400) |

**URL formats**

```
stun:stun.l.google.com:19302
turn:turn.example.com:3478?transport=udp
turn:alice:secret@turn.example.com:3478?transport=udp
turns:turn.example.com:5349?transport=tcp
```

**coturn REST** (per-call ephemeral credentials):

1. In `/etc/turnserver.conf`: `use-auth-secret`, `static-auth-secret=<same as ice_auth_secret>`.
2. On the profile: set `ice_auth_secret`; optionally set `ice_username` to a stable identity (default `gossipper`).
3. Each WebRTC call mints `username=<expiry>:<identity>` and `password=base64(hmac-sha1(secret, username))`.

STUN URLs never receive credentials. TURN URLs pick inline URL creds first, then REST, then static profile creds.

**Trickle ICE**

- Default: bridge returns SDP quickly with initial host candidates and `a=ice-options:trickle`; STUN/TURN candidates continue in background.
- Remote updates: SIP bodies with candidate-only SDP or `Content-Type: application/trickle-ice+json` are applied during the call (same JSON format as classic RTP path in `internal/media`).
- Legacy full-gather: pass `ICETrickleFullGather: true` in bridge options (waits for complete gathering, up to `ICEGatherTimeout`).

**TURN REST refresh**

When `ice_auth_secret` is set, each bridge mints REST credentials at creation and refreshes them automatically before expiry (margin ≈ min(60s, TTL/4)). Diagnostics expose `turn_cred_expires` and `turn_refresh_count`.

## What is *not* yet wired

- No SDP munging is performed between SIP and WebRTC offers beyond template injection. Scenario authors that need custom SIP SDP headers should still inline bridge output via `[webrtc_offer]` / `[webrtc_answer]`.
- Outbound SIP INFO for locally gathered trickle candidates is not automated — publish `[webrtc_trickle]` / scenario-driven INFO if peers need late local candidates.

## Runtime integration roadmap (Phase 4.2)

The wiring is non-trivial because today's media path is hard-wired around
`*media.Session`. A subagent code walk produced the following anchored plan;
sections in brackets cite the current code so reviewers can sanity-check the
patch surface.

### Hot spots in today's call flow

- `internal/engine/engine.go::executeCall` is the **single** entry point that
  owns a SIP call's media. Both UAC (`runClient*`) and UAS
  (`udpServerReceivePump`, `runServerTCP*`, `runServerWSShared`) reach this
  function.
- `media.NewSession(...)` is constructed in `executeCall` and once more in
  `runInit`. Outside those two call sites, nothing else hands out media
  sessions.
- SDP for outbound INVITEs is template-substituted from XML scenarios; SDP
  for inbound INVITEs is parsed by `internal/media.ParseAudioEndpoint` (and
  `EffectiveMediaSDPBody` for trickle ICE).
- Media starts / stops only via `applyExecAction` against
  `media.Session.{Start, Pause, Stop, StartRecording, ...}`.

### Patch surface (in dependency order)

1. **Adapter interface** in `internal/media` — `MediaPipeline` exposing the
   subset of methods `executeCall` actually calls today (Start/Pause/Stop/
   StartRecording/SetRemote/RTPStats). `*Session` becomes one implementation;
   a new `*webrtc.Bridge` adapter becomes the other.
2. **Engine factory branch** — when the resolved profile transport is
   `webrtc` (or scenario carries `[transport]=webrtc`), call
   `Engine.NewWebRTCBridge()` instead of `media.NewSession`. The returned
   bridge is wrapped in the new adapter so the rest of `executeCall` stays
   unchanged.
3. **SDP bridging** — for UAC: ask the Bridge for an offer, splice its
   `a=fingerprint`, `m=audio … UDP/TLS/RTP/SAVPF` and ICE candidates into
   the scenario template via a new `[webrtc_offer]` placeholder. For UAS:
   strip the inbound offer into `Bridge.Answer`, return that body to the
   scenario via `[webrtc_answer]`. `internal/template/render.go` needs the
   new keys; the XML scenario format does not.
4. **G.711 piping** — `Session.Start` synthesizes PCMA/PCMU. The adapter
   instead drives `Bridge.WritePCMA`, and on inbound registers
   `Bridge.OnPCMA` → feed the same `appendCallRecordJSONL` / WAV recorder
   used today.
5. **Stats** — `internal/stats` currently consumes `media.Stats`. Either
   keep a thin counter inside the Bridge adapter, or extend `Stats` with a
   `Kind` discriminator. Recording (WAV / stereo) keeps working because the
   adapter writes into the same `recordings/` directory.
6. **PCAP replay / mic / DTMF** — these inputs only exist on
   `*media.Session` today. They stay unsupported with WebRTC in v1; the
   adapter returns a clear `ErrUnsupportedOverWebRTC` when a scenario tries
   to invoke them.

### Risks worth budgeting for

| Risk | Source |
|---|---|
| SDP profile mismatch (`RTP/AVP` vs `UDP/TLS/RTP/SAVPF`) | scenario templates assume `RTP/AVP`; SAVPF requires DTLS fingerprint + ICE candidates pre-baked |
| ICE gather completes only after 5s | `internal/webrtc/bridge.go` waits for gather completion before returning the offer — `executeCall` is currently sequential and would block the dialog |
| Trickle ICE | inbound SDP parser supports trickle JSON for `media.Session` only |
| Symmetric RTP port maths | `localPort + 2 + ...` formula in scenario template will not match pion's UDP MUX choices |
| RTCP / loss stats | RTCP loop in `media` does not exist in Bridge — needs explicit telemetry or graceful blanks in `stats` output |
| Microphone / RFC 2833 DTMF | not represented in Bridge API today |

### Suggested first PR scope

- Land the `MediaPipeline` adapter in `internal/media`.
- Wire **only** the answer side of WebRTC (UAS) — easier to test against a
  browser peer.
- Keep ICE gather behind an `ICEGatherTimeout` option so `executeCall` does
  not stall when STUN is unreachable.
- Ship behind an opt-in scenario flag (`<scenario webrtc="true">`) so the
  default SIP path is untouched.

The bridge itself is intentionally tiny so it stays useful for ad-hoc
experiments (e.g. driving Pion-based test clients from Gossipper scenarios)
even before the full engine integration lands.
