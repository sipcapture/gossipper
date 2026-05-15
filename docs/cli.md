# Command-line interface (CLI)

Gossipper is a single binary (`cmd/gossip`) with:

1. **Reserved first-token subcommands** — fixed verbs (`tui`, `shell`, …) handled before the main `flag` parser.
2. **Default path** — everything else is parsed as **SIP/XML scenario flags** (SIPp-style surface; see [`compatibility.md`](compatibility.md) and [`cli-gap-list.md`](cli-gap-list.md)).
3. **Optional `gossipper sipp` prefix** — same scenario flags as (2); **not** for subcommands (see below).

Run **`gossipper -h`** for the full flag list embedded in the binary.

## Subcommands (first argument)

These tokens must appear **first** (after any optional `sipp` strip — you should **not** combine them with `sipp`; use the root command).

| Command | Purpose |
| --- | --- |
| `gossipper shell` | Line-oriented interactive shell (`set`, `run`, `wizard`, …). See [`interactive-shell.md`](interactive-shell.md). |
| `gossipper cli` | Alias for `shell`. |
| `gossipper tui` | Full-screen launcher and runtime UI. See [`tui.md`](tui.md). |
| `gossipper -interactive` / `--interactive` | Same control UI as `tui` (handled as a flag on the root path). |
| `gossipper server …` | Long-run **management** mode: the dispatcher **prepends `-server`** to the remainder of argv **unless** it already contains **`-server`** or **`-config-server`** (which imply server mode on their own). Prefer **`gossipper server -config-server /path.json`** in systemd. Same SIP/API flags as **`gossipper -server`**. |
| `gossipper pcap2scenario …` | PCAP → generated XML scenarios. See [`pcap2scenario.md`](pcap2scenario.md). |
| `gossipper report-html …` | Summary JSON → standalone HTML (separate small flag set). |
| `gossipper summary-to-pdf …` | HTML → PDF (optional embedded renderer when built with `-tags pdf`, else Chromium in `PATH`). |

**Version:** `-version` / `--version` anywhere in argv prints build info and exits (handled before subcommand routing).

## Scenario run (no subcommand)

```bash
gossipper -sn uac -rsa 127.0.0.1:5060 -m 10 -r 5
gossipper -sf ./scenario.xml -rsa 127.0.0.1:5060 …
```

Same parsing as **`gossipper sipp …`** for the flag tail. Run profiles (**`-config`**, **`-run-alias`**, **`-config-server`**, **`-config-client`**) apply on this path only — not after `shell` / `tui` / `pcap2scenario`. See [`run-profile.md`](run-profile.md).

## `gossipper sipp` (SIPp-style entry only)

**`gossipper sipp`** exists for **explicit SIPp-oriented documentation** and scripts that always prefix the binary name with `sipp`.

Rules (enforced in `internal/sipp`):

- **Allowed:** any argv that is valid **scenario / launcher flags** on the root command (e.g. **`-sn`**, **`-sf`**, **`-rsa`**, **`-config-server`**, **`-rtp_send`**, …).
- **Rejected:** placing **Gossipper-only** subcommands or **`-interactive`** after `sipp` (e.g. `gossipper sipp tui`). Use **`gossipper tui`** without `sipp`. Error: `ErrRootSubcommandAfterSipp`.

Leading **`sipp`** tokens may be repeated and are stripped (e.g. shell aliases); then empty argv prints **`gossipper sipp -h`** style usage.

Full narrative vs SIPp: [`gossipper-vs-sipp.md`](gossipper-vs-sipp.md).

## Management server quick reference

| Invocation | Notes |
| --- | --- |
| `gossipper server` | Prepends **`-server`**; then same defaults as **`gossipper -server`** (e.g. SIP port **5060** when **`-p`** omitted, API **`:8080`** when configured). |
| `gossipper server -config-server /path.json` | No extra **`-server`** injected (already implied by **`-config-server`**). |
| `gossipper -server …` | Equivalent server mode without the `server` token. |

Example unit file: [`examples/gossipper-server.service`](../examples/gossipper-server.service).

## Management HTTP API (`-api_addr`): live scenario

When **`GET /api/v1/stats`** works, **`PUT /api/v1/scenario?apply=true`** and **`POST /api/v1/scenario/apply`** call **`Engine.TryReplaceLiveScenario`**:

- **In-flight calls** keep the scenario XML they started with (snapshot at call start).
- **New calls** (including the next **incoming** dialog on server transports) use the updated scenario. The **first `<recv>`** used to match new server-side dialogs is taken from the **live** scenario on each packet, so it tracks hot reloads (not the startup file alone).
- **`POST /apply`** is allowed even while **`active_calls` > 0**; there is no longer a global “idle only” gate.
- The new scenario’s **mode** (`client` vs `server`) must still match how the process was started; changing mode requires a process restart.
- Built-in **`-sn`** scenarios without **`-sf`** cannot be edited via **`PUT /scenario`** (no on-disk path); use **`-sf`** or **`POST /apply`** with an **XML body**.

## SIP transports (server listeners)

Only in **`-server`** mode. Sockets stay bound; toggling affects **acceptance of new dialogs** only (existing calls are unchanged).

| Method | Path | Body | Response |
| --- | --- | --- | --- |
| **GET** | `/api/v1/transports` | — | `{"listeners":[{"index", "transport", "local_ip", "local_port", "enabled"}, ...]}` |
| **POST** | `/api/v1/transports` | `{"listeners":[{"index":0,"enabled":false},...]}` **or** shorthand `{"index":0,"enabled":false}` | Same shape as **GET** (current state after apply) |

In **client** mode, **GET** returns `listeners: []`; **POST** returns **400** (no listener slots).

## See also

- [`run-profile.md`](run-profile.md) — JSON aliases and flat server/client configs  
- [`gossipper-vs-sipp.md`](gossipper-vs-sipp.md) — product comparison and CLI philosophy  
- [`interactive-shell.md`](interactive-shell.md), [`tui.md`](tui.md) — interactive entry points  
