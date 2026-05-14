# Summary JSON (`-summary_json`)

## Schema version

The `schema_version` field is set to `gossipper_summary_v1` when writing the file via `-summary_json`. Consumers should treat unknown schema versions as incompatible.

## Tool version

`tool_version` is populated from the running binary (same string as `gossipper -version` short line).

## Media QoS (RTCP)

When RTCP Receiver Reports are received during RTP sessions, the summary may include:

- `media.rtcp_reception_reports` — count of RFC 3550 reception report blocks.
- `media.rtcp_max_fraction_lost` — maximum observed loss fraction in `0..1`.
- `media.rtcp_max_jitter_ts` / `media.rtcp_avg_jitter_ts` — jitter in RTP timestamp units (not milliseconds).
- `media.rtcp_min_jitter_ts` — minimum non-zero jitter among sampled RR blocks (timestamp units).
- `media.media_calls_with_rtp_received` — how many finished calls saw at least one inbound RTP packet.
- `media.per_call_min_rtp_packets_received` — minimum of per-call `rtp_packets_received` among calls that had inbound RTP (weakest call).

## Health checks (CI)

Optional flags (evaluated when **`-summary_json`**, **`-summary_html`**, or any health threshold is set — the run finalizes a summary in that case):

- `-health_min_success_ratio` — fail if `success_ratio` is below this value (e.g. `0.99`).
- `-health_max_failed_calls` — fail if `failed_calls` exceed this value; `0` means any failure fails the run. Default `-1` disables.
- `-health_max_timeouts` — fail if `timeouts` exceed this value. Default `-1` disables.

On failure the process exits with code **2** (other errors use **1**). The JSON file still contains `health` and `findings` with failure reasons when `-summary_json` is set.

### Media thresholds (aggregate summary)

When **`-summary_json`**, **`-summary_html`**, or any health flag is set, these
optional gates apply to **aggregated** `media` counters across all calls:

- `-health_max_rtcp_fraction_lost` — fail if `media.rtcp_max_fraction_lost` exceeds this value (0..1, e.g. `0.05`).
- `-health_max_rtcp_jitter_ts` — fail if `media.rtcp_max_jitter_ts` exceeds this integer (RTP timestamp units).
- `-health_min_rtp_packets_recv` — fail if `media.rtp_packets_received` is **below** this aggregate count.
- `-health_min_rtp_packets_recv_per_call` — fail if `media.per_call_min_rtp_packets_received` is below this value, or if no call had inbound RTP when the gate is enabled.

These media checks use the same exit code **2** on failure when health evaluation runs.

## HTML report

- **`-summary_html PATH`**: after a SIP run, writes the same finalized summary as a **standalone** UTF-8 HTML file (no CDN, works offline). Can be used **without** `-summary_json`.
- **`gossipper report-html -in summary.json -out report.html`**: rebuild HTML from an existing summary JSON file.
- **`gossipper summary-to-pdf -in report.html -out report.pdf`**: tries an **embedded chromedp** renderer when the binary was built with **`-tags pdf`**; otherwise falls back to Chromium/Chrome in `PATH` (`--print-to-pdf`).

See implementation in `internal/reporthtml`.
