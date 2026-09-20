# SIP REGISTER gateway

One `gossipper server` process can register one or more AORs on a PBX, originate
lab UAC calls as a chosen AOR, and answer inbound INVITEs on the registered
Contact with a per-profile armed UAS scenario. There is no WebRTC path, no kefir
Testing mode, and no separate `gossipper-agent` binary.

Example config: [`examples/gossipper-gateway.json`](../examples/gossipper-gateway.json).
Lab UAC/UAS ids: [lab scenarios](lab-scenarios.md). Control UI: [UI mode](ui-mode.md).

## How it works

1. Each **enabled** profile with `register=true` runs a Go Digest REGISTER loop
   (not SIPp XML) on the **UAS listen UDP** (same port as Contact).
2. Contact is `sip:{aor_user}@{advertised_ip}:{contact_port}` where
   `contact_port` is the management **UAS listen** port (default 5060).
3. FreeSWITCH / Sofia NAT-pins inbound INVITE to the REGISTER source
   (`received=` / `Route`). REGISTER and UAS must share that port so INVITE
   lands on the armed scenario instead of a dead ephemeral socket.
4. **Originate** starts a supervisor UAC job toward the PBX with `sip_from` =
   `sip:{user}@{domain}` and that profile's Digest credentials.
5. **Arm** stores a UAS scenario on the profile (`armed_scenario_id` in
   `gateway.json`). After restart the same id is loaded and re-armed. New
   INVITEs whose Request-URI user matches the profile AOR use that scenario.
   Unmatched INVITEs fall back to the last-armed live UAS. In-flight calls keep
   the previous XML. **Save** and **Originate** also persist the last UAC
   scenario, destination, and call count (`originate_scenario_id` /
   `originate_to` / `originate_calls`) so the Gateway dialog restores them
   after restart.

Disable a profile to stop REGISTER and refuse originate. Inbound matching also
ignores disabled profiles.

## Config

Top-level `"gateway"` (object) remains a seed for a single profile. Prefer
`"gateways"` (array) for more than one. Both may appear: the object is first,
then the array. `{ui_data_dir}/gateway.json` overrides the JSON seed after the
first save (legacy single-object files migrate to `{ "profiles": [ ... ] }`).

```json
"gateways": [
  {
    "id": "desk",
    "name": "Desk 1001",
    "enabled": true,
    "domain": "pbx.local",
    "addr": "192.168.1.10:5060",
    "username": "1001",
    "password": "secret",
    "register": true,
    "advertised_ip": "192.168.1.20",
    "contact_port": 5060
  }
]
```

Omitted `"enabled"` defaults to **true** (CLI seed / legacy files).

| Field | Notes |
| --- | --- |
| `id` | Path id (`[A-Za-z0-9][A-Za-z0-9._-]{0,63}`); generated if empty |
| `name` | UI label (defaults to AOR user or `Gateway`) |
| `enabled` | Off stops REGISTER, originate, and inbound AOR match |
| `domain` | SIP domain in AOR / From |
| `addr` | Registrar `host:port` (default port 5060) |
| `transport` | `udp` only in v1 |
| `username` / `password` | Digest credentials |
| `register_user` | Optional AOR user when it differs from `username` |
| `register` | When true (and enabled), start the refresh loop |
| `advertised_ip` | Host in REGISTER Contact **and** UAS 180/200 Contact/SDP (must be reachable from the PBX) |
| `register_expires` | Requested TTL seconds (default 300) |
| `keepalive_seconds` | NAT keep-alive OPTIONS interval toward the registrar (default 20; `-1` disables) |
| `contact_port` | Always the UAS listen port (server `listeners[0]`); REGISTER Contact uses that socket, not a stale saved value |

The loop honours Expires from 200 (header or Contact `expires=`), refreshes at
90% (minimum 5s), retries every 15s on failure, and sends `Expires: 0` on
shutdown from the **same** UDP socket and Call-ID. While registered, gossipper
sends fire-and-forget **OPTIONS** to the registrar on that socket every
`keepalive_seconds` (default 20) so consumer NAT mappings (~30–60s) stay open;
inbound INVITE to Contact would otherwise black-hole. Saving a profile does not
re-REGISTER unless SIP identity, credentials, advertised IP, keep-alive, or
enable/register changed. Passwords are never logged.

UI persistence: `{ui_data_dir}/gateway.json` (mode `0600`), including
`armed_scenario_id` and originate defaults (`originate_scenario_id`,
`originate_to`, `originate_calls`) per profile. Control UI **Save** writes the
selected Armed UAS and UAC originate fields with the SIP fields. A PUT that
omits `armed_scenario_id` keeps the previous arm. Empty originate fields keep
the previous UAC defaults (so enable/disable does not wipe them). **Originate**
also remembers the last used UAC id, destination, and call count. The server
JSON is only a seed when that file is missing.

## NAT

Set `advertised_ip` to the address the PBX must use in Request-URI / Contact
routing. If gossipper is behind NAT, that is typically a public or SBC-facing
IP with UDP forwarded to the UAS listen port. REGISTER is sent from that same
listen port so the registrar's NAT mapping matches Contact. Contact in REGISTER
and keep-alive OPTIONS is that same listen port — a saved `contact_port` that
differs (for example 15069 vs listen 5060) is ignored, otherwise the PBX INVITEs
a hole that does not exist. REGISTER refresh alone (~270s at default Expires
300) is too slow for typical UDP NAT timeouts; keep-alive OPTIONS on the same
socket (default 20s) holds the mapping so inbound INVITE to
`sip:{aor}@{advertised_ip}:{uas_port}` reaches gossipper.

UAS answers (180/200 Contact and SDP `c=`) also use that `advertised_ip` for
`[local_ip]`, not the LAN bind address. RFC 3261 ACK to 200 is sent to Contact;
a private Contact (`192.168.x.x`) is unreachable from the PBX, so Timer G
retransmits 200 forever. RTP sockets bind the real listen/bind IP (or `0.0.0.0`
when that address is not on a local interface) — `advertised_ip` is SIP/SDP
only. Builtin `uas` answers INVITE with negotiated SDP and plays synthetic RTP
until BYE. Cadence labs stay on `fake_ringing_uas` / `one_way_uas`.

## Originate vs arm

| Action | Role | API | Typical ids |
| --- | --- | --- | --- |
| Originate | UAC toward the trunk | `POST /api/v2/gateways/{id}/originate` | `one_way`, `short_call`, custom UAC XML |
| Arm inbound | UAS on Contact | `PUT /api/v2/gateways/{id}/arm` | `uas`, `one_way_uas`, `early_183`, `late_180`, custom UAS XML |

Compat aliases: `GET`/`PUT /api/v2/gateway`, `PUT /gateway/arm`,
`POST /gateway/originate` operate on the **first** profile.

Arming a UAC id (for example `one_way`) returns **400** — use originate.
Originating a UAS id returns **400** — use arm.

Empty arm id resets to builtin `uas`. Each UAC lab has an inbound twin
(`one_way_uas`, `no_rtp_uas`, …) that answers INVITE with the same SDP/RTP.

When any gateway profile is enabled, unmatched out-of-dialog **OPTIONS** get an
immediate 200 (PBX keep-alive) without consuming the armed INVITE scenario.

## REST (`/api/v2`)

| Method | Path | Body / result |
| --- | --- | --- |
| `GET` | `/gateways` | `{ "gateways": [ snapshot, ... ] }` |
| `POST` | `/gateways` | Create profile; password masked in response |
| `GET` | `/gateways/{id}` | One snapshot |
| `PUT` | `/gateways/{id}` | Update; empty/`***` password keeps the previous secret |
| `DELETE` | `/gateways/{id}` | 204 |
| `PUT` | `/gateways/{id}/arm` | `{ "scenario_id": "early_183" }` |
| `POST` | `/gateways/{id}/originate` | `{ "scenario_id": "one_way", "to": "100", "total_calls": 1 }` → `{ "job_id" }` |
| `GET` | `/gateway` | First profile (compat) |
| `GET` | `/gateway/ips` | Local IPv4s + public IPv4 for Advertised IP Autodiscover |
| `PUT` | `/gateway` | Create-or-update first profile |
| `PUT` | `/gateway/arm` | Arm first profile |
| `POST` | `/gateway/originate` | Originate as first profile |
| `GET` | `/sip/trace?since=&limit=` | REST snapshot of the process-wide live-trace ring (`{ messages, next, capture }`; `kind` is `sip` or `app`) |
| `GET` | `/sip/trace/ws?since=&token=` | WebSocket: first frame is backlog, then push on each new record / Clear / capture change |
| `GET`/`PUT` | `/sip/trace/capture` | `{ sip, app, level }` independent capture flags plus Debug floor (`debug`/`info`/`warn`/`error`, default `debug`; empty PUT `level` keeps the previous value). SIP default on, app/debug default off |
| `GET` | `/sip/trace/export?format=text or pcap&scenario=` | Full ring as a `.txt` dump or reconstructed Ethernet/IPv4/UDP `.pcap` (pcap is SIP only; respects capture flags) |
| `POST` | `/sip/trace/clear` | Drop the in-memory ring (`{ cleared: true }`) |

Originate jobs use `profile_kind=gateway` and `profile_id` = gateway id, with
engine overlay `remote_host` / `remote_port` = registrar, `sip_from`,
`auth_username` / `auth_password`, `service` = `to`.

Challenged trunks need Digest-ready UAC XML (or builtin `uac` / `invite_media`).
IP-auth PBXs work with the current lab UAC scenarios.

## Control UI

Nav **Gateway**: table of profiles (name, AOR, REGISTER status, enable switch)
and a modal to create/edit, Autodiscover Advertised IP, arm UAS, and originate.
The edit dialog footer shows Save progress (`Saving…`, `Saved · time · register
state`, or `Save failed`). **Live Trace** is its own nav page (`#/sip`): a
process-wide circular buffer of SIP (UAS listen UDP plus in-process engines)
and scenario/app debug (`call.started`, command steps, `<log>`, timeouts).
**SIP** and **Debug** buttons independently enable capture. Filter by scenario.
Click a row for the raw message. Pause / Clear (`POST /api/v2/sip/trace/clear`).
**Text** / **PCAP** download the full ring (`GET /api/v2/sip/trace/export`); pcap
is SIP only. The page is a WebSocket (`/sip/trace/ws`), not HTTP polling. Gateway
has a shortcut button to the same page. Originate UAC jobs still run as a child
process and do not appear here. No Graph or WebRTC on Gateway.
