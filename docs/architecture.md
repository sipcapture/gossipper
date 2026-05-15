# Runtime architecture

`Gossipper` intentionally separates concerns that are tightly coupled inside SIPp.

## Binary entry (`cmd/gossip`)

The main binary parses **`-version`** early, routes **reserved subcommands** (`tui`, `shell`, `server`, `auth`, `pcap2scenario`, …) and optional **`gossipper sipp`** (SIPp-style flag tail only), then runs **`internal/cli.Parse`** and **`internal/launcher`**. See **[`cli.md`](cli.md)**.

## Main components

- `internal/scenario`
  - parses XML into a neutral command list
  - resolves labels and scenario mode

- `internal/template`
  - renders SIPp-style keywords without network side effects
  - computes `Content-Length` in a dedicated second pass
  - now fails explicitly on unsupported scenario keywords instead of silently
    emitting empty strings

- `internal/sip`
  - parses SIP start line and headers
  - provides Call-ID extraction and recv matching helpers

- `internal/transport`
  - owns UDP, TCP, and TLS transport implementations
  - supports shared/per-call client modes plus server-side UDP aliases

- `internal/engine`
  - orchestrates calls, branching, retransmits, timeouts, auth, XML actions,
    and stats
  - keeps runtime state per call instead of using globals

- `internal/hep`
  - encodes HEP3 packets and manages UDP export to Homer-compatible collectors
  - mirrors SIP send/recv traffic when HEP is enabled

- `internal/scheduler`
  - abstracts time and rate pacing
  - keeps room for future deterministic test clocks

- `internal/stats`
  - aggregates success/failure, failure classes, latency distributions,
    retransmits, and timeouts

- `internal/media`
  - isolated RTP helpers based on Pion
  - SRTP (**SDES** + **DTLS-SRTP**), ICE, and TURN integration for scenario media when **`-media_srtp`** (see **`docs/srtp.md`**)
  - intentionally decoupled from SIP call execution

- `internal/api`
  - HTTP **`/api/v1/*`** management surface: stats, scenario get/put/apply, control, transports, dynamic clients, **`/live`** WebSocket, optional **`/auth/*`** when internal auth is enabled
  - optional embedded **Control UI** (`go:embed webdist`) at **`GET /`**

- `internal/settingsauth`
  - SQLite user store, bcrypt passwords, JWT issuance for **`auth.type`**: **`internal`**

- `internal/launcher`
  - builds engines from CLI and **flat JSON** (`gossipper server -config`): composite **`server`** + **`clients[]`**, hybrid multi-listener profiles, **`LoadCoordinator`** for **POST /api/v1/clients**

- `web/control-ui`
  - Vite/React panel for the management API (see `web/control-ui/README.md`); production assets go to `internal/api/webdist/`

## Execution flow

1. CLI loads XML or a built-in scenario and builds runtime config
2. Engine starts trace sinks, optional HEP export, and optional command peers
3. Engine selects client or server mode
4. Transport creates shared or per-call UDP/TCP/TLS sockets as needed
5. Each call renders a step through the template layer
6. XML actions execute against per-call/global/user variable scopes
7. Sent and received SIP messages are parsed through `internal/sip`
8. HEP export observes SIP send/recv traffic without duplicating scenario logic
9. Stats collector aggregates final results and optional CSV trace snapshots
