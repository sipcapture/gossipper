# web/control-ui — gossIpper control UI

React + TypeScript (strict), Vite, Vitest, ESLint. Package manager: npm only (package-lock.json).
Built by `make frontend` into internal/api/webdist and embedded into the Go binary.

## Structure
- src/api/v1.ts, src/api/v2.ts   typed clients for internal/api and internal/api/v2 — the ONLY place that calls fetch
- src/lib/                        pure logic (parsing, graphs, diffing, routing, live-data reducers) + *.test.ts next to each file
- src/lib/*Live.ts                live data (jobs, SIP) — reuse these, never open new sockets/polling in components
- src/components/ui/              shared UI primitives — reuse, don't duplicate
- src/components/v2/, src/views/  feature components and pages

## Rules
- Non-trivial logic goes into src/lib as pure functions with a test; components stay thin.
- Types in src/api/*.ts mirror Go JSON exactly (same field names as Go `json:"..."` tags, same optionality).
  If a Go handler changes, update the TS types and callers in the same change.
- New features use API v2 (src/api/v2.ts) unless told otherwise.
- No `any`; no inline eslint-disable without a comment explaining why.
- Large lists (calls, SIP messages, jobs): virtualize or paginate; never render unbounded arrays.
- Do not edit generated/build output (dist/, internal/api/webdist/).

## Commands (run from web/control-ui)
- `npm run typecheck && npm run lint && npm test` must pass
- dev server: `npm run dev`
