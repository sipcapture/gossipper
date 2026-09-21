# Lab scenarios (kefir ports)

Embedded SIPp XML with the **same ids** as kefir’s bundled gateway catalog
(`internal/siprtp/bundled/*.toml`). They are listed on Control UI **Scenarios**
under **Lab scenarios (kefir ports)** and in job/profile dropdowns as
**Lab (kefir ports)**.

XML lives in [`internal/scenario/lab/`](../internal/scenario/lab/) and is compiled
into the binary (`go:embed`). `GET /api/v2/builtin-scenarios` returns them with
`"source": "lab"`.

## How to run

CLI (UAC toward a PBX, unless noted):

```bash
gossipper -sn one_way -i 127.0.0.1 -p 5060 -s 100
```

UAS labs (`early_183`, `late_180`, and `{id}_uas` twins) need a listener:

```bash
gossipper -sn one_way_uas -t uas
```

In the Control UI: pick the id on a client/server profile, or **Clone to editor**
on Scenarios so the XML lands in `scenarios/<id>_copy.xml` and opens on the Graph
canvas. Nested `<nop><action><exec rtp_stream=…>` nodes stay **raw** so they are
not dropped.

To run a UAC lab **through a PBX AOR** (REGISTER + originate) or arm inbound
UAS labs (`one_way_uas`, `early_183`, `late_180`, …) on the registered Contact, see
[SIP REGISTER gateway](gateway.md).

## Mapping vs kefir

Gossipper is not kefir. There is no `drop_rx`, no second hairpin socket, no
bundled `kws.ulaw`, and a UAC cannot emit an illegal 180 after CONNECT.

| Approximation | Gossipper behaviour |
| --- | --- |
| RTP speech / cadence | `<exec rtp_stream="synthetic,0,0,PCMU/8000,20"/>` plus `pause` / `stop` |
| Comfort Noise | PT 13 `CN/8000` (`rtp_stream="synthetic,0,13,CN/8000,20"`) |
| No RTP / mute / recvonly | SDP direction only; omit `rtp_stream` |
| Hold / resume | re-INVITE `a=inactive` then `a=sendrecv` |
| T.38 | `m=image [media_port] udptl t38` |
| Port move | re-INVITE `m=` `[media_port+2]` |
| Media redirect | re-INVITE `c=IN IP4 127.0.0.2` |
| `early_183` / `late_180` | **UAS** only (the only way those SIP sequences work here) |
| `{id}_uas` | Inbound twin of the UAC lab: recv INVITE, answer with the same SDP/RTP, then wait for BYE (`short_call_uas` sends BYE at 1500 ms). Hold/gap/redirect re-INVITE toward the caller. |
| `kws` / `hairpin` | bidirectional synthetic PCMU (not a keyword clip / not a second socket) |

Engine-baked `invite_media_early` is a separate UAC early-media load scenario.
Lab `early_183` is the kefir inbound 183+SDP port.

## Catalog

| Id | Role | Intent |
| --- | --- | --- |
| `one_way` | uac | N1: `a=sendonly` + TX RTP |
| `no_rtp` | uac | N4: `a=inactive`, no stream |
| `cn_only` | uac | N5: RTP PT 13 only |
| `audio_gap` | uac | N6: 2s RTP then re-INVITE inactive |
| `codec_chg` | uac | N8: PCMU then PCMA on the same 5-tuple |
| `fas_cadence` | uac | N13: 1s on / 2s off ×3 after 1.5s |
| `fas_speech` | uac | N14: continuous synthetic G.711 |
| `fas_early` | uac | N16: cadence in the first 1.5s |
| `fas_late` | uac | N16: cadence starts 10s after CONNECT |
| `fake_ringing` | uac | Same cadence as N13 |
| `kws` | uac | Synthetic PCMU stand-in for `kws.ulaw` |
| `late_180` | uas | 200 then illegal 180 |
| `hairpin` | uac | Bidirectional synthetic RTP (no second socket) |
| `media_redirect` | uac | re-INVITE `c=127.0.0.2` |
| `short_call` | uac | BYE 1500 ms after CONNECT |
| `fax_t38` | uac | INVITE `m=image` |
| `recvonly` | uac | N2: `a=recvonly`, no TX |
| `hold_moh` | uac | re-INVITE `a=sendonly`, keep TX |
| `hold_resume` | uac | inactive then sendrecv |
| `port_move` | uac | re-INVITE `m=` `[media_port+2]` |
| `codec_chg_alaw` | uac | PCMA then PCMU |
| `rtp_mute` | uac | sendrecv SDP, no stream |
| `g711_silence` | uac | Synthetic PCMU μ-law 0xFF |
| `cn_then_speech` | uac | PT 13 then PCMU |
| `early_183` | uas | 183+SDP and RTP before CONNECT |
| `fax_switch` | uac | G.711 then re-INVITE T.38 |
| `one_way_uas` … `fax_switch_uas` | uas | Inbound twin of each UAC lab (same SDP/RTP; arm on gateway Contact) |
