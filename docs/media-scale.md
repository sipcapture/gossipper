# High-scale cleartext RTP (`-media_scale`)

**Full documentation (English):** [`high-scale-cleartext-rtp.md`](high-scale-cleartext-rtp.md) — architecture, rationale (XDP vs userspace), tuning, acceptance criteria, and tests.

Gossipper can generate many parallel **cleartext synthetic** RTP streams without one goroutine and one `time.Ticker` per stream. Use this for load tests toward a single SBC or media gateway (for example 10k PCMU streams at 20 ms).

## When to use

| Use `-media_scale` | Do not use |
|--------------------|------------|
| `rtp_stream="synthetic,…"` in scenarios | SRTP / DTLS (`-media_srtp`) |
| `-rtp_send` with optional `-rtp_streams N` | PCAP, mic, echo, `rtpcheck` |
| Send-only toward one or few peers | Per-packet HEP RTP mirror on hot path |

This path does **not** use XDP/eBPF. Throughput is improved by a central scheduler, buffer reuse, and batched UDP send (`sendmmsg` on Linux).

## Flags

| Flag | Description |
|------|-------------|
| `-media_scale` | Enable the scale RTP engine |
| `-media_iouring` | With `-media_scale`: send RTP batches from the scheduler (Linux `sendmmsg`; fewer goroutines). Or `GOSSIPPER_MEDIA_IOURING=1` |
| `-rtp_streams N` | With `-rtp_send -media_scale`, start **N** parallel streams (local ports step by 2) |

`-media_scale` cannot be combined with `-media_srtp`.

## Standalone example

```bash
# 1000 PCMU streams to one SBC (lab)
gossipper sipp -rtp_send -media_scale -rtp_streams 1000 \
  -rtp_addr 10.0.0.5:30000 -rtp_codec PCMU/8000 -rtp_freq 20 -i 10.0.0.1
```

## Standard scenario (SIP + 10k RTP)

```bash
gossipper sipp -sn invite_media_scale -rsa SBC:5060 -i LOCAL -p 5060 -m 10000 -l 10000 -r 20
```

`-media_scale` is turned on automatically for `-sn invite_media_scale`. See [`high-scale-cleartext-rtp.md`](high-scale-cleartext-rtp.md).

## Custom scenario example

```xml
<exec rtp_stream="synthetic,0,0,PCMU/8000,20"/>
```

Run with `-media_scale` (or use the built-in name above). Remote RTP address comes from SDP (`ParseAudioEndpoint`).

## OS tuning

Before large runs (especially 5k–10k streams):

1. **File descriptors:** `ulimit -n` ≥ 20000 (one UDP socket per stream plus SIP).
2. **UDP buffers:** apply sysctl values similar to Homer:

```bash
sudo sysctl -w net.core.rmem_max=33554432
sudo sysctl -w net.core.wmem_max=33554432
sudo sysctl -w net.core.wmem_default=4194304
sudo sysctl -w net.core.netdev_max_backlog=250000
```

Or use [`scripts/tune-udp-sysctl.sh`](scripts/tune-udp-sysctl.sh) / [`examples/sysctl/gossipper-high-scale.conf`](examples/sysctl/gossipper-high-scale.conf).

3. **CPU:** set `GOMAXPROCS` to the number of physical cores on the generator host.

## Acceptance checks

- `summary` / JSON: `media.rtp_packets_sent` ≈ `streams × (1000/freq_ms) × duration_seconds`
- Process goroutine count stays **O(workers)**, not O(streams)
- No sustained `sendto` errors on the generator (`dmesg`, `ss -s`)

## Architecture

See [`internal/media/scale_engine.go`](../internal/media/scale_engine.go): min-heap scheduler (1 ms tick), sender worker pool, per-stream UDP socket bound to `[media_port]`.
