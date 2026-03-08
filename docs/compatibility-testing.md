# Compatibility testing strategy

The most valuable tests for `gossip` are behavior-level compatibility checks,
not only unit tests.

## Layers

1. Unit tests
   - XML parser
   - template rendering
   - SIP parsing
   - RTP packet generation

2. Integration tests
   - run `gossip` against a lightweight UDP SIP stub
   - verify sent request order, response matching, retransmit behavior, and summary stats

3. Side-by-side compatibility tests
   - execute representative SIPp scenarios from `sipp/docs`
   - compare resulting SIP messages, branch progression, and observed timing windows

## Recommended reference scenarios

- basic UAC call flow
- basic UAS responder
- optional provisional responses
- label-based branching
- timeout and retransmit cases

## Golden artifacts

Keep expected SIP exchanges under `testdata/` as plain text golden fixtures so
both the parser and engine can be regression-tested deterministically.
