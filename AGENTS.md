# gossIpper

SIPp-compatible SIP/RTP load & functional test tool in Go (sipgo + pion/rtp), with a web control UI.
Module: github.com/sipcapture/gossipper. Binary: cmd/gossip (`gossipper sipp ...`, `gossipper ui`, ...).

## Layout
Public packages (importable by other modules — keep API backward compatible):
- mediasink/          public contract for RTP/RTCP HEP export
- hepcodec/           HEP3 wire encoding/decoding

Core:
- internal/scenario   XML scenario model/parser; built-in lab scenarios embedded from internal/scenario/lab/*.xml
- internal/engine     scenario execution
- internal/sip, internal/siplog, internal/transport   SIP handling, SIP logging, UDP/TCP/TLS
- internal/media, internal/webrtc                     RTP/RTCP, pcap playback, rtpcheck; WebRTC bridge
- internal/hep        HEP export
- internal/stats, internal/reporthtml, internal/pdf   counters/summary, HTML/PDF reports
- internal/eventlog   non-blocking structured event logger
- internal/sipp       `gossipper sipp` — SIPp CLI parity
- internal/pcap2scenario  PCAP → UAC+UAS scenario pair with RTP mini-PCAPs
- internal/safepath   path-traversal hardening

UI control plane (`gossipper ui`):
- internal/api        HTTP API v1 + embedded UI (internal/api/webdist, built by `make frontend`)
- internal/api/v2     HTTP API v2 (new UI-facing API)
- internal/supervisor, internal/launcher, internal/loadtest, internal/scheduler   engine lifecycle, run preparation, load-test jobs
- internal/uistore    on-disk layout for UI state
- web/control-ui/     React + TypeScript frontend (see web/control-ui/AGENTS.md)

Test data: testdata/{scenarios,media,run-profiles,injection}. Example configs: examples/.

## Invariants
- XML scenarios stay SIPp-compatible; new actions are additive, never change semantics of existing ones.
- No mutable package-level state written at runtime (feature flags, modes, caches). Pass it via config/session structs.
  Tests use t.Parallel() and run with -race; a global write breaks every parallel test in the package.
- No allocations in the per-packet RTP path; benchmark before/after.
- Every new scenario action gets: parser test, e2e test against a local UAS, docs entry.
- Tests are deterministic: no wall-clock sleeps; use fake clock / pcap fixtures.
- HEP and eventlog output never block call processing (non-blocking send, drop + count on overflow).
- Any file path coming from user input, API or scenario goes through internal/safepath.
- Do not edit or commit internal/api/webdist/* (build output) except .gitkeep.

## Backend ↔ UI contract
- API v1: internal/api ↔ web/control-ui/src/api/v1.ts. API v2: internal/api/v2 ↔ web/control-ui/src/api/v2.ts.
- Changing a handler, route or JSON field (Go `json:"..."` tag) requires updating the matching TS types and callers in the same change.
- New UI features go to API v2 unless told otherwise.

## Go conventions
- Wrap errors with %w; no panics outside main/init.
- context.Context first for anything blocking or doing I/O.
- Every goroutine has an owner and a shutdown path.
- Table-driven tests; benchmarks for hot paths.

## Workflow
- Non-trivial change: plan first, write a failing test, then implement.
- After Go changes in sip/transport/media/webrtc/engine: run the sip-reviewer subagent; other Go changes: go-reviewer.
- After UI changes: run the typescript-reviewer subagent.

## Commands
- build:    `go build ./...`  (full binary with UI: `make`)
- test:     `go test -race ./...`
- lint:     `go vet ./...`
- bench:    `go test -run '^$' -bench . -benchmem ./internal/media`
- ui:       `cd web/control-ui && npm run typecheck && npm run lint && npm test`  (npm only; package-lock.json is the lockfile)
- ui build: `make frontend`
