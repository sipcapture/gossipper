# Compatibility testing strategy

The most valuable tests for `Gossipper` are behavior-level compatibility checks,
not only unit tests.

## Layers

1. Unit tests
   - XML parser
   - template rendering
   - SIP parsing
   - auth / HEP helpers
   - RTP packet generation

2. Integration tests
   - run `gossipper` against a lightweight SIP stub over UDP/TCP/TLS as needed
   - verify sent request order, response matching, retransmit behavior, XML
     actions, and summary stats
   - verify trace outputs such as `-trace_stat`, `-trace_rtt`, `-trace_logs`,
     `-trace_err`, and HEP export

3. Side-by-side compatibility tests
   - execute representative SIPp scenarios from `sipp/docs`
   - compare resulting SIP messages, branch progression, and observed timing windows

## Recommended reference scenarios

- basic UAC call flow
- basic UAS responder
- optional provisional responses
- label-based branching
- action-heavy XML flows (`ereg`, `lookup`, arithmetic, auth validation)
- HEP export smoke coverage
- timeout and retransmit cases

## Golden artifacts

Keep expected SIP exchanges under `testdata/` as plain text golden fixtures so
both the parser and engine can be regression-tested deterministically.

For CSV/JSON/HEP outputs, prefer assertions on stable semantic fields rather
than byte-for-byte full-file snapshots, so the tests stay resilient when new
columns or metadata are added intentionally.

For `-trace_stat` / `-trace_rtt` / `-trace_screen`, treat the CSV header as an explicit contract:
assert exact column names and order against `docs/trace-schema-contract.md`.

For `-trace_error_codes`, keep a smoke assertion that verifies the CSV header
and at least one expected unexpected-response row in the artifact.

For `-trace_screen`, keep a smoke assertion that verifies the CSV header and at
least one runtime snapshot row in the artifact.

For parser migrations, prefer an external mapping adapter over adding runtime
legacy alias columns in generated CSV traces.
