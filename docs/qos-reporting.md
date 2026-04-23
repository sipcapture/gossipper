# QoS media reporting

`Gossipper` can mirror RTP and RTCP quality statistics to a Homer/hepic
collector via HEP3. This allows call-quality metrics to appear alongside SIP
traces in the Homer UI without a separate network capture agent.

Reporting is **disabled by default**. It must be explicitly enabled with
`-send_media_report=true` and requires `-hep_addr` to point at a running
HEP3 collector.

---

## CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `-hep_addr` | `""` | HEP3 collector address (`host:port`). Required for any HEP output. |
| `-hep_capture_id` | `0` | HEP3 capture node ID sent in every encapsulated packet. |
| `-hep_password` | `""` | Optional HEP3 auth key. |
| `-hep_raw_rtcp` | `true` | Selects the report format: `true` = raw binary RTCP, `false` = JSON reports. |
| `-send_media_report` | `false` | Enables periodic QoS reporting. When `false`, nothing is sent to HEP for RTP or RTCP. |

---

## Operating modes

The combination of `-send_media_report` and `-hep_raw_rtcp` selects one of
three distinct behaviors.

### Disabled (default)

```
-send_media_report=false
```

No RTP or RTCP data is sent to the HEP collector. SIP messages are still
mirrored if `-hep_addr` is set.

### JSON report mode

```
-send_media_report=true -hep_raw_rtcp=false
```

`Gossipper` accumulates per-SSRC statistics in the HEP client and emits
three JSON report types every **10 seconds** per active RTP stream:

- **HEP type 35** — Short RTP Report. Contains packet count, octet count,
  RTP timestamp, SSRC, and a `CORRELATION_ID` / `RTP_SIP_CALL_ID` mapped
  to the SIP `Call-ID` of the originating call.
- **HEP type 37** — Short RTCP Report. Contains RTCP Sender Report fields
  (NTP timestamp, packet count, octet count) plus cumulative packet loss
  (reported as `0` because `Gossipper` is a sender and does not measure
  loss on its own outbound stream).
- **HEP type 100** — DTMF Report (emitted only when `telephone-event` RFC 2833
  packets were detected during the interval). Contains the accumulated digit
  events with timestamps, volume, and duration. See [DTMF reporting](#dtmf-reporting) below.

These types are processed by `hepagent-go`'s Short RTP/RTCP Report
pipeline and appear in Homer as media-quality timeline entries correlated
to the SIP call.

### Raw RTCP mode (default when reporting is on)

```
-send_media_report=true -hep_raw_rtcp=true
```

`Gossipper` accumulates per-SSRC RTP counters and emits a synthetic
**binary RTCP Sender Report** every **5 seconds** per active stream:

- **HEP type 5** — raw binary RTCP. The payload is a standard 28-byte
  RTCP SR (no Report Blocks). The NTP timestamp is set to the wall clock
  at emit time. Packet count and octet count are the values accumulated
  since the start of the stream.

This payload is processed by `hepagent-go`'s `RTCPConverter`, which
correlates streams by source/destination IP+port, computes jitter,
MOS estimates, and R-factor, and publishes the results through the
standard RTCP analytics pipeline.

---

## HEP protocol types used

| HEP type | Decimal | Description | Consumer |
|----------|---------|-------------|----------|
| `0x01` | 1 | SIP message | Homer SIP viewer |
| `0x05` | 5 | Raw binary RTCP SR | hepagent-go `RTCPConverter` |
| `0x23` | 35 | Short RTP Report (JSON) | hepagent-go short-report pipeline |
| `0x25` | 37 | Short RTCP Report (JSON) | hepagent-go short-report pipeline |
| `0x64` | 100 | DTMF Report (JSON) | hepagent-go DTMF pipeline |

SIP (type 1) is always forwarded when `-hep_addr` is configured, regardless
of the `-send_media_report` setting.

---

## Aggregation and intervals

Reports are emitted on a time-based ticker, not per-packet. Sending one
report per RTP packet would flood the collector with hundreds of events per
second per call; the ticker approach limits output to a predictable,
configurable rate.

The HEP client maintains an in-process map keyed by SSRC. `SendRTP` updates
the counters atomically on every call; the background goroutine reads the
accumulated state on each tick and emits the report.

Streams that have not received any new RTP packets for more than 60 seconds
are pruned from the map automatically.

### What is included in each report

**Raw RTCP SR (type 5):**

```
Byte layout (28 bytes, no Report Blocks):
  [0]     0x80       V=2, Padding=0, RC=0
  [1]     0xC8       PT=200 (Sender Report)
  [2-3]   0x0006     Length field (word count - 1)
  [4-7]   SSRC       parsed from the last RTP packet header
  [8-15]  NTP time   64-bit NTP timestamp (wall clock at emit time)
  [16-19] RTP ts     last RTP timestamp seen in this stream
  [20-23] pkt count  cumulative packet count since stream start
  [24-27] oct count  cumulative octet count since stream start
```

**JSON RTP report (type 35) fields:**

| JSON key | Source |
|----------|--------|
| `CORRELATION_ID` | SIP `Call-ID` |
| `RTP_SIP_CALL_ID` | SIP `Call-ID` |
| `SSRC` | hex string, e.g. `"0x1a2b3c4d"` |
| `PACKET_COUNT` | cumulative RTP packets sent |
| `OCTET_COUNT` | cumulative RTP payload octets |
| `REPORT_NAME` | `"<srcIP>-<srcPort>"` |
| `SOURCE` | `"GOSSIPPER"` |
| `TYPE` | `"PERIODIC"` |
| `REPORT_TS` | Unix millisecond timestamp of the report |
| `SRC_IP` | source IP of the RTP stream |
| `SRC_PORT` | source port |
| `DST_IP` | destination IP |
| `DST_PORT` | destination port |
| `RTP_TS` | last RTP timestamp from packet header |
| `CODEC_PT` | RTP payload type (0=PCMU, 8=PCMA, etc.) |
| `CLOCK` | clock rate (8000 for audio, 90000 for video) |
| `CODEC_NAME` | codec name, e.g. `"PCMU/8000"` |

---

## Collector requirements

- Homer 7 / hepic backed by a version of `hepagent-go` that includes the
  `RTCPConverter` component.
- For type 5 payloads: the binary RTCP SR must be at least 28 bytes.
  `hepagent-go` silently drops shorter payloads.
- For type 35/37 payloads: the JSON must be valid and include at least
  `CORRELATION_ID`. Missing optional fields are tolerated by the pipeline.
- The HEP capture ID (`-hep_capture_id`) should match the node ID
  configured in Homer so that media reports are associated with the correct
  capture agent.

---

## DTMF reporting

In JSON mode (`-send_media_report=true -hep_raw_rtcp=false`), `Gossipper`
detects RFC 2833 `telephone-event` RTP packets sent during a call and
reports them as a **HEP type 100** `DTMFReport` JSON message.

### Detection

`telephone-event` packets carry a 4-byte payload:

```
Byte 0:  event code (bit 7 = End-of-Event flag, bits 6-0 = digit code)
           0-9 → digits 0-9,  10 → *,  11 → #,  12-15 → A-D
Byte 1:  reserved/volume (bits 5-0 = volume in dBm0)
Byte 2-3: duration (big-endian, in clock-rate units, e.g. 8000 Hz)
```

Each digit generates three RTP packets (start, continuation, end). Only
the end-of-event packet (bit 7 of byte 0 set) is recorded to avoid
duplicates.

### Accumulation and flush

Detected events are stored per-SSRC alongside the RTP statistics.
On every 10-second tick the `reportLoop` goroutine flushes all accumulated
events as a single `DTMFReport` per stream, then clears the buffer.

### DTMFReport JSON fields

| JSON key | Value |
|----------|-------|
| `CORRELATION_ID` | SIP `Call-ID` |
| `REPORT_TS` | Unix millisecond timestamp of the report |
| `DTMF` | Semicolon-separated event strings (see format below) |
| `SRC_IP` | Source IP of the RTP stream |
| `SRC_PORT` | Source port |
| `DST_IP` | Destination IP |
| `DST_PORT` | Destination port |
| `CODEC_PT` | RTP payload type (usually `101`) |
| `CODEC_NAME` | `"telephone-event"` |
| `PARTY` | `1` |
| `STYPE` | `"GOSSIPPER-DTMF"` |
| `TYPE` | `"PERIODIC"` |

Each entry in the `DTMF` string has the format:

```
ts:<unix_sec>,tsu:<unix_usec>,e:<digit_code>,v:<volume>,d:<duration>,c:1
```

Multiple events are joined with `;`.

Example `DTMF` value for digits `1` then `2`:

```
ts:1741987200,tsu:123456,e:1,v:0,d:12000,c:1;ts:1741987201,tsu:456789,e:2,v:0,d:12000,c:1
```

### Raw RTCP mode and DTMF

In raw RTCP mode (`-hep_raw_rtcp=true`) `telephone-event` packets are
included in the per-SSRC packet/octet counters but no separate DTMF report
is generated. In this mode `hepagent-go` detects DTMF directly from the
RTCP stream it processes.

---

## Known limits

- **No jitter measurement.** `Gossipper` is a pure sender for its own RTP
  streams. It does not receive its own packets back on the same path, so
  it cannot compute inter-arrival jitter for the outbound stream.
- **Packet loss is reported as 0** in JSON mode. As a sender, `Gossipper`
  has no visibility into whether the remote side received every packet.
- **Incoming RTCP Receiver Reports** from the remote leg are received and
  counted in the session stats (`rtcp_rr` counter) but are not forwarded
  to the HEP collector.
- **SSRC changes.** If the SSRC rotates mid-call (rare in practice), the
  old SSRC entry is replaced in the map and the old stream's counters are
  lost.
- **Server-side `uas` scenarios** start RTP echo sessions. Their streams
  are tracked and reported the same way as UAC streams.

---

## Examples

### SIP-only HEP mirroring (no media reports)

```sh
gossipper -sn uac -hep_addr 127.0.0.1:9060 -hep_capture_id 100 \
  -r 10 -m 1000 192.168.1.1:5060
```

SIP messages are mirrored. No RTP or RTCP is sent to HEP.

### Raw RTCP SR reports every 5 seconds (recommended for Homer/hepic)

```sh
gossipper -sn uac \
  -hep_addr 127.0.0.1:9060 \
  -hep_capture_id 100 \
  -send_media_report=true \
  -hep_raw_rtcp=true \
  -r 10 -m 1000 192.168.1.1:5060
```

Binary RTCP SR (type 5) is emitted every 5 seconds per active stream.
Processed by `hepagent-go` `RTCPConverter`. This is the default when
`-send_media_report=true` because `-hep_raw_rtcp` defaults to `true`.

### JSON RTP + RTCP reports every 10 seconds

```sh
gossipper -sn uac \
  -hep_addr 127.0.0.1:9060 \
  -hep_capture_id 100 \
  -send_media_report=true \
  -hep_raw_rtcp=false \
  -r 10 -m 1000 192.168.1.1:5060
```

JSON type 35 (RTP), type 37 (RTCP), and type 100 (DTMF, when digits are sent)
reports are emitted every 10 seconds, correlated to the SIP `Call-ID`.
Use this mode if the Homer instance is configured to consume short RTP/RTCP
JSON reports rather than raw RTCP.
