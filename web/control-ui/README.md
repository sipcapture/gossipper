# Gossipper Control UI

Admin console for **`gossipper ui`** and **`gossipper server`** when `ui_data_dir` is set. Talks to **`/api/v2/*`** on the same origin (embedded after `make frontend`).

## License and styling

**Gossipper** and this UI are distributed under **AGPL-3.0-or-later** (see the root `LICENSE`).

The theme (Tailwind 4 + shadcn tokens) is **derived from** the [Homer](https://github.com/sipcapture/homer) web UI — SPDX and a source link are at the top of `src/App.css`.

## Development

Requires Node **20.19+** or **22.12+** (see `package.json` → `engines`).

```bash
cd web/control-ui
npm install
npm run dev
```

Vite listens on port **5174** and proxies **`/api`** to the backend (**`VITE_API_TARGET`**, default `http://127.0.0.1:8080`). The app uses **`/api/v2`** paths (health, jobs, load-test, scenarios, …).

Run gossipper with the v2 API enabled:

```bash
gossipper ui --data-dir ./data --listen :8099
# or gossipper server -config management.json  # with ui_data_dir in JSON
```

Sign in: default bootstrap user **`admin` / `sipcapture`** (or env `GOSSIPPER_BOOTSTRAP_*`).

## Production build / embed

```bash
npm run build
# or from repo root:
make frontend
```

Output goes to **`internal/api/webdist/`** and is embedded via `go:embed` in `internal/api/embed_ui.go`. The binary serves **`GET /`**, **`/assets/*`**, and **`/api/v2/*`**.

## Main UI areas

| Nav | API |
| --- | --- |
| Dashboard | `/api/v2/live`, `/api/v2/jobs`, optional `/api/v1/*` hybrid panel |
| Load test | `GET/POST /api/v2/load-test/*` |
| Jobs | `/api/v2/jobs`, `/api/v2/jobs/{id}/events` |
| Reports | `/api/v2/reports`, artifacts |
| Scenarios | `/api/v2/scenarios`, `/api/v2/tools/*` (Prep) |
| Clients / Servers | `/api/v2/clients`, `/api/v2/servers` |

Deep links: `#/load/{jobId}`, `#/jobs/{jobId}`, `#/reports?report={jobId}`.

See also [`docs/ui-mode.md`](../../docs/ui-mode.md) and [`docs/sipstress-style-load-testing.md`](../../docs/sipstress-style-load-testing.md).

## Tests

```bash
npm test
npm run build   # tsc + vite
```

## Legacy `/api/v1` (hybrid server only)

When **`gossipper server`** runs with **`legacy_api_v1: true`** and live SIP engines, the Dashboard may show **rate/pause** and **dynamic clients** via `/api/v1/control` and `/api/v1/clients`. That is separate from the v2 supervisor job model used by **Load test** and **Jobs**.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/health` | Liveness |
| GET | `/api/v1/stats` | Engine snapshot(s) |
| GET/POST | `/api/v1/control` | Rate / pause |
| GET/POST/DELETE | `/api/v1/clients` | Dynamic UAC engines |

Errors: JSON `{"error":"..."}` and an HTTP status code.
