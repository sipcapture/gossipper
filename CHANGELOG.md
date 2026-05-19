# Changelog

All notable changes to this project are documented in this file.

## [Unreleased]

## [0.1.54] — 2026-05-20

### Added

- **WebRTC Phase 4.2 (partial)**: opt-in `<scenario webrtc="true">` uses per-call `webrtc.Bridge`; UAS `[webrtc_answer]` template keyword; synthetic `rtp_stream` over WebRTC; `ICEGatherTimeout` bridge option.

## [0.1.53] — 2026-05-19

### Added

- **E2E smoke** (`scripts/smoke-api-v2.sh`, `make smoke`): starts real `gossipper ui`, checks health/login/scenarios/RBAC and embedded UI; runs in CI after `make build-go`.

## [0.1.52] — 2026-05-19

### Added

- **RBAC enforcement**: admin-only routes (`/users`, `/audit`, `POST /settings/rotate-jwt-secret`) return **403** for non-`admin` JWT roles; new users default to **`operator`** when role is omitted.
- **Docs**: roles table and v1 per-engine control in `docs/ui-mode.md`; `import-from-pcap-job` in `docs/pcap2scenario.md`.

## [0.1.51] — 2026-05-19

### Added

- **Auto-import pcap2scenario**: `POST /api/v2/scenarios/import-from-pcap-job` reads `scenario_uac.xml` / `scenario_uas.xml` from a succeeded tool job; Scenarios UI one-click import.
- **JWT role claim** on login (`role` from SQLite users table); `GET /me` prefers token claim.
- **Per-engine v1 control**: `POST /api/v1/control` accepts `engine_id` / `id` to set rate/pause for one engine; Dashboard panel updated.

### Changed

- **CI** (`go.yml`): explicit `npm test`, full `go test ./...`, and `go test -race -short` on API/auth/store packages (engine/cmd soak tests still run without `-race` until existing races are fixed).

## [0.1.50] — 2026-05-19

### Added

- **Docs**: `docs/sipstress-style-load-testing.md` — Load test API (`POST /api/v2/load-test/run`), soak, UI features; `docs/ui-mode.md` and `web/control-ui/README.md` updated for v2 (no legacy UI fallback).
- **Tests**: soak load-test API (`total_calls=0`), `POST /api/v2/me/password`.

## [0.1.49] — 2026-05-19

### Added

- **Load test UI**: live monitor panel after start, localStorage presets, soak mode (`total_calls=0`), defaults from `GET /api/v2/load-test`.
- **Jobs UI**: kind filters (load test / tool / server / client), Stop all running, engine overrides on Start job, artifact Open/Download, report preview tab, auto-refresh recordings while running.
- **Reports UI**: summary KPI column, embedded HTML/summary preview, side-by-side compare, bulk ZIP export.
- **Dashboard**: clickable recent jobs, alerts (running load tests, failed spike, disk usage), hybrid **/api/v1** management panel (rate/pause/dynamic clients when available).
- **Scenarios**: pcap2scenario import panel; Prep tools expanded (`rtp_send`, `report-html`).
- **Auth/settings**: `GET /me` in header, role-based nav (hide Users/Audit for non-admin), `POST /api/v2/me/password`, session expiry on 401, hash deep links (`#/jobs/{id}`, `#/load`, `#/reports?report=`).

## [0.1.48] — 2026-05-19

### Added

- **Load test API**: `GET /api/v2/load-test` (schema/defaults) and `POST /api/v2/load-test/run` — starts a sipstress-style `invite_media` worker in the background (`202 Accepted` + job id). Status/stop via existing `GET/POST /api/v2/jobs/{id}` endpoints.
- **`internal/loadtest`**: shared server-side entry used by the API and admin UI wizard (`UpsertWizardProfile`, `Start`).

### Changed

- **Load test wizard** calls `POST /api/v2/load-test/run` instead of client upsert + generic `POST /jobs`.

## [0.1.47] — 2026-05-19

### Changed

- **Admin UI**: replaced misleading **Stress tools** catalog with **Load test** wizard (sipstress-style `invite_media` job: director, calls/CPS/concurrency, trunk identity, health gates, WAV).
- **Scenarios**: **Prep** panel for `pcap2scenario` and CSV `infindex` tool jobs (moved out of load-test nav).
- **Worker jobs**: engine overrides now accept `sip_from`, `sip_pai`, `sip_provider`, and health thresholds; jobs auto-write `report.html` alongside `summary.json`.

## [0.1.46] — 2026-05-19

### Fixed

- **`gossipper ui`**: mount embedded Control UI at **`GET /`** and **`/assets/*`** (same as `gossipper server -api_addr`); log a clear hint when the binary was built without `make frontend`.

## [0.1.45] — 2026-05-19

### Added

- **Stress tool jobs API**: `GET /api/v2/tools`, `POST /api/v2/tools/{id}/run`, and `POST /api/v2/jobs` with `profile_kind: "tool"` for `pcap2scenario`, `report-html`, `summary-to-pdf`, `rtp_send`, `infindex`.
- **Reports in admin UI**: new **Reports** nav page (`GET /api/v2/reports`) listing summary/HTML/PDF artifacts; job detail download/open via `GET /api/v2/jobs/{id}/artifacts/{kind}`; **Generate HTML/PDF** actions on completed load jobs.
- **Stress tools** page: catalog + **Run as job** forms wired to the tools API.

### Changed

- Worker tool execution moved to `internal/toolrun` (avoids import cycle with API package).

## [0.1.44] — 2026-05-19

### Added

- **Internal auth by default** when `ui_data_dir` is set (`gossipper server`) or when running `gossipper ui` (disable with `auth.type: none` or `--no-auth`).
- **Settings DB bootstrap on first start**: auto-creates `<ui_data_dir>/settings.sqlite` with JWT secret, default admin user (`admin` / `sipcapture`), and baseline `kv_settings`.

### Changed

- `gossipper auth user-add` works with any config that has `ui_data_dir` (no explicit `auth` block required).
- About page footer shows **AGPL-3.0** (was incorrectly Apache-2.0).

## [0.1.43] — 2026-05-18

### Fixed

- **Start job with built-in scenarios**: `POST /api/v2/jobs` now accepts engine built-ins (`uas`, `uac`, `management`, `invite_media*`) instead of returning `scenario: not found`.
- **Jobs UI — custom job ID**: optional **Job ID** field in the Start job modal with validation; value is sent as `id` in the API request.

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
