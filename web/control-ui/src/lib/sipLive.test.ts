import { describe, expect, it } from 'vitest'

import type { SIPTraceMessage } from '@/api/v2'
import {
  appendSIPTrace,
  applySIPTraceFrame,
  filenameFromDisposition,
  rowMatchesCapture,
  sipTraceExportQuery,
} from '@/lib/sipLive'

function msg(seq: number, scenario = 'REGISTER'): SIPTraceMessage {
  return {
    seq,
    ts: '2026-09-20T20:00:00Z',
    dir: 'recv',
    summary: 'OPTIONS sip:ex SIP/2.0',
    scenario,
    raw: 'OPTIONS sip:ex SIP/2.0',
  }
}

describe('sipLive', () => {
  it('caps the ring at max rows', () => {
    const out = appendSIPTrace([msg(1), msg(2)], [msg(3), msg(4)], 3)
    expect(out.map((m) => m.seq)).toEqual([2, 3, 4])
  })

  it('clears on a cleared frame', () => {
    expect(applySIPTraceFrame([msg(1)], { messages: [], next: 4, cleared: true })).toEqual([])
  })

  it('appends incremental messages', () => {
    const out = applySIPTraceFrame([msg(1)], { messages: [msg(2)], next: 2 })
    expect(out.map((m) => m.seq)).toEqual([1, 2])
  })

  it('builds export query and parses Content-Disposition', () => {
    expect(sipTraceExportQuery('pcap', 'REGISTER')).toBe(
      '/sip/trace/export?format=pcap&scenario=REGISTER',
    )
    expect(sipTraceExportQuery('text')).toBe('/sip/trace/export?format=text')
    expect(filenameFromDisposition('attachment; filename="gossipper-sip.pcap"')).toBe(
      'gossipper-sip.pcap',
    )
  })

  it('filters SIP vs app by capture flags', () => {
    const sip = msg(1)
    const app: SIPTraceMessage = { ...msg(2), kind: 'app', dir: 'app', summary: 'call started' }
    expect(rowMatchesCapture(sip, { sip: true, app: false })).toBe(true)
    expect(rowMatchesCapture(app, { sip: true, app: false })).toBe(false)
    expect(rowMatchesCapture(app, { sip: false, app: true })).toBe(true)
  })

  it('hides debug app rows when min level is info', () => {
    const dbg: SIPTraceMessage = { ...msg(2), kind: 'app', dir: 'app', level: 'debug', summary: 'send' }
    const info: SIPTraceMessage = { ...msg(3), kind: 'app', dir: 'app', level: 'info', summary: 'started' }
    const sip = msg(1)
    const cap = { sip: true, app: true, level: 'info' }
    expect(rowMatchesCapture(sip, cap)).toBe(true)
    expect(rowMatchesCapture(dbg, cap)).toBe(false)
    expect(rowMatchesCapture(info, cap)).toBe(true)
  })
})
