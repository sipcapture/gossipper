# Gossipper Control UI

Local web panel for the gossipper HTTP API (`internal/api`): health, stats, XML scenario, hot apply, rate/pause.

## License and styling

**Gossipper** and this UI are distributed under **AGPL-3.0-or-later** (see the root `LICENSE`).

The theme (Tailwind 4 + shadcn tokens, `App.css`) is **derived from** the [Homer](https://github.com/sipcapture/homer) web UI (`src/ui`), also AGPL — SPDX and a source link are at the top of `src/App.css`.

## Development

Requires Node **20.19+** or **22.12+** (see `package.json` → `engines`).

```bash
cd web/control-ui
npm install
```

By default Vite listens on port **5174** and proxies the `/api` prefix to the backend (**`VITE_API_TARGET`**, default `http://127.0.0.1:8080`). UI requests use **`VITE_API_BASE`** (default **`/api/v1`**, so via the proxy you get paths like `/api/v1/health`).

```bash
# example: API on another host/port
VITE_API_TARGET=http://10.0.0.5:9090 npm run dev
```

Run gossipper with the API enabled, for example:

```text
-api_addr :8080
# optional:
-api_token <secret>
```

Use a scenario file when you need `PUT` to disk and apply “from disk” with an empty body:

```text
-sf /path/to/scenario.xml
```

## Production build / embed in gossipper

```bash
npm run build
```

The Vite production build writes into **`internal/api/webdist/`** at the repo root (relative from `web/control-ui`: `../../internal/api/webdist/`). **`go:embed`** in `internal/api/embed_ui.go` picks that up and the same HTTP server that serves `/api/v1/*` also serves **`GET /`** and **`/assets/*`**.

The repo root **`make frontend`** target runs this (and **`make build`** / `scripts/build_package.sh` call it automatically).

A separate **`go.mod`** (empty module) lives in this directory so **`go list ./...`** / **`go test ./...`** from the gossipper repo root do not walk `node_modules` after `npm ci`.

**`.env.production`** sets `VITE_API_BASE=/api/v1` (same origin as the embedded UI).

## Build to `dist/` only (no embed)

For a classic **`dist/`** tree behind nginx without embedding in the binary, temporarily set `build.outDir` to `dist` in `vite.config.ts` and build as usual.

## Endpoints (reference)

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/v1/health` | `{ "status": "ok" }` |
| GET | `/api/v1/stats` | Engine stats JSON |
| GET | `/api/v1/scenario` | Metadata + XML (or built-in scenario) |
| PUT | `/api/v1/scenario` | Write XML to `-sf`; `?apply=true` hot-reloads |
| POST | `/api/v1/scenario/apply` | `application/xml` body or empty body (reads `-sf`) |
| GET / POST | `/api/v1/control` | Read / change `rate`, `paused` |

Errors: JSON `{"error":"..."}` and an HTTP status code.
