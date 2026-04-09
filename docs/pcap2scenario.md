# pcap2scenario — Generate Scenarios from a PCAP File

`gossipper pcap2scenario` reads a PCAP file containing SIP+RTP traffic and
produces a pair of ready-to-replay scenarios (UAC + UAS) together with the
extracted RTP streams.

---

## Quick Start

```bash
gossipper pcap2scenario call.pcap
```

With explicit options:

```bash
gossipper pcap2scenario call.pcap -out ./scenarios -sip-port 5060
```

---

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `<file.pcap>` | — | Input PCAP file (required) |
| `-out <dir>` | `.` (current directory) | Output directory for generated files |
| `-sip-port <port>` | `0` (auto) | SIP port; `0` enables heuristic detection |

---

## Output Files

After a successful run the output directory contains:

| File | Description |
|------|-------------|
| `scenario_uac.xml` | Caller-side scenario (client mode) |
| `scenario_uas.xml` | Callee-side scenario (server mode) |
| `caller_rtp.pcap` | RTP packets from the caller's media stream |
| `callee_rtp.pcap` | RTP packets from the callee's media stream |

---

## Running the Generated Scenarios

### UAC (caller side)

```bash
gossipper -scenario scenarios/scenario_uac.xml \
          -d 192.168.1.20 \
          -p 5060 \
          -l 192.168.1.10 \
          -r 1
```

### UAS (callee side)

```bash
gossipper -scenario scenarios/scenario_uas.xml \
          -l 192.168.1.20 \
          -p 5060 \
          -r 1
```

> The `caller_rtp.pcap` / `callee_rtp.pcap` files must reside in the same
> directory from which gossipper is run, or in the path where it resolves
> scenario resources.

---

## Processing Pipeline

```
call.pcap
    │
    ▼
┌─────────────────────────────────────────┐
│  Extractor                              │
│  • SIP over UDP  → sip.Parse()          │
│  • SIP over TCP  → tcpassembly +        │
│                    sip.ReadMessage()    │
│  • all UDP frames retained for RTP      │
└───────────────┬─────────────────────────┘
                │
    ┌───────────▼──────────┐
    │  Dialog Builder      │
    │  INVITE → 200 → ACK  │
    │  BYE    → 200        │
    │  SDP: RTP IP + ports │
    └───────────┬──────────┘
                │
    ┌───────────▼──────────────────────────────┐
    │  RTP Split                               │
    │  filter by callerRTPPort → caller_rtp.pcap│
    │  filter by calleeRTPPort → callee_rtp.pcap│
    └───────────┬──────────────────────────────┘
                │
    ┌───────────▼──────────────────────────────┐
    │  Generator                               │
    │  scenario_uac.xml  (templatised SIP)     │
    │  scenario_uas.xml  ([last_Via:] pattern) │
    └──────────────────────────────────────────┘
```

---

## SIP Message Templatisation

Concrete values from the capture are replaced with gossipper template
variables:

| PCAP value | Scenario variable |
|---|---|
| Caller IP | `[local_ip]` |
| Callee IP | `[remote_ip]` |
| Caller SIP port | `[local_port]` |
| Callee SIP port | `[remote_port]` |
| Call-ID | `[call_id]` |
| Via branch | `[branch]` |
| Via transport | `[transport]` |
| From tag | `[pid]GossipTag00[call_number]` |
| To tag (ACK/BYE) | `[peer_tag_param]` |
| SDP `c=` address | `[local_ip]` |
| SDP `m=audio <port>` | `[media_port]` |
| Content-Length | `[len]` (recalculated at send time) |
| CSeq number | preserved as-is |
| All other headers | preserved as-is |

---

## Generated UAC Scenario Example

```xml
<?xml version="1.0" encoding="UTF-8"?>
<scenario name="pcap-uac (from call.pcap)">

  <send retrans="500">
    <![CDATA[
    INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
    Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
    From: "Alice" <sip:alice@[local_ip]:[local_port]>;tag=[pid]GossipTag00[call_number]
    To: "Bob" <sip:bob@[remote_ip]:[remote_port]>
    Call-ID: [call_id]
    CSeq: 1 INVITE
    Contact: <sip:alice@[local_ip]:[local_port];transport=[transport]>
    Content-Type: application/sdp
    Content-Length: [len]

    v=0
    o=- 0 0 IN IP4 [local_ip]
    s=-
    c=IN IP4 [local_ip]
    t=0 0
    m=audio [media_port] RTP/AVP 0 8
    a=rtpmap:0 PCMU/8000
    a=rtpmap:8 PCMA/8000
    a=sendrecv
    ]]>
  </send>

  <recv response="100" optional="true"/>
  <recv response="180" optional="true"/>

  <!-- On 200 OK, start replaying caller RTP immediately -->
  <recv response="200" rtd="true">
    <action>
      <exec play_pcap_audio="caller_rtp.pcap"/>
    </action>
  </recv>

  <send>
    <![CDATA[
    ACK sip:[service]@[remote_ip]:[remote_port] SIP/2.0
    ...
    ]]>
  </send>

  <!-- Pause matches the original call duration from the PCAP -->
  <pause milliseconds="5000"/>

  <nop>
    <action><exec rtp_stream="stop"/></action>
  </nop>

  <send retrans="500">
    <![CDATA[
    BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
    ...
    ]]>
  </send>

  <recv response="200"/>

</scenario>
```

---

## Generated UAS Scenario Example

```xml
<?xml version="1.0" encoding="UTF-8"?>
<scenario name="pcap-uas (from call.pcap)">

  <recv request="INVITE"/>

  <!-- 180 Ringing — headers copied from the received INVITE -->
  <send>
    <![CDATA[
    SIP/2.0 180 Ringing
    [last_Via:]
    [last_From:]
    [last_To:];tag=[pid]GossipTag01[call_number]
    [last_Call-ID:]
    [last_CSeq:]
    Contact: <sip:[local_ip]:[local_port];transport=[transport]>
    Content-Length: 0
    ]]>
  </send>

  <!-- 200 OK with SDP — codec list taken from the original PCAP -->
  <send retrans="500">
    <![CDATA[
    SIP/2.0 200 OK
    [last_Via:]
    [last_From:]
    [last_To:];tag=[pid]GossipTag01[call_number]
    [last_Call-ID:]
    [last_CSeq:]
    Contact: <sip:[local_ip]:[local_port];transport=[transport]>
    Content-Type: application/sdp
    Content-Length: [len]

    v=0
    o=- 0 0 IN IP4 [local_ip]
    s=-
    c=IN IP4 [local_ip]
    t=0 0
    m=audio [media_port] RTP/AVP 0 8
    a=rtpmap:0 PCMU/8000
    a=rtpmap:8 PCMA/8000
    a=sendrecv
    ]]>
  </send>

  <!-- On ACK, start replaying callee RTP -->
  <recv request="ACK">
    <action>
      <exec play_pcap_audio="callee_rtp.pcap"/>
    </action>
  </recv>

  <pause milliseconds="5000"/>

  <nop>
    <action><exec rtp_stream="stop"/></action>
  </nop>

  <recv request="BYE"/>

  <send>
    <![CDATA[
    SIP/2.0 200 OK
    [last_Via:]
    [last_From:]
    [last_To:];tag=[pid]GossipTag01[call_number]
    [last_Call-ID:]
    [last_CSeq:]
    Content-Length: 0
    ]]>
  </send>

</scenario>
```

---

## Feature Support

| Feature | Support |
|---|---|
| SIP over UDP | ✅ |
| SIP over TCP (with stream reassembly) | ✅ |
| RTP audio (`m=audio`) | ✅ |
| IPv4 | ✅ |
| libpcap format (`.pcap`) | ✅ |
| IPv6 | ⚠️ Parsed, but address templatisation is untested |
| Multiple calls in one PCAP | ⚠️ First Call-ID found is used |
| Re-INVITE / hold / transfer | ❌ Not supported in v1 |
| 407/401 authentication challenge | ❌ Not supported in v1 |
| SRTP | ❌ Not supported in v1 |
| Video RTP (`m=video`) | ❌ Audio only |

---

## SIP Detection

When `-sip-port` is not set (value `0`), SIP datagrams are identified
heuristically by the start of the UDP payload:

```
INVITE   ACK   BYE   CANCEL   OPTIONS   REGISTER
NOTIFY   SUBSCRIBE   PUBLISH   REFER   INFO   UPDATE   PRACK
SIP/2.0 ...
```

If SIP runs on a non-standard port, specify it explicitly to avoid false
positives and speed up extraction:

```bash
gossipper pcap2scenario call.pcap -sip-port 5080
```

---

## Related Documentation

- [RTP in scenarios](rtp-in-scenarios.md) — using `play_pcap_audio` and `rtp_stream` actions
- [Synthetic RTP sender](synthetic-rtp-sender.md) — generating silence frames without a PCAP file
