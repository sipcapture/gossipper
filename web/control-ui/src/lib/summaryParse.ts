/** Parsed fields from worker summary.json (stats.Summary). */
export type SummaryKPI = {
  total_calls: number
  success_calls: number
  failed_calls: number
  success_ratio: number
  calls_per_second: number
  retransmits: number
  timeouts: number
  health_ok: boolean | null
  findings: string[]
  rtp_packets_received: number
  duration_ms: number | null
}

export function parseSummaryJSON(raw: unknown): SummaryKPI | null {
  if (!raw || typeof raw !== 'object') return null
  const o = raw as Record<string, unknown>
  const media = (o.media && typeof o.media === 'object' ? o.media : {}) as Record<string, unknown>
  const health = o.health && typeof o.health === 'object' ? (o.health as Record<string, unknown>) : null
  let healthOk: boolean | null = null
  if (health && typeof health.passed === 'boolean') healthOk = health.passed
  const findings = Array.isArray(o.findings)
    ? o.findings.filter((x): x is string => typeof x === 'string')
    : []
  if (healthOk === null && findings.length > 0) healthOk = false
  if (healthOk === null && findings.length === 0 && health) healthOk = true

  let durationMs: number | null = null
  if (typeof o.duration === 'number') durationMs = o.duration / 1e6
  else if (typeof o.duration === 'string') {
    const m = /^(\d+(?:\.\d+)?)(ms|s|m|h)?$/.exec(o.duration.trim())
    if (m) {
      const n = parseFloat(m[1])
      const unit = m[2] ?? 'ns'
      if (unit === 'ms') durationMs = n
      else if (unit === 's') durationMs = n * 1000
      else if (unit === 'm') durationMs = n * 60_000
      else if (unit === 'h') durationMs = n * 3_600_000
      else durationMs = n / 1e6
    }
  }

  return {
    total_calls: num(o.total_calls),
    success_calls: num(o.success_calls),
    failed_calls: num(o.failed_calls),
    success_ratio: num(o.success_ratio),
    calls_per_second: num(o.calls_per_second),
    retransmits: num(o.retransmits),
    timeouts: num(o.timeouts),
    health_ok: healthOk,
    findings,
    rtp_packets_received: num(media.rtp_packets_received),
    duration_ms: durationMs,
  }
}

function num(v: unknown): number {
  return typeof v === 'number' && Number.isFinite(v) ? v : 0
}

export function formatRatio(r: number): string {
  if (!Number.isFinite(r)) return '—'
  return `${(r * 100).toFixed(1)}%`
}
