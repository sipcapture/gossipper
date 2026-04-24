# Structured event logging

Gossipper exposes a single, transport‑agnostic logger that emits structured
events from the engine: SIP send/recv, call lifecycle, authentication, recv
timeouts, unexpected SIP, action `<log>`, and engine errors. Events are written
through a ring buffer and fanned out to one or more sinks, so emission never
blocks the SIP hot path.

This document covers the runtime semantics, the `Event` schema, the list of
`Kind` values, the CLI surface, and operational guidance for tuning the buffer
under high CPS.

## Architecture

```
Engine goroutines  ──── Emit(ev) (non-blocking) ───►  RingBuffer (overwrite oldest)
                                                            │
                                                  drain goroutine
                                                            │
                          ┌─────────────────────────────────┼─────────────────────────────────┐
                          ▼                                 ▼                                 ▼
                   StdoutSink (text)                FileSink (JSONL)                  OTLPSink (otel-sdk + BatchProcessor)
                                                                                              │
                                                                          ┌───────────────────┴───────────────────┐
                                                                          ▼                                       ▼
                                                                 otlploggrpc :4317                       otlploghttp :4318
```

- **Producers**: every engine goroutine that handles a call writes to the
  shared ring with `logger.Emit(ev)`. The call is non-blocking.
- **Ring buffer**: fixed-size, `-log_buffer_size` (default `16384`), MPSC, FIFO
  while it has capacity, **overwrite oldest** when full. The number of dropped
  events is exposed via `Logger.Dropped()` and printed in the final summary so
  capacity issues are visible.
- **Drain goroutine**: serially drains the ring and fans events out to all
  configured sinks. There is exactly one drain goroutine per `Logger`.
- **Sinks**: stdout (human-readable text), file (JSON Lines), OTLP (the OTel
  SDK `BatchProcessor` ships records to a collector over gRPC or HTTP). Sinks
  are independent — a slow sink only delays its own batches; the ring keeps
  absorbing new events until it overflows.

## Event schema

```go
type Event struct {
    Time  time.Time
    Level Level                  // info | debug | warn | error
    Kind  string                 // see "Kind values" below
    Msg   string                 // short human-readable summary (e.g. "INVITE")
    Attrs map[string]any         // structured attributes
}
```

Resource attributes (`service.name`, `service.version`, `gossipper.role`,
plus all `-log_attr key=value` pairs) are attached **once** at the OTel
`LoggerProvider` level instead of being copied into every `Event.Attrs`. The
text and JSON sinks merge them into each line so all three sinks stay
symmetrical.

### Kind values

| Kind                | Level | When it fires                                                   | Notable attributes                                                                 |
|---------------------|-------|-----------------------------------------------------------------|------------------------------------------------------------------------------------|
| `engine.started`    | info  | Engine starts a run                                             | `total_calls`, `rate`                                                              |
| `engine.stopped`    | info  | Engine finishes a run                                           | `total_calls`, `successful`, `failed`, `dropped_logs`                              |
| `call.started`      | info  | A new call begins                                               | `call_id`, `call_num`                                                              |
| `call.ended`        | info  | A call ends (any outcome)                                       | `call_id`, `call_num`, `result`, `duration_ms`                                     |
| `sip.send`          | info  | Engine writes a SIP message to the wire                         | `call_id`, `call_num`, `src_ip`, `src_port`, `dst_ip`, `dst_port`, `transport`, `sip.method`, `sip.status`, `sip.reason` |
| `sip.recv`          | info  | Engine successfully matches a received SIP message              | same as `sip.send`                                                                 |
| `sip.unexpected`    | warn  | A received SIP message did not match the expected `<recv>` step | `call_id`, `call_num`, `expected`, `sip.method`, `sip.status`, `sip.reason`        |
| `auth`              | info  | Digest `[authentication]` keyword built an `Authorization` header | `call_id`, `call_num`, `username`, `realm`, `algorithm`, `qop`, `challenge.status`, `result` |
| `timeout`           | warn  | A `<recv>` (or `recvCmd`) deadline expired                      | `call_id`, `call_num`, `command.index`, `expected`, `timeout_ms`, `source`         |
| `action.log`        | info  | XML `<action><log>` step ran                                    | `Msg` carries the rendered text                                                    |
| `error`             | error | Engine reported a runtime failure                               | `call_num`, `error.kind`; `Msg` carries the human message                          |

`call.ended.result` uses the same classification as the JSON summary
(`success`, `timeout`, `unexpected_sip`, `transport_error`, `parse_error`,
`scenario_error`, `cancelled`).

### Example (stdout sink, client `NYC02` calling server `NYC01`)

```
2026-04-24T11:42:01.103Z INFO call.started call_id=ab12 call_num=1 self_tag=NYC02 peer_tag=NYC01 role=client
2026-04-24T11:42:01.123Z INFO sip.send INVITE call_id=ab12 src_ip=10.0.0.2 src_port=5060 dst_ip=10.0.0.3 dst_port=5060 sip.method=INVITE
2026-04-24T11:42:01.140Z INFO sip.recv "407 Proxy Authentication Required" call_id=ab12 sip.status=407
2026-04-24T11:42:01.141Z INFO auth realm=biloxi.com algorithm=MD5 qop=auth result=ok
2026-04-24T11:42:01.150Z INFO sip.send ACK call_id=ab12 sip.method=ACK
2026-04-24T11:42:01.155Z INFO sip.send INVITE call_id=ab12 sip.method=INVITE
2026-04-24T11:42:01.180Z INFO sip.recv "200 OK" call_id=ab12 sip.status=200
2026-04-24T11:42:01.181Z INFO sip.send ACK call_id=ab12 sip.method=ACK
2026-04-24T11:42:01.190Z INFO sip.send BYE call_id=ab12 sip.method=BYE
2026-04-24T11:42:01.205Z INFO sip.recv "200 OK" call_id=ab12 sip.status=200
2026-04-24T11:42:01.206Z INFO call.ended call_id=ab12 call_num=1 result=success duration_ms=103
```

The mirroring server (`role=server`, `self_tag=NYC01`, `peer_tag=NYC02`) sees
the same dialog from the opposite direction.

## CLI

| Flag                    | Default | Description                                                                 |
|-------------------------|---------|-----------------------------------------------------------------------------|
| `-log_otel_endpoint`    | (off)   | OTLP endpoint as `host:port` or `http(s)://host:port[/path]`. When set, OTLPSink is created. |
| `-log_otel_proto`       | `grpc`  | `grpc` (port 4317) or `http` (port 4318).                                    |
| `-log_otel_insecure`    | `false` | Disables TLS for gRPC, or forces `http://` for HTTP transport.               |
| `-log_otel_header`      | (none)  | Repeatable `key=value` header (e.g. tenant id / API key for the collector).   |
| `-log_attr`             | (none)  | Repeatable `key=value` resource attribute. Examples: `self_tag=NYC02`, `peer_tag=NYC01`, `role=client`. |
| `-log_stdout`           | `false` | Emit human-readable text events to stderr.                                   |
| `-log_file_jsonl`       | (off)   | Path to a JSON Lines file; one JSON object per event.                        |
| `-log_buffer_size`      | `16384` | Capacity of the ring buffer.                                                 |
| `-log_level`            | `info`  | Minimum severity (`debug`, `info`, `warn`, `error`).                         |

When no `-log_*` flag is provided, the logger is a zero-overhead no-op and
events are silently dropped from the producers without acquiring any lock.

## Ring-buffer sizing

Use `-log_buffer_size` to bound memory while keeping the SIP engine non-blocking.
Rough sizing rules of thumb:

- The number of events per call is typically `2*<sends> + 2` (each SIP send/recv
  plus `call.started` + `call.ended`). A vanilla UAC call (`INVITE → 200 → ACK
  → BYE → 200`) emits ~9 events.
- For sustained CPS *C* and worst-case sink latency *L* seconds, target
  `-log_buffer_size ≥ 9 * C * L`. Doubling that for headroom is reasonable.
- Watch the `dropped_logs` field in the final JSON summary. If it grows during
  steady-state load, increase the buffer or reduce the number of sinks (the
  most common hot spot is a slow OTLP collector).

## OTLP details

Both gRPC and HTTP transports are wrapped in the OTel SDK `BatchProcessor`,
which is what actually serializes records and contacts the collector. That
means standard env-var style overrides (`OTEL_EXPORTER_OTLP_TIMEOUT`,
`OTEL_EXPORTER_OTLP_HEADERS`, etc.) work out of the box on top of the explicit
flags above.

The endpoint parser is permissive:

- Bare `host:port` is treated as `http://host:port` for HTTP and as a plain
  gRPC target.
- `http://` / `https://` URLs control TLS for HTTP regardless of `-log_otel_insecure`.
- For gRPC, `-log_otel_insecure` toggles between `WithInsecure()` and the
  default credentials.

`-log_otel_header` may be passed multiple times to set custom headers (most
common: `Authorization: Bearer …` or a tenant id for multi-tenant collectors).

## Coexistence with `-trace_*`

Event logging is additive. The legacy text traces (`-trace_msg`, `-trace_err`,
`-trace_logs`, `-trace_error_codes`, …) keep working unchanged. They live next
to the structured events so you can rely on existing tooling while migrating
dashboards to OTel.
