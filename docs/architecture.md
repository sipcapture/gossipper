# Runtime architecture

`gossip` intentionally separates concerns that are tightly coupled inside SIPp.

## Main components

- `internal/scenario`
  - parses XML into a neutral command list
  - resolves labels and scenario mode

- `internal/template`
  - renders SIPp-style keywords without network side effects
  - computes `Content-Length` in a dedicated second pass

- `internal/sip`
  - parses SIP start line and headers
  - provides Call-ID extraction and recv matching helpers

- `internal/transport`
  - owns UDP socket implementations
  - supports shared socket mode (`u1`) and per-call socket mode (`un`)

- `internal/engine`
  - orchestrates calls, branching, retransmits, timeouts, and stats
  - keeps runtime state per call instead of using globals

- `internal/scheduler`
  - abstracts time and rate pacing
  - keeps room for future deterministic test clocks

- `internal/stats`
  - aggregates success/failure, latency, retransmits, and timeouts

- `internal/media`
  - isolated RTP helpers based on Pion
  - intentionally decoupled from SIP call execution

## Execution flow

1. CLI loads XML or a built-in scenario
2. Engine selects client or server mode
3. Transport creates shared or per-call UDP sockets
4. Each call renders a step through the template layer
5. Sent and received SIP messages are parsed through `internal/sip`
6. Stats collector aggregates final results
