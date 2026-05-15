# Run profiles and aliases

Gossipper can load a **JSON run profile**: a file with a top-level `aliases` map. Each **alias** is a named preset of structured fields (and optional `extra_args`) that seed the internal [`Config`](../internal/cli/config.go) before the normal `flag` parser runs. Use this to version-control common invocations (HEP UAS, lab UAC, etc.) instead of long shell one-liners.

**Short flag summary in the binary:** `gossipper profile` or `gossipper profile -h`.

For **subcommands** (`tui`, `shell`, `server`, …), the optional **`gossipper sipp`** prefix, and **`-version`**, see [`cli.md`](cli.md).

## Command-line interface

| Flag | Meaning |
|------|---------|
| `-config <path>` | Path to the JSON profile file. |
| `-run-alias <name>` | Apply the object `aliases.<name>` from that file. Required together with `-config` (unless listing). |
| `-list-aliases` | Print all alias names (sorted, one per line) and exit with status 0. Requires `-config`. |

Run profiles (`-config` / `-run-alias` / `-list-aliases`) are **root-command only**.

**Flat JSON (no `aliases` wrapper)** loads via **`gossipper server -config <path>`**. Gossipper infers **management** (SIP UAS + API) vs **load** (UAC preset) using a JSON `"role"` field (`management` / `server` vs `load` / `client` / `uac`) and simple heuristics (`listeners`, `api_addr`, `scenario_name` + `remote_addr`, …). Ambiguous files must set `"role"` explicitly. The legacy flags **`-config-server`** / **`-config-client`** were removed; they now error with a pointer to **`gossipper server -config <path>`**.

Supported forms: `-config /path/file.json`, `-config=/path/file.json`, and the same for `-run-alias`. Long forms `--config`, `--run-alias`, `--list-aliases` are accepted.

**Rules**

- On the **root** command, `-config` without `-run-alias` is an error **unless** you pass `-list-aliases`.
- `-run-alias` without `-config` is an error.
- **`gossipper server`** accepts **flat JSON** via **`-config <path>`** (no `-run-alias`). Run-profile aliases stay on the root command: **`gossipper -config <path> -run-alias …`** (or `-list-aliases`).
- **`gossipper server`** must not be combined with **`-run-alias`** / **`-list-aliases`** (those are profile-only).
- Run-profile flags are **not** passed to subcommands such as `gossipper shell`, `gossipper tui`, `gossipper profile`, or `gossipper pcap2scenario`; they apply only to the main SIP/launcher path after `cli.Parse` (including the `gossipper server …` argv tail).

## Flat JSON (`gossipper server -config`)

For **systemd** or scripted deployments, you can keep a **single flat JSON** next to the binary instead of an `aliases` map:

### Composite `server` + `clients` (one process, multiple SIP engines)

A flat file may contain a top-level **`"server": { ... }`** object (management / listener profile) and optionally **`"clients": [ {...}, ... ]`** or a single **`"client": { ... }`** / **`"client": [ ... ]`** for UAC load profiles. Use **`clients`** OR **`client`**, not both.

Root keys (**`api_addr`**, **`hep_addr`**, …) are **shallow-merged** into the server object and into each client object. Reserved keys **`server`**, **`clients`**, **`client`**, **`workloads`**, **`aliases`** never appear inside merged profiles. Optional **`"id"`** on the server or a client names that engine in logs and in **`GET /api/v1/stats`**.

Rules:

- The **server** profile must infer as **management** (listener + optional Control UI). It becomes the **primary** engine; **`api_addr` / `api_token`** apply only here — client profiles do not start their own HTTP listener.
- **Client** profiles (typically **`role": "load"`**) run **in parallel goroutines** with their own [`engine.Engine`](https://pkg.go.dev/github.com/sipcapture/gossipper/internal/engine).
- **`GET /api/v1/stats`** returns **`{"multi":true,"engines":[{"id":"…","stats":{…}},…]}`** when clients exist (single-engine mode keeps the flat stats object).
- **`POST /api/v1/control`** (rate / pause) applies to **all** engines. Scenario **PUT/apply** and SIP **transport** toggles still target the **primary** management engine only (Control UI should branch on **`multi`**).
- **Several SIP transports on listen:** put a **`listeners`** array on the **server** object (see the **`server`** section in [`examples/gossipper-hybrid.json`](../examples/gossipper-hybrid.json)). The first listener sets the primary **`transport` / `local_ip` / `local_port`** for API/scenario defaults; every bind must be **unique** (**transport + IP + port**). Gossipper rejects duplicate binds **across profiles** in one file.
- **Several transports on load:** add **more than one** object under **`clients`** (or several in **`client`** as an array). Each client engine has one outbound stack: **`transport`**, **`remote_addr`**, and a **distinct `local_port`** on **`local_ip`**. Match **`remote_addr`** to the listener under test.

Top-level **`"workloads"`** is **rejected** (removed; migrate to **`server`** / **`clients`**).

See [`examples/gossipper-hybrid.json`](../examples/gossipper-hybrid.json) and **`gossipper-hybrid.service`** (also shipped in `.deb`/`.rpm`).

- Same **snake_case** keys as in [Supported alias fields](#supported-alias-fields) (one object = one former alias body).
- Do **not** use a top-level **`aliases`** key; if present, gossipper rejects the file and tells you to use `-config` / `-run-alias` instead on the **root** command.
- **SIP listen address** for the UAS is **`local_ip`** and **`local_port`** in JSON (CLI: **`-i`** / **`-p`**), not **`remote_addr`** / **`-rsa`** (those are for UAC-style remote targets).
- **Management** flat presets enable **management server mode** after merge; **load** flat presets clear management server mode (even if `"server": true` appeared in the file by mistake).
- With **management server mode** (from a **management** flat JSON or plain **`gossipper server`** without a flat file), if **`local_port`** is absent and **`-p`** is not passed on the command line, gossipper sets **`5060`** (pass **`-p 0`** explicitly for an ephemeral SIP port).
- Remaining argv (e.g. **`-api_token`**, **`-p`** to override JSON) still wins per the merge order below.

### Inferring management vs load

If `"role"` is omitted, gossipper picks **management** when any of these hold (first match wins):

- non-empty **`listeners`** array
- top-level **`server`** object (composite hybrid layout; see above)
- non-empty **`api_addr`**
- **`scenario_name`** is `management` or `uas` (case-insensitive)

Otherwise it picks **load** when:

- **`scenario_name`** is `uac` and **`remote_addr`** is set, or
- **`scenario_name`** is empty/absent and **`remote_addr`** is set

If still ambiguous, **`Parse` / `InferServerFlatManagement`** return an error asking you to set **`"role"`** to **`management`** / **`server`** or **`load`** / **`client`** / **`uac`**.

Examples:

- [`examples/gossipper-management.json`](../examples/gossipper-management.json) — management only: top-level **`server`** + shared **`api_addr`** (installed as **`gossipper-server.json`** in `.deb`/`.rpm`).
- [`examples/gossipper-uac.json`](../examples/gossipper-uac.json) — flat **load** preset (`uac` + `remote_addr`; installed as **`gossipper-client.json`** in packages).
- [`examples/gossipper-hybrid.json`](../examples/gossipper-hybrid.json) — composite **`server`** + **`clients`**: management with **`listeners`** (UDP / TCP / multi-socket UDP) plus several UAC client profiles.

```bash
gossipper server -config /path/to/gossipper-management.json
gossipper server -config /path/to/gossipper-uac.json
gossipper server -config /path/to/gossipper-hybrid.json
```

## JSON file shape

```json
{
  "aliases": {
    "my-alias": {
      "scenario_name": "uac",
      "remote_addr": "127.0.0.1:5060",
      "total_calls": 1,
      "extra_args": ["-trace_msg"]
    }
  }
}
```

- **`aliases`** (required): object whose keys are alias names (use stable identifiers: `hep-uas-lab`, `staging-uac`, …).
- Each alias value is an object: any subset of the **supported keys** below, plus optional **`extra_args`**: an array of strings in the same form as shell argv (e.g. `"-m", "0"`). Keys omitted keep [`DefaultConfig()`](../internal/cli/config.go) defaults for those fields.

Invalid JSON or an unknown alias name produces a clear parse error.

## Merge order and overrides

1. **`DefaultConfig()`** — baseline.
2. **Flat JSON file** (`gossipper server -config …`) *or* **selected `aliases.<name>` from `-config`** — typed fields from JSON are written into `Config`. Relative **`scenario_file`** and **`injection_file`** paths are resolved against the **directory containing the JSON file**.
3. **`extra_args`** — inserted **before** the remainder of argv so that **later** tokens (your real CLI) win when the same flag is repeated (standard `flag` parsing).
4. **Remaining command-line arguments** — override profile and `extra_args` for flags you repeat on the command line.

Example: profile sets `hep_addr`, you run:

```bash
gossipper -config prod.json -run-alias=uas -hep_addr 10.0.0.5:9060
```

The collector address becomes `10.0.0.5:9060`.

## `total_calls` and unlimited runs

Gossipper requires **explicit** `-m 0` for “unlimited” stress mode. If you set `"total_calls": 0` in JSON, that counts as explicit for the profile path (the same rule as passing `-m 0` on the CLI).

## Supported alias fields

These JSON keys map to `Config` / CLI (snake_case in JSON only):

| JSON key | Type | Description |
|----------|------|-------------|
| `scenario_file` | string | XML scenario path (`-sf`). Relative paths are relative to the config file directory. |
| `scenario_name` | string | Built-in scenario name (`-sn`, e.g. `uac`, `uas`). Ignored for file loading when `scenario_file` is set (launcher prefers the file). |
| `role` | string | **Flat JSON only:** `management` / `server` vs `load` / `client` / `uac` to force preset selection when heuristics are ambiguous (ignored for run-profile aliases). |
| `service` | string | `-s` |
| `transport` | string | `-t` (`u1`, `un`, …) |
| `local_ip` | string | `-i` |
| `local_port` | int | `-p` |
| `listeners` | object[] | **Management / UAS only:** several SIP binds in parallel. Each element: `transport` (`u1`, `un`, `t1`, `tn`, `l1`, `ln`; optional — inherits top-level `transport`), `local_ip`, `local_port` (optional fields inherit top-level `local_ip` / `local_port`). **TLS** listeners require **`tls_cert`** / **`tls_key`** (CLI or `extra_args`). Global `total_calls` counts accepted calls across all listeners. |
| `remote_addr` | string | `-rsa` (`host:port`) — UAC remote peer; **not** the UAS SIP listen address (use `local_ip` / `local_port` for UAS / management bind) |
| `auth_username` | string | `-au` |
| `auth_password` | string | `-ap` |
| `rate` | number | `-r` |
| `max_concurrent` | int | `-l` |
| `total_calls` | int | `-m` (use `0` for unlimited when intended) |
| `users` | int | `-users` |
| `hep_addr` | string | `-hep_addr` |
| `hep_capture_id` | number | `-hep_capture_id` |
| `hep_password` | string | `-hep_password` |
| `hep_raw_rtcp` | bool | `-hep_raw_rtcp` |
| `hep_homer_lake_rtcp` | bool | `-hep_homer_lake_rtcp` |
| `send_media_report` | bool | `-send_media_report` |
| `summary_json` | string | `-summary_json` |
| `trace_msg` | bool | `-trace_msg` |
| `stat_period` | string | `-stat_period` (Go duration, e.g. `5s`, `1m30s`) |
| `injection_file` | string | `-inf` (relative to config dir) |
| `ip_field` | int | `-ip_field` / `-ipfield` |
| `log_otel_endpoint` | string | `-log_otel_endpoint` (OTLP: HTTP full URL or gRPC `host:port`) |
| `log_otel_proto` | string | `-log_otel_proto` (`grpc` or `http`) |
| `log_otel_insecure` | bool | `-log_otel_insecure` |
| `api_addr` | string | `-api_addr` (Control UI / `/api/v1`; in management server mode, default `:8080` if omitted after profile merge) |
| `api_token` | string | `-api_token` |
| `server` | bool | Request management server mode from JSON when using run-profile aliases (flat JSON management vs load is chosen separately via `InferServerFlatManagement`) |
| `extra_args` | string[] | Additional argv fragments (see merge order). |

These `log_otel_*` keys configure **structured event export** over OTLP (same flags as shell `LOG_OTEL_*` in `scripts/hep-uas-listen.sh`); gossipper does not expose separate Prometheus scrape settings in the run profile.

Flags **not** in this list must be passed via **`extra_args`** or on the command line. To add first-class JSON keys, extend `runSpec` and `applyRunSpec` in [`internal/cli/run_profile.go`](../internal/cli/run_profile.go).

## Bundled examples

The repository includes:

- [`testdata/run-profiles/example.json`](../testdata/run-profiles/example.json) — **`hep-uas-listen`** (UAS + HEP + optional OTLP defaults matching the shell script comments) and **`uac-local`** (one UAC call). For systemd-style **`"server": true`** in a profile, use a dedicated alias in your own JSON or a composite sample [`examples/gossipper-management.json`](../examples/gossipper-management.json) with **`gossipper server -config …`**.
- **`examples/`** — [`gossipper-management.json`](../examples/gossipper-management.json) (management composite), [`gossipper-uac.json`](../examples/gossipper-uac.json) (flat UAC load), [`gossipper-hybrid.json`](../examples/gossipper-hybrid.json) (**`server`** + **`clients`**). **`.deb` / `.rpm`** install the first two under **`/usr/local/gossipper/etc/gossipper-server.json`** and **`gossipper-client.json`** and ship **`gossipper-hybrid.json`** plus matching **`systemd`** units from **`examples/*.service`**.
- [`testdata/run-profiles/hep-scripts.json`](../testdata/run-profiles/hep-scripts.json) — parity with [`scripts/hep-uas-listen.sh`](../scripts/hep-uas-listen.sh) and [`scripts/hep-uac-send.sh`](../scripts/hep-uac-send.sh) (Homer-Lake, raw RTCP, and short-JSON UAC variants); see [`testdata/run-profiles/README.md`](../testdata/run-profiles/README.md).

From the repo root:

```bash
gossipper -config testdata/run-profiles/example.json -list-aliases
gossipper -config testdata/run-profiles/example.json -run-alias=hep-uas-listen -hep_addr "${HEP_ADDR:-127.0.0.1:9060}"
go run ./cmd/gossip -config testdata/run-profiles/example.json -run-alias=uac-local
```

HEP script presets (always pass **`-hep_addr`**):

```bash
gossipper -config testdata/run-profiles/hep-scripts.json -list-aliases
gossipper -config testdata/run-profiles/hep-scripts.json -run-alias=hep-uas-listen -hep_addr "$HEP_ADDR"
gossipper -config testdata/run-profiles/hep-scripts.json -run-alias=hep-uac-send -hep_addr "$HEP_ADDR"
```

## Minimal inline example

If the JSON file lives in the repo root, paths in the profile are often relative to that file:

```json
{
  "aliases": {
    "uas-listen": {
      "scenario_file": "testdata/scenarios/uas_pcap.xml",
      "transport": "u1",
      "local_ip": "0.0.0.0",
      "local_port": 9050,
      "total_calls": 0,
      "rate": 1,
      "max_concurrent": 2048,
      "hep_addr": "127.0.0.1:9060",
      "hep_capture_id": 2001,
      "extra_args": ["-hep_password", "secret"]
    }
  }
}
```

```bash
export HEP_ADDR=collector.example.com:9060
gossipper -config gossipper.json -run-alias=uas-listen -hep_addr "$HEP_ADDR"
```

## Implementation notes

- Profile handling lives in [`internal/cli/run_profile.go`](../internal/cli/run_profile.go); [`Parse`](../internal/cli/config.go) strips `-config` / `-run-alias` / `-list-aliases`, applies the chosen preset (run-profile alias selection on the root command, or flat JSON via `gossipper server -config`), then builds the `flag.FlagSet` using **current `cfg` values** as defaults for key string flags (`-sf`, `-hep_addr`, …) so profile values are not wiped when the flag set is constructed.
- `-list-aliases` causes `Parse` to return [`ErrListAliases`](../internal/cli/run_profile.go); the binary exits 0 after printing names.

## See also

- [`README.md`](../README.md) — documentation index.
- [`docs/qos-reporting.md`](qos-reporting.md) — HEP media flags when combining profiles with `-send_media_report`.
