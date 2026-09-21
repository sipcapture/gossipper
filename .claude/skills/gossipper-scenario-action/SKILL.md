---
name: gossipper-scenario-action
description: Use when adding or changing an XML scenario action/attribute in gossIpper (e.g. exec, rtpcheck, play_pcap_audio).
---

1. Check how SIPp defines the action/attribute; keep names and defaults compatible. If SIPp has no equivalent, prefix is not needed but document it as a gossIpper extension.
2. Write a failing parser test in internal/scenario (RED).
3. Write a failing e2e test: local UAS scenario + UAC scenario under scenarios/testdata.
4. Implement; keep the RTP hot path allocation-free.
5. Run `go test -race ./...` and the media benchmarks; compare with main.
6. Update docs/scenarios.md with an example.
