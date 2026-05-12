# Summary JSON (`-summary_json`)

## Schema version

The `schema_version` field is set to `gossipper_summary_v1` when writing the file via `-summary_json`. Consumers should treat unknown schema versions as incompatible.

## Tool version

`tool_version` is populated from the running binary (same string as `gossipper -version` short line).

## Per-call rows (`calls`)

When SIP scenarios complete calls through the engine, the summary may include a **`calls`** array: one object per finished call with `call_number`, `call_id`, `success`, `result` (failure class or `success`), `duration`, optional `invite_rtt`, and **`media`** counters for that call only (same shape as top-level `media` but not aggregated across calls).

## Media QoS (RTCP)

When RTCP Receiver Reports are received during RTP sessions, the summary may include:

- `media.rtcp_reception_reports` — count of RFC 3550 reception report blocks.
- `media.rtcp_max_fraction_lost` — maximum observed loss fraction in `0..1`.
- `media.rtcp_max_jitter_ts` / `media.rtcp_avg_jitter_ts` — jitter in RTP timestamp units (not milliseconds).

## Health checks (CI)

Optional flags (evaluated when **`-summary_json`**, **`-summary_html`**, or any health threshold is set — the run finalizes a summary in that case):

- `-health_min_success_ratio` — fail if `success_ratio` is below this value (e.g. `0.99`).
- `-health_max_failed_calls` — fail if `failed_calls` exceed this value; `0` means any failure fails the run. Default `-1` disables.
- `-health_max_timeouts` — fail if `timeouts` exceed this value. Default `-1` disables.

On failure the process exits with code **2** (other errors use **1**). The JSON file still contains `health` and `findings` with failure reasons.

## HTML report

- **`-summary_html PATH`**: after a SIP run, writes the same finalized summary as a **standalone** UTF-8 HTML file (no CDN, works offline). Can be used **without** `-summary_json`.
- **`gossipper report-html -in summary.json -out report.html`**: rebuild HTML from an existing summary JSON file.
- **`gossipper report-pdf -in report.html|summary.json -out report.pdf`**: print to PDF via headless Chromium/Chrome (`--print-to-pdf`). For `.json` input, HTML is rendered to a temp file first.

See implementation in `internal/reporthtml` and `internal/reportpdf`.
