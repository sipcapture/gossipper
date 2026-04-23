# Synthetic RTP sender

Gossipper can generate and transmit RTP streams **without a media file**.  Silence
frames are produced programmatically at the configured codec rate, which lets you
load-test media paths, validate RTP reachability, and verify HEP/RTCP plumbing
without preparing `.wav` or `.raw` fixtures.

There are two independent entry points:

| Mode | Trigger | Use-case |
|---|---|---|
| **Standalone CLI** | `-rtp_send` flag | Send RTP without any SIP scenario |
| **Scenario action** | `rtp_stream="synthetic,…"` | Emit synthetic RTP inside an existing call flow |

---

## Standalone CLI mode

Run Gossipper as a pure RTP generator by passing `-rtp_send`.  The SIP scenario
engine is bypassed entirely.

### Required flag

| Flag | Description |
|---|---|
| `-rtp_send` | Enable standalone RTP sender mode |
| `-rtp_addr <host:port>` | Destination UDP address |

### Optional flags

| Flag | Default | Description |
|---|---|---|
| `-rtp_codec <name>` | `PCMU/8000` | Codec descriptor; sets PT, clock rate and packet size |
| `-rtp_pt <n>` | `0` | Override payload type number after codec params are applied (useful for dynamic PTs) |
| `-rtp_freq <ms>` | `20` | Packet interval in milliseconds |
| `-rtp_dur <ms>` | `0` | Total duration in milliseconds; `0` runs until interrupted |
| `-rtp_ch <n>` | `1` | Number of audio channels |
| `-i <ip>` | `0.0.0.0` | Local bind IP (shared with the SIP engine) |

### Supported codec descriptors

| Descriptor | PT | Clock rate | Pkt interval | Payload/pkt |
|---|---|---|---|---|
| `PCMU/8000` | 0 | 8 000 Hz | 20 ms | 160 B (μ-law silence `0xFF`) |
| `PCMA/8000` | 8 | 8 000 Hz | 20 ms | 160 B (A-law silence `0xD5`) |
| `G722/8000` | 9 | 8 000 Hz | 20 ms | 160 B (zero) |
| `ILBC/8000` | 97 | 8 000 Hz | 30 ms | 240 B (zero) |
| `H264/90000` | 96 | 90 000 Hz | 33 ms | 3000 B (zero) |
| `OPUS/48000` | 111 | 48 000 Hz | 20 ms | 3 B (Opus DTX silence frame) |

> **Dynamic payload types** — for ILBC, H264 and Opus the PT values above are
> common defaults.  Use `-rtp_pt` to override when the remote party negotiates a
> different PT in the SDP offer.

### Examples

```bash
# Send PCMU silence for 5 seconds at 20 ms intervals
gossipper -rtp_send -rtp_addr 192.168.1.10:20000 -rtp_dur 5000

# Send PCMA at 20 ms intervals until Ctrl-C
gossipper -rtp_send -rtp_addr 192.168.1.10:20000 -rtp_codec PCMA/8000

# G722 with a non-default dynamic PT, 10 seconds
gossipper -rtp_send -rtp_addr 192.168.1.10:20000 \
  -rtp_codec G722/8000 -rtp_pt 110 -rtp_dur 10000

# 30 ms ILBC frames, stereo channel layout, 1 minute
gossipper -rtp_send -rtp_addr 192.168.1.10:20000 \
  -rtp_codec ILBC/8000 -rtp_ch 2 -rtp_dur 60000

# Bind to a specific local IP
gossipper -rtp_send -rtp_addr 192.168.1.10:20000 -i 10.0.0.5 -rtp_dur 3000
```

### Output

Gossipper prints a start line and a summary when the stream stops:

```
rtp_sender: streaming to 192.168.1.10:20000  codec=PCMA/8000 pt=8 freq=20ms duration=5000ms channels=1
rtp_sender: done  sent=250 octets=40000 rtcp_sr=0
```

---

## Scenario action: `rtp_stream="synthetic,…"`

The `synthetic` keyword can be used anywhere `rtp_stream` is accepted inside an
XML scenario.  It follows the same comma-separated parameter convention as
file-based streaming but requires no file path.

### Syntax

```
rtp_stream="synthetic[,<loop_count>[,<pt>[,<codec_name>[,<freq_ms>[,<dur_ms>[,<channels>]]]]]]"
```

All parameters after `synthetic` are optional and fall back to the same defaults
as the standalone CLI mode.

### Parameters

| Position | Name | Default | Description |
|---|---|---|---|
| 1 | `loop_count` | — | Accepted for spec symmetry but ignored; `dur_ms` (or context cancellation) controls the stop |
| 2 | `pt` | from codec | RTP payload type number |
| 3 | `codec_name` | `PCMU/8000` | Codec descriptor (same table as CLI) |
| 4 | `freq_ms` | `20` | Packet interval in milliseconds |
| 5 | `dur_ms` | `0` | Duration in milliseconds; `0` = stream until `stop` action or call teardown |
| 6 | `channels` | `1` | Number of audio channels |

### Examples

```xml
<!-- PCMU silence, default 20 ms, run until stop -->
<exec rtp_stream="synthetic"/>

<!-- PCMA, 20 ms intervals, stop after 5 seconds -->
<exec rtp_stream="synthetic,0,8,PCMA/8000,20,5000,1"/>

<!-- G722, 20 ms intervals, 10 seconds, stereo -->
<exec rtp_stream="synthetic,0,9,G722/8000,20,10000,2"/>

<!-- ILBC, 30 ms intervals, run until stop -->
<exec rtp_stream="synthetic,0,97,ILBC/8000,30,0,1"/>

<!-- H264, 33 ms intervals, 30 seconds -->
<exec rtp_stream="synthetic,0,96,H264/90000,33,30000,1"/>
```

Control commands (`pause`, `resume`, `stop`, `echo`) work the same way as with
file-based streams:

```xml
<exec rtp_stream="pause"/>
<exec rtp_stream="resume"/>
<exec rtp_stream="stop"/>
```

### Complete scenario example

The following UAC scenario sends synthetic PCMU media for the duration of the
call without requiring any audio file on disk:

```xml
<?xml version="1.0" encoding="UTF-8" ?>
<scenario name="synthetic-audio-uac">

  <send retrans="500">
    <![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: sipp <sip:sipp@[local_ip]:[local_port]>;tag=[call_number]
To: sut <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: [cseq] INVITE
Contact: sip:sipp@[local_ip]:[local_port]
Max-Forwards: 70
Content-Type: application/sdp
Content-Length: [len]

v=0
o=user1 53655765 2353687637 IN IP[local_ip_type] [local_ip]
s=-
c=IN IP[local_ip_type] [local_ip]
t=0 0
m=audio [media_port] RTP/AVP 0
a=rtpmap:0 PCMU/8000
    ]]>
  </send>

  <recv response="100" optional="true"/>

  <recv response="200" rtd="true">
    <action>
      <!-- Start synthetic PCMU stream; no audio file needed -->
      <exec rtp_stream="synthetic,0,0,PCMU/8000,20,0,1"/>
    </action>
  </recv>

  <send>
    <![CDATA[
ACK sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: sipp <sip:sipp@[local_ip]:[local_port]>;tag=[call_number]
To: sut <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: [cseq] ACK
Max-Forwards: 70
Content-Length: 0
    ]]>
  </send>

  <pause milliseconds="4000"/>

  <nop>
    <action>
      <exec rtp_stream="stop"/>
    </action>
  </nop>

  <send retrans="500">
    <![CDATA[
BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: sipp <sip:sipp@[local_ip]:[local_port]>;tag=[call_number]
To: sut <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: [cseq] BYE
Max-Forwards: 70
Content-Length: 0
    ]]>
  </send>

  <recv response="200"/>

</scenario>
```

---

## Payload content

Synthetic payloads contain codec-appropriate silence bytes:

| Codec | Payload | Rationale |
|---|---|---|
| PCMU (G.711 μ-law) | `0xFF` × `SamplesPerPkt × Channels` | μ-law encoding of zero-amplitude signal |
| PCMA (G.711 A-law) | `0xD5` × `SamplesPerPkt × Channels` | A-law encoding of zero-amplitude signal |
| Opus | `{0xF8, 0xFF, 0xFE}` (3 bytes) | Valid CELT FB 20 ms mono DTX frame (RFC 6716) |
| All others | `0x00` × `SamplesPerPkt × Channels` | Zero-filled; most codecs treat this as silence or a degenerate frame |

For PCM-based codecs (PCMU, PCMA, G722) the payload size is
`SamplesPerPkt × Channels`.  For mono PCMU/PCMA at 20 ms this is
`160 × 1 = 160` bytes per packet.

For Opus, the payload is always 3 bytes regardless of channel count.  Opus is a
variable-bitrate codec; its frames are not raw PCM and their size does not follow
the PCM formula.  The `SamplesPerPkt` value (960 for 20 ms at 48 kHz) is used
only for the RTP timestamp increment.

---

## Channels

The `-rtp_ch` flag (CLI) or the `channels` parameter (scenario) multiplies the
per-packet payload size:

```
payload_bytes = samples_per_packet × channels
```

RTP itself is not inherently channel-aware; the channel count here simply
controls the byte count placed in each packet payload.  The remote endpoint must
be configured to expect the same frame size.

---

## Interaction with HEP mirroring

When `-hep_addr` is configured and `-send_media_report` is enabled, synthetic
RTP packets are mirrored to the HEP collector the same way file-based packets
are.  No additional configuration is needed.

---

## Differences from file-based `rtp_stream`

| Aspect | File-based | Synthetic |
|---|---|---|
| Requires a media file | Yes (`.wav` or `.raw`) | No |
| Payload content | Actual audio samples | Codec-appropriate silence |
| Loop control | `loop_count`; `-1` = infinite | Ignored; use `dur_ms` or `stop` |
| Duration control | Derived from file length × loop count | `dur_ms` parameter or context cancellation |
| Codec parameters | Parsed from file format + spec | Fully configurable via spec |

---

## Known limits

- Synthetic streams always produce silence; arbitrary tone or noise generation is
  not supported.
- No SRTP support.
- Multi-channel WAV encoding is not validated against the `channels` parameter;
  the parameter only affects the raw byte count of each synthetic payload.
