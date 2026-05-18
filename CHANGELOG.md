# Changelog

All notable changes to this project are documented in this file.

## [Unreleased]

## [0.1.42] — 2026-05-18

### Added

- **Admin console Phase 5.2**: dedicated **Audit** nav page, global toast notifications, runtime settings panel (`GET /api/v2/settings` — `ui_data_dir`, `scenario_history_keep`, disk usage).
- **Built-in scenarios API**: `GET /api/v2/builtin-scenarios` and `GET /api/v2/builtin-scenarios/{id}` expose engine-baked XML (`uac`, `uas`, `management`, `invite_media*`) read-only in the UI.
- **ScenarioSelect** component with role-aware filtering (UAS/UAC) in Servers, Clients, and Jobs start forms; built-in scenario XML viewer on the Scenarios page.
- **Jobs live feed**: Jobs page subscribes to `/api/v2/live` WebSocket instead of polling; restart button for failed/stopped jobs; stats sparkline from `stats.jsonl`.
- **Dashboard 24h timeline** chart for succeeded/failed job outcomes.
- **Scenario history**: side-by-side diff mode, media alias picker (`[[media:wav/…]]`), missing-media validation, restore-overwrite action.
- **Media library**: drag-and-drop upload, reverse-reference column (“Used in scenarios”).
- **Profile Duplicate** action on Servers and Clients; **hard block Start** when cross-profile port conflicts are detected.

### Changed

- Port-conflict badges now use cross-profile checks (`server:*` / `client:*` prefixes) on Servers, Clients, Dashboard, and Jobs.
- About page corrected (Audit lives on its own nav item); Settings roadmap updated for Phase 5.2.

### WebRTC (UI prep)

- Transport editor warns when `webrtc` is enabled without ICE servers; running jobs show a WebRTC badge when the profile uses a webrtc transport row.

## [0.1.41] — 2026-05-18

### Added

- **Admin console (`/api/v2/*`) on `gossipper server`**: the management process now optionally mounts the full admin console REST surface when `ui_data_dir` is set in the management JSON (auto-disables `/api/v1` unless `legacy_api_v1: true`). Packaged `examples/gossipper-server.sample.json` ships with `ui_data_dir` pre-set so a default install gets the console out of the box.
- **Profile seeding from management config**: on first start, `cfg.Server` and every `cfg.JoinedClients[i]` are imported into the UI store as `ServerProfile` / `ClientProfile` records (tagged `source: "built-in"`) so operators see what they already configured rather than empty Servers/Clients pages. Seeding is one-shot — hand-edits and UI-created profiles are never overwritten on restart.
- **Runtime status column on Servers & Clients**: list endpoints embed a derived `runtime` object (`built-in` / `running` / `pending` / `succeeded` / `failed` / `stopped` / `idle`) sourced from `supervisor.JobsStore`. The UI renders a colour-coded `RuntimeBadge` with a tooltip exposing job id, pid, exit code and a relative "started Xs ago". The page also auto-polls every 3 s so background state changes promote without operator interaction.
- **Built-in scenario fallback in supervisor workers**: `BuildConfigFromSpec` now consults `scenario.LoadNamed` when a profile references a scenario name (`management`, `uac`, `uas`, `invite_media*` …) that isn't separately imported into uistore — the worker uses `cfg.ScenarioName` and lets the engine resolve the baked XML.

### Changed

- `POST /api/v2/jobs` and `/api/v2/{servers,clients}/{id}/start` refuse to fork a supervisor worker for `source: "built-in"` profiles (would collide on bind with the master). Returns **409 Conflict** with an explanation; the UI disables Start/Stop/Delete buttons for these rows.
- `POST /api/v2/{servers,clients}/{id}/stop` now distinguishes "profile does not exist" (404) from "profile exists but has no supervisor job" (409 with a hint that the profile is owned by the management process). Previously every miss became a confusing 404.
- Start/Stop handlers in the Servers/Clients pages call `refresh()` in `finally` so the status column updates even when the request errored.

### Diagnostics

- Launcher prints a one-line warning when the management API is up but the admin console isn't mounted, pointing at the `ui_data_dir` key to enable it.
- Startup log now reports which API surfaces are mounted on the management listener (`v1 + v2 (admin console)`, etc.).


### Added

- **`-media_srtp`**: optional SDES SRTP for `rtp_stream start` / `rtp_stream mic` when the remote SDP contains `a=crypto` with `inline:` (AES_CM_128_HMAC_SHA1_80 / AES_CM_128_HMAC_SHA1_32). Outbound RTP is encrypted and inbound RTP decrypted via `github.com/pion/srtp/v3`. DTLS-SRTP (`a=fingerprint` without SDES) is rejected with a clear error.
- **Summary / HTML report**: `media.rtp_recv_max_cumulative_lost` and `media.rtp_recv_interarrival_jitter_peak_ts` aggregate local RTP receive path quality (RFC 3550-style loss and interarrival jitter estimator peaks).

### Changed

- When remote SDP suggests SRTP and neither **`-media_reject_srtp`** nor **`-media_srtp`** is set, `rtp_stream start` / `mic` fail with an explicit hint to choose one of these flags (avoids silent cleartext toward an SRTP endpoint).

### Documentation

- `docs/srtp.md`, `docs/summary-json.md`, and HTML report template aligned with SRTP and new media counters.
