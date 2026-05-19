import { describe, expect, it } from 'vitest'

import { parseSummaryJSON } from './summaryParse'

describe('parseSummaryJSON', () => {
  it('extracts KPI fields', () => {
    const k = parseSummaryJSON({
      total_calls: 10,
      success_calls: 9,
      failed_calls: 1,
      success_ratio: 0.9,
      calls_per_second: 4.2,
      retransmits: 2,
      timeouts: 1,
      media: { rtp_packets_received: 100 },
      health: { passed: true },
      findings: [],
    })
    expect(k?.total_calls).toBe(10)
    expect(k?.success_ratio).toBe(0.9)
    expect(k?.health_ok).toBe(true)
    expect(k?.rtp_packets_received).toBe(100)
  })

  it('marks health fail from findings', () => {
    const k = parseSummaryJSON({ findings: ['success ratio below threshold'] })
    expect(k?.health_ok).toBe(false)
  })
})
