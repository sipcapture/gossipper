# Gossipper profiler analysis (pprof)

**Date:** 2026-03-16  
**Profiles:** CPU, Heap (alloc_space)  
**Workload:** UAC 500 calls, ~44 cps, UAS at 127.0.0.1:35060

---

## Update (2026-03-16): chan *sip.Message + use-after-free fix

### Transport channel refactoring

Completed migration from `chan sip.Message` to `chan *sip.Message` with pool usage:

- `mailboxRegistry.mailboxes`: `map[string]chan *sip.Message`
- `dispatch` / `dispatchMessages` / `dispatchMessagePtr` — operate on `*sip.Message`
- `serverUDPSession.inbox`, `udpServerInbound.msg` — `chan *sip.Message`
- `receive` in `executeCall` — returns `(*sip.Message, error)`
- `runClientSharedTCP` / `runClientSharedTLS` — "inbox-first" receive

### Use-after-free fix (waitForMatch)

**Bug:** with optional recv (`<recv response="180" optional="true"/>`) on mismatch:
1. `stash(msg, fromPending=false)` added `msg` to `pending`
2. Then `sip.PutMessage(msg)` returned it to the pool
3. `pending` held a "dead" pointer → next recv read garbage

**Fix** (`engine.go`, `waitForMatch`):
```go
if cmd.Optional {
    // Free only if stash did NOT take ownership
    if stash == nil || fromPending {
        sip.PutMessage(msg)
    }
    return nil, errOptionalRecvMismatch
}
```

This fixed three failing tests: `TestEngineRunsTCPScenario`, `TestEngineRunsTLSScenario`, `TestEngineOptionalRecvShortCircuitsOnUnexpected`.

### Benchmarks (Intel Core Ultra 9 185H, 22 threads)

File: `internal/sip/message_test.go`

#### Sequential run

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| `OldDispatchPath` (Parse + Copy) | 1328 | 1696 | **21** |
| `NewUDPDispatchPath` (ParseInto + pool) | 1045 | 1585 | **14** |
| `NewDispatchPath` (Parse + CopyInto, TCP) | 1355 | 1697 | 21 |
| `CopyInto` standalone | 301 | 112 | 7 |

**UDP path: −21% latency, −7 allocs/op, −111 B/op** (intermediate `Copy()` removed)

#### Parallel run (GOMAXPROCS=22)

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| `OldDispatchParallel` | 580 | 1696 | 21 |
| `NewUDPDispatchParallel` | 530 | 1585 | 14 |

**−8.6% latency under load** (bottleneck is string operations in parser, not struct allocation)

---

## Update (2026-03-16): sip.Parse unification + RenderMessage fast path + pprof

### sip.Parse — unification with byte scanner

**Change:** `Parse` now uses the same `parseBytes` helper as `ParseInto`. Shared code, shared performance.

**Result:** `Parse` sped up from **1004 ns → 864 ns** (−14%), **1584 B → 1176 B** (−26%).

### RenderMessage / RenderMessageStrict — fast path for bodiless messages

**Problem:** `RenderMessage` always ran two renders — first to compute `BodyLength`, second final — even when the template lacked `[len]` (typical INVITE/ACK/BYE without body).

**Fix:** added `hasLenToken(raw string) bool` (O(n) byte scan), which skips the second render when `[len]` is absent.

**Benchmarks:**

| Scenario | ns/op before | ns/op after | B/op before | B/op after |
|----------|--------------|-------------|-------------|------------|
| `RenderMessageStrict` without `[len]` (fast path) | ~4162 | **1761** (−58%) | ~3398 | **1298** (−62%) |
| `RenderMessageStrict` with `[len]` (unchanged) | 4162 | 4162 | 3398 | 3398 |

Most SIP messages (INVITE without SDP, ACK, BYE, registrations) have no `[len]` → **−58% latency, −62% B/op** for typical traffic.

### New pprof profile (2026-03-16 — after all optimizations)

**Benchmark:** `BenchmarkEngineUACUDPThroughput` — full UAC cycle (INVITE→200→ACK→pause→BYE→200) over UDP, 1 call per iteration, mock UAS in same process.  
**Result:** ~51 ms/op (including network round-trips), 34 KB/op, 338 allocs/op.

#### CPU (top flat, after optimizations)

| Function | flat% | cum% | Description |
|----------|-------|------|-------------|
| `syscall.Syscall6` | 17.95% | — | UDP sockets (I/O bound — expected) |
| `runtime.stealWork` | 5.13% | 15.38% | Goroutine scheduler |
| `net.UDPConn.readFrom` | 2.56% | — | UDP read |
| other | <3% | — | Various runtime costs |

**CPU conclusion:** application is fully I/O-bound. SIP parsing and rendering **do not appear in CPU top**. All optimizations met their goals.

#### Heap (alloc_space, top — after optimizations)

| Function | flat | cum | Change vs before |
|----------|------|-----|------------------|
| `runtime.mallocgc` | 12.82% | — | — (baseline) |
| `sip.parseBytes` | **11.18%** | — | ↓ was 19.7% (`sip.Parse`) = **−43%** |
| `transport.NewSharedUDP` | 9.63% | — | test overhead (new conn each iteration) |
| `executeCall` | 6.39% | 24.02% | — |
| `pprof.StartCPUProfile` | 5.50% | — | profiling overhead |
| `wrapSIPSend` | 4.79% | — | SIP payload → trace/HEP |
| `MakeNoZero` | **1.60%** | — | ↓ was **17.8%** = **−91%** |
| `template.*` | **not in top** | — | ↓ was 28-31% = **fully eliminated** |
| `regexp.*` | **not in top** | — | ↓ was 6.3% = **fully eliminated** |

### Summary of key metrics

| Metric | Before | After | Δ |
|--------|--------|-------|---|
| `sip.Parse` alloc | 19.7% | 11.18% | **−43%** |
| `MakeNoZero` (strings.Builder) | 17.8% | 1.60% | **−91%** |
| `regexp` in render | 6.3% | 0% | **−100%** |
| `template.RenderStrict` (cum) | 31.6% | ~0% | **−100%** |
| CPU hot-path | executeCall/render | syscall I/O | Application I/O-bound |

---

### Conclusions

1. **TCP dispatch** (`dispatchMessages`): marginal gain — `CopyInto` is equivalent to `Copy` in allocs since the packet template is already parsed by SharedTCP. Benefit is semantic clarity (explicit object lifetime).
2. **UDP dispatch** (`dispatch`): **−33% allocs** (21→14) — removed `Message.Copy()`, parsing goes directly into pool object.
3. **Pool (`sync.Pool`) in isolation**: no visible gain in micro-benchmark since `ParseInto` still does `msg.Headers = make(...)`. Real benefit — reduced GC pressure at high CPS.
4. **Next step**: removing `msg.Headers = make(...)` from `ParseInto` (restoring delete-loop) can save 1 more alloc; safe only with guaranteed no concurrent reads — requires verification.

---

## Update (2026-03-16): ParseInto byte-scanner + trace race fix

### ParseInto: ReplaceAll+Split replaced with direct byte scanner

**Change (`internal/sip/message.go`):**
- `strings.ReplaceAll(string(raw), "\r\n", "\n")` + `strings.Split(...)` replaced with direct byte scanner over `raw` (no intermediate strings).
- `msg.Headers = make(...)` replaced with delete-loop to reuse existing map.

**Benchmark results (Intel Core Ultra 9 185H):**

| Benchmark | ns/op before | ns/op after | B/op before | B/op after | allocs before | allocs after |
|-----------|--------------|-------------|-------------|------------|---------------|--------------|
| `ParseIntoPool` | 1051 | **777** (−26%) | 1585 | **776** (−51%) | 14 | 17* |
| `NewUDPDispatchPath` | 1013 | **761** (−25%) | 1585 | **776** (−51%) | 14 | 17* |
| `NewUDPDispatchParallel` | 530 | **289** (−45%) | 1585 | **776** (−51%) | 14 | 17* |

*Alloc count slightly increased due to per-string conversion, but total memory halved → lower GC pressure.

**File:** `internal/sip/message.go`

### trace.go: data race fix in loop goroutines

**Problem:** goroutines `startStatsLoop`, `startCountsLoop`, `startScreenLoop` accessed fields `t.statsStop`, `t.countsStop`, `t.screenStop` via `select <-t.fieldName`. Meanwhile `Close()` nulled these fields (`t.statsStop = nil`) — data race detected by race detector.

**Fix (`internal/engine/trace.go`):** channels captured in local variables before starting goroutine:
```go
stop := t.statsStop
done := t.statsDone
go func() {
    defer close(done)
    ...
    case <-stop:  // no longer reads t.statsStop
    ...
}()
```

Applied to all three loop functions. All tests with `-race` now pass cleanly.



---

## Current bottlenecks (2026-03-16, after ParseInto)

### CPU (top flat)

| Function | flat% | cum% | Description |
|----------|-------|------|-------------|
| indexbytebody | 11.5 | - | String search (IndexByte) |
| Syscall6 / futex | 11.5 + 7.7 | - | Syscall, locks |
| executeCall | 3.9 | **38.5** | Scenario execution |
| renderSIPMessage | - | 15.4 | SIP template render |
| template.RenderStrict / expandTokensStrict | - | 15.4 | Variable substitution |
| waitForMatch | - | 11.5 | SIP response wait |
| mailboxRegistry.dispatch | - | 11.5 | Call-ID routing |
| MakeNoZero | - | 11.5 | String allocations |

### Allocations (alloc_space, top)

| Function | flat | cum | Description |
|----------|------|-----|-------------|
| MakeNoZero / strings.Builder.grow | **29.8%** | - | String buffers in render |
| mallocgc | 19.9 | - | General allocation |
| sip.ParseInto | 8.3 | **14.9** | SIP parsing (pool+Copy) |
| sip.Message.Copy | 3.3 | - | Copy on channel send |
| executeCall | 5.0 | **43.1** | Scenario |
| renderSIPMessage | - | **33.1** | Template render |
| template.RenderStrict / expandTokensStrict | - | **28.1** | [$var] variables |
| dispatch (mailboxRegistry) | - | 16.6 | Routing |
| ensureMessageTerminator | 5.0 | - | SIP CRLF |
| strings.genSplit | 5.0 | - | split on parse |

### Recommendations

1. **template.RenderStrict / expandTokensStrict** (~28% alloc) — replace regexp with `strings.Replace` or map lookup for `[$var]`, reuse buffers.
2. **MakeNoZero / strings.Builder** (30% alloc) — pool of string buffers for rendering.
3. **sip.Message.Copy** — still required when passing between goroutines; pool already helps with ParseInto.
4. **ensureMessageTerminator / genSplit** — simplify without extra split/allocations.

---

## Update (2026-03-16): sip.Parse → ParseInto (pool)

Introduced `GetMessage` / `ParseInto` / `PutMessage` (SIP struct pool) to reduce allocations:

- **engine_test.go** — all mock servers (~25 sites)
- **main_test.go** — mock servers (6 sites); `readSIPMessage` kept on `sip.Parse` (returns message)
- **engine.go** — local INVITE check (~line 1335)

**Limitation:** in hot-path (engine.go:417, 758, 876, 1717) messages are sent over channels to other goroutines — pool cannot be used without:
  - switching to `chan *sip.Message` and `PutMessage` in receivers, or
  - introducing `sip.Message.Copy()` and copying before send.

---

## Update (2026-03-16): after reuse buffer

After optimizing `varStore.Snapshot` (scratch buffer) profile was re-taken:
- **Workload:** UAC, 3000 calls, rate ~91 cps
- **Result:** `varStore.Snapshot` and `makemap` no longer in **top** of allocs/CPU

### Current bottlenecks (alloc_space)

| Function | flat | cum | Description |
|----------|------|-----|-------------|
| `sip.Parse` | 19.7% | 32.0% | SIP message parsing (partially replaced with ParseInto) |
| `MakeNoZero` | 17.8% | 17.8% | String allocations |
| `executeCall` | 7.5% | 62.0% | Scenario execution |
| `regexp.ReplaceAllStringFunc` | 6.3% | 19.7% | Variable substitution in template |
| `regexp.replaceAll` | 6.3% | 13.4% | Regex in render |
| `strings.genSplit` | 4.7% | 4.7% | split in string processing |
| `ensureMessageTerminator` | 3.5% | 3.5% | SIP CRLF |
| `template.Context.RenderStrict` | 2.0% | 31.6% | SIP template render |

### Further optimization recommendations

1. **sip.Parse** (~40 MB cum) — lightweight zero-copy parser or object pool for SIP structs
2. **regexp** (ReplaceAllStringFunc in template) — replace with simple strings.Replace or map lookup for `[$var]`
3. **template.RenderStrict** — reuse string buffers, avoid extra split
4. **strings.genSplit** — consider strings.Cut or manual parsing without allocations

---

## CPU profile (historical)

### General stats

- **Duration:** 35.73 s
- **Samples captured:** 20 ms (0.056%) — profile taken under light load
- **Mode:** UAC (runClientShared), baseline scenario

### Call chains (traces)

```
10ms  runtime.netpoll
      → runtime.findRunnable → runtime.schedule → runtime.park_m → runtime.mcall

10ms  runtime.newobject
      → runtime.makemap
      → github.com/qxip/gossipper/internal/engine.(*varStore).Snapshot
      → github.com/qxip/gossipper/internal/engine.(*Engine).executeCall
      → github.com/qxip/gossipper/internal/engine.(*Engine).runClientShared.func1
```

### CPU conclusions

| Source | Share | Interpretation |
|--------|-------|----------------|
| `runtime.netpoll` | 50% | Network wait (recv/send) — normal for I/O-bound |
| `runtime.newobject` + `makemap` | 50% | Allocations in `varStore.Snapshot` during `executeCall` |

**Main application hotspot:** `varStore.Snapshot` — on each scenario step a copy of the variable map (`makemap`) was created. At high CPS this adds noticeable load on GC and CPU.

---

## Heap profile

### Top allocations (inuse_space)

| Function | Size | % |
|----------|------|---|
| `runtime.mallocgc` | 3084 kB | 57.55% |
| `runtime/pprof.StartCPUProfile` | 1763 kB | 32.90% |
| `os.runtime_args` | 512 kB | 9.56% |

**Note:** `StartCPUProfile` uses ~1.7 MB — profiling buffer. In production without `-cpuprofile` this is not allocated.

---

## Recommendations

1. ~~**varStore.Snapshot:**~~ **Done:** scratch buffer in `vars.go` — map reuse instead of `make(map)` per Snapshot.
2. **Network:** `netpoll` dominates — bottleneck is network I/O, not CPU. Switching to gnet unlikely to help much without clear connection limits.
3. **Next candidates:** sip.Parse, regexp in template, template.RenderStrict — see table above.

---

## Reproduction commands

```bash
# CPU + heap to files
go run ./cmd/gossip -sn uac -rsa 127.0.0.1:5060 -m 2000 -r 50 -cpuprofile cpu.prof -memprofile mem.prof

# Interactive view
go tool pprof -http=:8080 cpu.prof
go tool pprof -http=:8080 mem.prof
```
