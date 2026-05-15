# Run profiles and aliases

Gossipper can load a **JSON run profile**: a file with a top-level `aliases` map. Each **alias** is a named preset of structured fields (and optional `extra_args`) that seed the internal [`Config`](../internal/cli/config.go) before the normal `flag` parser runs. Use this to version-control common invocations (HEP UAS, lab UAC, etc.) instead of long shell one-liners.

## Command-line interface

| Flag | Meaning |
|------|---------|
| `-config <path>` | Path to the JSON profile file. |
| `-run-alias <name>` | Apply the object `aliases.<name>` from that file. Required together with `-config` (unless listing). |
| `-list-aliases` | Print all alias names (sorted, one per line) and exit with status 0. Requires `-config`. |
| `-config-server <path>` | **Server-only:** one JSON object (same keys as a single alias, **no** `aliases` wrapper). After load, gossipper behaves as if **`-server`** were set. Mutually exclusive with `-config`, `-run-alias`, `-list-aliases`, and **`-config-client`**. |
| `-config-client <path>` | **Client preset:** one JSON object (same keys as a single alias, **no** `aliases` wrapper). After load, **`-server` is off** (even if the JSON contained `"server": true`). Mutually exclusive with `-config`, `-run-alias`, `-list-aliases`, and **`-config-server`**. |

Supported forms: `-config /path/file.json`, `-config=/path/file.json`, and the same for `-run-alias`, `-config-server`, `-config-client`. Long forms `--config`, `--run-alias`, `--list-aliases`, `--config-server`, `--config-client` are accepted.

**Rules**

- `-config` without `-run-alias` is an error **unless** you pass `-list-aliases`.
- `-run-alias` without `-config` is an error.
- **`-config-server`** and **`-config-client`** cannot be combined with **`-config`**, **`-run-alias`**, or **`-list-aliases`**.
- **`-config-server`** and **`-config-client`** cannot be combined with each other.
- Run-profile flags are **not** passed to subcommands such as `gossipper shell`, `gossipper tui`, or `gossipper pcap2scenario`; they apply only to the main SIP/launcher path after `cli.Parse`.

## Flat server config (`-config-server`)

For **systemd** or any long-lived **Control UI** deployment, you can keep a **single flat JSON** next to the binary instead of an `aliases` map:

- Same **snake_case** keys as in [Supported alias fields](#supported-alias-fields) (one object = one former alias body).
- Do **not** use a top-level **`aliases`** key; if present, gossipper rejects the file and tells you to use `-config` / `-run-alias` instead.
- **`"server": true`** in the file is optional: **`-config-server` always enables server mode** after merge (equivalent to passing **`-server`**).
- **SIP listen address** for the UAS is **`local_ip`** and **`local_port`** in JSON (CLI: **`-i`** / **`-p`**), not **`remote_addr`** / **`-rsa`** (those are for UAC-style remote targets).
- With **`-server`** or **`-config-server`**, if **`local_port`** is absent and **`-p`** is not passed on the command line, gossipper sets **`5060`** (pass **`-p 0`** explicitly for an ephemeral SIP port).
- Remaining argv (e.g. **`-api_token`**, **`-p`** to override JSON) still wins per the merge order below.

Example: [`examples/gossipper-server.json`](../examples/gossipper-server.json). Several binds (UDP `u1` / `un`, TCP `t1`, TLS `l1` / `ln`, or mixed): [`examples/gossipper-server-multi-listener.json`](../examples/gossipper-server-multi-listener.json).

```bash
gossipper -config-server /etc/gossipper/server.json
```

## Flat client config (`-config-client`)

For a **second gossipper** (or any process) running as **UAC / load generator**, use a flat JSON with the same keys as one run-profile alias (no `aliases` wrapper). After load, gossipper **never** stays in management **`-server`** mode (`ServerMode` is cleared even if `"server": true` appeared in the file by mistake).

Example: [`examples/gossipper-client.json`](../examples/gossipper-client.json).

```bash
gossipper -config-client /etc/gossipper/client.json
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
2. **`-config-server` file**, **`-config-client` file**, *or* **selected `aliases.<name>` from `-config`** — typed fields from JSON are written into `Config`. Relative **`scenario_file`** and **`injection_file`** paths are resolved against the **directory containing the JSON file**.
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
| `service` | string | `-s` |
| `transport` | string | `-t` (`u1`, `un`, …) |
| `local_ip` | string | `-i` |
| `local_port` | int | `-p` |
| `listeners` | object[] | **`-server` / UAS only:** several SIP binds in parallel. Each element: `transport` (`u1`, `un`, `t1`, `tn`, `l1`, `ln`; optional — inherits top-level `transport`), `local_ip`, `local_port` (optional fields inherit top-level `local_ip` / `local_port`). **TLS** listeners require **`tls_cert`** / **`tls_key`** (CLI or `extra_args`). Global `total_calls` counts accepted calls across all listeners. |
| `remote_addr` | string | `-rsa` (`host:port`) — UAC remote peer; **not** the UAS SIP listen address (use `local_ip` / `local_port` for `-server` / UAS bind) |
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
| `api_addr` | string | `-api_addr` (Control UI / `/api/v1`; with `-server`, default `:8080` if omitted after profile merge) |
| `api_token` | string | `-api_token` |
| `server` | bool | `-server` (long-run management: OPTIONS UAS + HTTP API; same built-in defaults as CLI — empty `api_addr` becomes `:8080`, scenario becomes `management` unless `scenario_file` / `scenario_name` override) |
| `extra_args` | string[] | Additional argv fragments (see merge order). |

These `log_otel_*` keys configure **structured event export** over OTLP (same flags as shell `LOG_OTEL_*` in `scripts/hep-uas-listen.sh`); gossipper does not expose separate Prometheus scrape settings in the run profile.

Flags **not** in this list must be passed via **`extra_args`** or on the command line. To add first-class JSON keys, extend `runSpec` and `applyRunSpec` in [`internal/cli/run_profile.go`](../internal/cli/run_profile.go).

## Bundled examples

The repository includes:

- [`testdata/run-profiles/example.json`](../testdata/run-profiles/example.json) — **`hep-uas-listen`** (UAS + HEP + optional OTLP defaults matching the shell script comments) and **`uac-local`** (one UAC call). For systemd-style **`"server": true`** in a profile, use a dedicated alias in your own JSON or **`-config-server`** with [`examples/gossipper-server.json`](../examples/gossipper-server.json).
- [`examples/gossipper-client.json`](../examples/gossipper-client.json) — minimal flat preset for **`-config-client`** (lab UAC toward one peer). **`.deb` / `.rpm`** also ship **`examples/gossipper-server.service`** and **`examples/gossipper-client.service`** as **`/lib/systemd/system/gossipper-server.service`** and **`gossipper-client.service`** (with matching JSON under **`/usr/local/gossipper/etc/`**).
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

- Profile handling lives in [`internal/cli/run_profile.go`](../internal/cli/run_profile.go); [`Parse`](../internal/cli/config.go) strips `-config` / `-run-alias` / `-list-aliases` / `-config-server` / `-config-client`, applies the chosen preset, then builds the `flag.FlagSet` using **current `cfg` values** as defaults for key string flags (`-sf`, `-hep_addr`, …) so profile values are not wiped when the flag set is constructed.
- `-list-aliases` causes `Parse` to return [`ErrListAliases`](../internal/cli/run_profile.go); the binary exits 0 after printing names.

## See also

- [`README.md`](../README.md) — documentation index.
- [`docs/qos-reporting.md`](qos-reporting.md) — HEP media flags when combining profiles with `-send_media_report`.
