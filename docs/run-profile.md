# Run profiles and aliases

Gossipper can load a **JSON run profile**: a file with a top-level `aliases` map. Each **alias** is a named preset of structured fields (and optional `extra_args`) that seed the internal [`Config`](../internal/cli/config.go) before the normal `flag` parser runs. Use this to version-control common invocations (HEP UAS, lab UAC, etc.) instead of long shell one-liners.

## Command-line interface

| Flag | Meaning |
|------|---------|
| `-config <path>` | Path to the JSON profile file. |
| `-run-alias <name>` | Apply the object `aliases.<name>` from that file. Required together with `-config` (unless listing). |
| `-list-aliases` | Print all alias names (sorted, one per line) and exit with status 0. Requires `-config`. |

Supported forms: `-config /path/file.json`, `-config=/path/file.json`, and the same for `-run-alias`. Long forms `--config`, `--run-alias`, `--list-aliases` are accepted.

**Rules**

- `-config` without `-run-alias` is an error **unless** you pass `-list-aliases`.
- `-run-alias` without `-config` is an error.
- Run-profile flags are **not** passed to subcommands such as `gossipper shell`, `gossipper tui`, or `gossipper pcap2scenario`; they apply only to the main SIP/launcher path after `cli.Parse`.

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
2. **Selected alias** — typed fields from JSON are written into `Config`. Relative **`scenario_file`** and **`injection_file`** paths are resolved against the **directory containing the JSON file**.
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
| `remote_addr` | string | `-rsa` (`host:port`) |
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
| `summary_html` | string | `-summary_html` |
| `sip_from` | string | `-sip_from` |
| `sip_pai` | string | `-sip_pai` |
| `sip_provider` | string | `-sip_provider` |
| `sip_extra_headers` | string[] | repeatable `-sip_extra_header` (one header string per array element) |
| `health_min_success_ratio` | number | `-health_min_success_ratio` (evaluated with summary export) |
| `health_max_failed_calls` | int | `-health_max_failed_calls` |
| `health_max_timeouts` | int | `-health_max_timeouts` |
| `trace_msg` | bool | `-trace_msg` |
| `stat_period` | string | `-stat_period` (Go duration, e.g. `5s`, `1m30s`) |
| `injection_file` | string | `-inf` (relative to config dir) |
| `ip_field` | int | `-ip_field` / `-ipfield` |
| `log_otel_endpoint` | string | `-log_otel_endpoint` (OTLP: HTTP full URL or gRPC `host:port`) |
| `log_otel_proto` | string | `-log_otel_proto` (`grpc` or `http`) |
| `log_otel_insecure` | bool | `-log_otel_insecure` |
| `extra_args` | string[] | Additional argv fragments (see merge order). |

These `log_otel_*` keys configure **structured event export** over OTLP (same flags as shell `LOG_OTEL_*` in `scripts/hep-uas-listen.sh`); gossipper does not expose separate Prometheus scrape settings in the run profile.

Flags **not** in this list must be passed via **`extra_args`** or on the command line. To add first-class JSON keys, extend `runSpec` and `applyRunSpec` in [`internal/cli/run_profile.go`](../internal/cli/run_profile.go).

## Bundled examples

The repository includes:

- [`testdata/run-profiles/example.json`](../testdata/run-profiles/example.json) — **`hep-uas-listen`** (UAS + HEP + optional OTLP defaults matching the shell script comments) and **`uac-local`** (one UAC call).
- [`testdata/run-profiles/hep-scripts.json`](../testdata/run-profiles/hep-scripts.json) — parity with [`scripts/hep-uas-listen.sh`](../scripts/hep-uas-listen.sh) and [`scripts/hep-uac-send.sh`](../scripts/hep-uac-send.sh) (Homer-Lake, raw RTCP, and short-JSON UAC variants); see [`testdata/run-profiles/README.md`](../testdata/run-profiles/README.md).
- [`testdata/run-profiles/trunk-ci.json`](../testdata/run-profiles/trunk-ci.json) — example **`uac-trunk-report`**: SIP trunk-style identity (`sip_from`, `sip_pai`, `sip_provider`, `sip_extra_headers`), `summary_json` / `summary_html` under `out/`, and CI-style **health** thresholds.

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

- Profile handling lives in [`internal/cli/run_profile.go`](../internal/cli/run_profile.go); [`Parse`](../internal/cli/config.go) strips `-config` / `-run-alias` / `-list-aliases`, applies the alias, then builds the `flag.FlagSet` using **current `cfg` values** as defaults for key string flags (`-sf`, `-hep_addr`, …) so profile values are not wiped when the flag set is constructed.
- `-list-aliases` causes `Parse` to return [`ErrListAliases`](../internal/cli/run_profile.go); the binary exits 0 after printing names.

## See also

- [`README.md`](../README.md) — documentation index.
- [`docs/qos-reporting.md`](qos-reporting.md) — HEP media flags when combining profiles with `-send_media_report`.
