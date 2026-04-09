# RTP in gossIpper scenarios

This document explains how to send and receive RTP media from within XML scenario
files.

## How media endpoints are resolved

`gossIpper` automatically extracts the remote RTP endpoint from the last received
SIP message.  The engine parses the SDP body and reads the `m=audio`, `m=video`,
or `m=image` line to determine the destination IP and port.  No manual endpoint
configuration is required inside the scenario XML.

The local bind IP and port are derived from `[local_ip]` and `[media_port]`.

---

## exec rtp_stream

The primary way to control an audio RTP stream from a scenario is the
`exec rtp_stream` action inside any command that supports `<action>` blocks
(`<nop>`, `<recv>`, etc.).

> **Synthetic streams** — to stream without a media file, use the `synthetic`
> keyword instead of a file path.  See [synthetic-rtp-sender.md](synthetic-rtp-sender.md)
> for the full reference.

### Start a stream from a file

```xml
<nop>
  <action>
    <exec rtp_stream="audio.raw"/>
  </action>
</nop>
```

The file path is resolved relative to the scenario directory.  Both `.raw` and
`.wav` files are supported.

WAV files must be **PCM mono 8 kHz** (8-bit or 16-bit samples).  Raw files are
read as-is and split into fixed-size chunks.

#### Full parameter syntax

```
rtp_stream="<path>,<loop_count>,<payload_type>,<payload_name>"
```

| Parameter | Default | Description |
| --- | --- | --- |
| `path` | required | Path to the audio file |
| `loop_count` | `1` | Number of times to loop the file; `-1` loops indefinitely |
| `payload_type` | `0` | RTP payload type number |
| `payload_name` | `PCMU/8000` | Codec descriptor |

Supported payload descriptors:

| Descriptor | PT | Clock rate | Pkt duration |
| --- | --- | --- | --- |
| `PCMU/8000` | 0 | 8000 Hz | 20 ms |
| `PCMA/8000` | 8 | 8000 Hz | 20 ms |
| `G722/8000` | 9 | 8000 Hz | 20 ms |
| `ILBC/8000` | 97 | 8000 Hz | 30 ms |
| `H264/90000` | 96 | 90000 Hz | 33 ms |

Examples:

```xml
<!-- PCMU, play once -->
<exec rtp_stream="audio.raw,1,0,PCMU/8000"/>

<!-- PCMA WAV, loop 3 times -->
<exec rtp_stream="voice.wav,3,8,PCMA/8000"/>

<!-- Loop indefinitely until stop -->
<exec rtp_stream="hold_music.raw,-1,0,PCMU/8000"/>
```

### Control commands

```xml
<!-- Pause the running stream -->
<exec rtp_stream="pause"/>

<!-- Resume a paused stream -->
<exec rtp_stream="resume"/>

<!-- Stop the stream and close the socket -->
<exec rtp_stream="stop"/>

<!-- Echo mode: reflects every received RTP packet back to the sender -->
<exec rtp_stream="echo"/>
```

---

## exec play_pcap_audio

Replays a pre-recorded PCAP capture to the remote audio endpoint discovered from
SDP.  Inter-packet timing from the original capture is preserved.

```xml
<nop>
  <action>
    <exec play_pcap_audio="capture.pcap"/>
  </action>
</nop>
```

The engine looks for the `m=audio` line in the last SDP to determine the
destination.

---

## exec play_pcap_video

Replays a PCAP capture to the video endpoint discovered from `m=video` in SDP.

```xml
<nop>
  <action>
    <exec play_pcap_video="video.pcap"/>
  </action>
</nop>
```

This is a pragmatic implementation: the raw RTP payload from the PCAP is
forwarded as-is without any codec-specific processing.

---

## exec play_pcap_image

Same as `play_pcap_video` but targets the `m=image` SDP line.

```xml
<nop>
  <action>
    <exec play_pcap_image="fax.pcap"/>
  </action>
</nop>
```

---

## exec rtpcheck

Blocks scenario execution until a minimum number of RTP packets have been
observed, or a timeout expires.

```xml
<nop>
  <action>
    <exec rtpcheck="min_packets=10 timeout_ms=2000 direction=any"/>
  </action>
</nop>
```

### Parameters

| Parameter | Default | Description |
| --- | --- | --- |
| `min_packets` | `1` | Minimum RTP packet count required |
| `timeout_ms` | `1000` | Timeout in milliseconds |
| `direction` | `any` | `any`, `send`, `recv`, or `both` |

`direction=both` requires at least `min_packets` in **each** direction.

Short form (just a packet count, 1-second timeout, `any` direction):

```xml
<exec rtpcheck="10"/>
```

---

## exec send_dtmf

Sends DTMF tones as RFC 2833 telephone-event RTP packets to the audio endpoint
discovered from SDP.

```xml
<nop>
  <action>
    <exec send_dtmf="1234#"/>
  </action>
</nop>
```

Supported digits: `0–9`, `*`, `#`, `A–D`.

---

## RTCP

RTCP sender reports are emitted automatically by the media session every 500 ms
while a stream is running.  Incoming RTCP packets are parsed and counted.
RTCP statistics are aggregated into the engine summary and JSON export.

---

## Complete example: UAC audio call

```xml
<?xml version="1.0" encoding="UTF-8" ?>
<scenario name="audio-uac">

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
      <!-- Start RTP stream to the endpoint found in the 200 OK SDP -->
      <exec rtp_stream="audio.raw,1,0,PCMU/8000"/>
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

  <!-- Wait for the stream to finish, or hold for a fixed duration -->
  <pause milliseconds="4000"/>

  <!-- Stop media before hanging up -->
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

## Complete example: UAS with echo

```xml
<?xml version="1.0" encoding="UTF-8" ?>
<scenario name="audio-uas-echo">

  <recv request="INVITE"/>

  <send>
    <![CDATA[
SIP/2.0 100 Trying
[last_Via:]
[last_From:]
[last_To:]
[last_Call-ID:]
[last_CSeq:]
Content-Length: 0
    ]]>
  </send>

  <send>
    <![CDATA[
SIP/2.0 200 OK
[last_Via:]
[last_From:]
[last_To:];tag=[call_number]
[last_Call-ID:]
[last_CSeq:]
Contact: <sip:[local_ip]:[local_port]>
Content-Type: application/sdp
Content-Length: [len]

v=0
o=gossIpper 0 0 IN IP[local_ip_type] [local_ip]
s=-
c=IN IP[local_ip_type] [local_ip]
t=0 0
m=audio [media_port] RTP/AVP 0
a=rtpmap:0 PCMU/8000
    ]]>
  </send>

  <recv request="ACK">
    <action>
      <!-- Reflect incoming RTP back to the caller -->
      <exec rtp_stream="echo"/>
    </action>
  </recv>

  <recv request="BYE">
    <action>
      <exec rtp_stream="stop"/>
    </action>
  </recv>

  <send>
    <![CDATA[
SIP/2.0 200 OK
[last_Via:]
[last_From:]
[last_To:];tag=[call_number]
[last_Call-ID:]
[last_CSeq:]
Content-Length: 0
    ]]>
  </send>

</scenario>
```

---

## Known limits

- No SRTP support.
- `rtpcheck` performs pragmatic activity counting only; full SIPp `rtpcheck`
  parity with jitter and loss metrics is deferred.
- No dedicated video/image codec pipeline; PCAP replay forwards raw RTP payloads.
- HEP mirroring of RTP/RTCP is deferred; only SIP signaling is currently
  forwarded to Homer.
