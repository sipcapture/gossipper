# QoS media reporting

`Gossipper` can mirror RTP and RTCP quality statistics to a Homer
collector via HEP3. This allows call-quality metrics to appear alongside SIP
traces in the Homer UI without a separate network capture agent.

Reporting is **disabled by default**. It must be explicitly enabled with
`-send_media_report=true` and requires `-hep_addr` to point at a running
HEP3 collector.

The **open-source** build supports **raw binary RTCP SR** (HEP type 5) or
**Homer-Lake JSON** on type 5. Any other HEP media export path requires a
compile-time `mediasink` extension (see `mediasink/README.md`). With
`-send_media_report=true`, use `-hep_homer_lake_rtcp` or keep `-hep_raw_rtcp=true`
unless such an extension is linked into the binary.

---

## CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `-hep_addr` | `""` | HEP3 collector address (`host:port`). Required for any HEP output. |
| `-hep_capture_id` | `0` | HEP3 capture node ID sent in every encapsulated packet. |
| `-hep_password` | `""` | Optional HEP3 auth key. |
| `-hep_raw_rtcp` | `true` | When reporting is on and Homer-Lake is off: periodic **binary** RTCP SR on HEP type 5; also forwards each RTCP packet immediately as raw type 5. |
| `-hep_homer_lake_rtcp` | `false` | Homer-Lake: HEP type 5 with **JSON** RTCP SR body every 5s. Takes precedence over `-hep_raw_rtcp`. |
| `-send_media_report` | `false` | Enables periodic QoS reporting. When `false`, nothing is sent to HEP for RTP or RTCP. |
| `-recv_bye_timeout_ms` | `90000` | Minimum wait (ms) for mandatory `<recv request="BYE"/>` when the scenario omits `timeout` (pairs with long UAC media pauses). Set to `0` to disable and use only `-recv_timeout_ms`. |

If `-send_media_report=true` with **both** `-hep_raw_rtcp=false` and
`-hep_homer_lake_rtcp=false`, the CLI rejects startup unless a `mediasink`
media exporter extension was linked into the binary.

---

## Operating modes

### Disabled (default)

```
-send_media_report=false
```

No RTP or RTCP data is sent to the HEP collector. SIP messages are still
mirrored if `-hep_addr` is set.

### Raw RTCP mode (default when reporting is on)

```
-send_media_report=true -hep_raw_rtcp=true
```

(`-hep_homer_lake_rtcp` must be `false`, which is the default.)

`Gossipper` accumulates per-SSRC RTP counters and emits a synthetic
**binary RTCP Sender Report** every **5 seconds** per active stream. Each
inbound RTCP packet can also be mirrored **immediately** as raw type 5.

- **HEP type 5** — raw binary RTCP. The periodic payload is a standard 28-byte
  RTCP SR (no Report Blocks). The NTP timestamp is set to the wall clock
  at emit time. Packet count and octet count are the values accumulated
  since the start of the stream.

This payload is processed by `hepagent-go`'s `RTCPConverter`, which
correlates streams by source/destination IP+port, computes jitter,
MOS estimates, and R-factor, and publishes the results through the
standard RTCP analytics pipeline.

### Homer-Lake JSON on HEP type 5

```
-send_media_report=true -hep_homer_lake_rtcp=true
```

When `-hep_homer_lake_rtcp=true`, `Gossipper` still uses **HEP proto_type 5**, but the **payload is JSON** (not the 28-byte binary SR). The object matches the Homer injector shape: `type` **200** (RTCP SR), `ssrc`, `report_count`, `report_blocks` (empty when gossipper has no RR), and `sender_information` with string NTP fields and packet/octet counts. Emitted every **5 seconds** per active SSRC while RTP is flowing. The HEP header uses the **same source/destination IP and UDP ports as the RTP stream** (not zeroed).

This mode is mutually exclusive with the binary SR ticker; `-hep_homer_lake_rtcp` takes precedence when enabled.

---

## HEP protocol types used

| HEP type | Decimal | Description | Consumer |
|----------|---------|-------------|----------|
| `0x01` | 1 | SIP message | Homer SIP viewer |
| `0x05` | 5 | RTCP on type 5: **binary** SR (`-hep_raw_rtcp=true`, Homer-Lake off) **or** Homer-Lake **JSON** SR (`-hep_homer_lake_rtcp=true`) | `RTCPConverter` / Homer-Lake |

SIP (type 1) is always forwarded when `-hep_addr` is configured, regardless
of the `-send_media_report` setting.

---

## Aggregation and intervals

Reports are emitted on a time-based ticker, not per-packet. Sending one
report per RTP packet would flood the collector with hundreds of events per
second per call; the ticker approach limits output to a predictable,
configurable rate.

The media exporter maintains an in-process map keyed by SSRC. `SendRTP` updates
the counters on every call; the background goroutine reads the
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
  [16-19] RTP ts     wall-clock-derived 32-bit RTP timestamp (implementation uses `now` nanoseconds / 125)
  [20-23] pkt count  cumulative packet count since stream start
  [24-27] oct count  cumulative octet count since stream start
```

**Homer-Lake JSON (type 5)** uses the keys `type`, `ssrc`, `report_count`,
`report_blocks`, and nested `sender_information` (packets, NTP string fields,
`rtp_timestamp`, octets). See tests in `mediasink`.

---

## Collector requirements

- Homer 7 backed by a version of `hepagent-go` that includes the
  `RTCPConverter` component.
- For type 5 binary payloads: the RTCP SR must be at least 28 bytes.
  `hepagent-go` silently drops shorter payloads.
- For Homer-Lake JSON on type 5: the JSON must be valid and include the
  fields expected by your Homer-Lake injector.
- The HEP capture id (`-hep_capture_id`) should match the node ID
  configured in Homer so that media reports are associated with the correct
  capture agent.

---

## Known limits

- **No jitter measurement.** `Gossipper` is a pure sender for its own RTP
  streams. It does not receive its own packets back on the same path, so
  it cannot compute inter-arrival jitter for the outbound stream.
- **Packet loss** in aggregated SR may reflect values merged from observed
  RTCP when available; otherwise treat as indicative only.
- **Incoming RTCP Receiver Reports** from the remote leg are received and
  counted in the session stats (`rtcp_rr` counter) but are not always forwarded
  to the HEP collector unchanged (raw mode forwards `SendRTCP` payloads).
- **SSRC changes.** If the SSRC rotates mid-call (rare in practice), the
  old SSRC entry is replaced in the map and the old stream's counters are
  lost.
- **Server-side `uas` scenarios** start RTP echo sessions. Their streams
  are tracked and reported the same way as UAC streams.

---

## Examples

### SIP-only HEP mirroring (no media reports)

```sh
gossipper sipp -sn uac -hep_addr 127.0.0.1:9060 -hep_capture_id 100 \
  -r 10 -m 1000 192.168.1.1:5060
```

SIP messages are mirrored. No RTP or RTCP is sent to HEP.

### Raw RTCP SR reports every 5 seconds (recommended for Homer)

```sh
gossipper sipp -sn uac \
  -hep_addr 127.0.0.1:9060 \
  -hep_capture_id 100 \
  -send_media_report=true \
  -hep_raw_rtcp=true \
  -r 10 -m 1000 192.168.1.1:5060
```

Binary RTCP SR (type 5) is emitted every 5 seconds per active stream.
Processed by `hepagent-go` `RTCPConverter`. This is the default when
`-send_media_report=true` because `-hep_raw_rtcp` defaults to `true`.

### Homer-Lake JSON on type 5 every 5 seconds

```sh
gossipper sipp -sn uac \
  -hep_addr 127.0.0.1:9060 \
  -hep_capture_id 100 \
  -send_media_report=true \
  -hep_homer_lake_rtcp=true \
  -r 10 -m 1000 192.168.1.1:5060
```

JSON RTCP SR shape on HEP proto 5 for Homer-Lake injectors.
