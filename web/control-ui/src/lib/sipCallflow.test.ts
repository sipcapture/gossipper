import { describe, expect, it } from 'vitest'

import type { SIPTraceMessage } from '@/api/v2'
import { LOCAL_HOST, buildSipCallflow, flowLabel, hopTone } from '@/lib/sipCallflow'

function sip(partial: Partial<SIPTraceMessage> & Pick<SIPTraceMessage, 'seq' | 'dir'>): SIPTraceMessage {
  return {
    ts: '2026-09-21T09:12:10.274Z',
    summary: partial.method ? `${partial.method} sip:x SIP/2.0` : partial.status ? `SIP/2.0 ${partial.status} OK` : 'SIP',
    raw: '',
    ...partial,
  }
}

describe('buildSipCallflow', () => {
  it('places Local first and peers in first-seen order', () => {
    const model = buildSipCallflow([
      sip({ seq: 1, dir: 'recv', peer: '10.0.0.1:5060', method: 'INVITE' }),
      sip({ seq: 2, dir: 'send', peer: '10.0.0.1:5060', status: 200, method: 'INVITE' }),
      sip({ seq: 3, dir: 'recv', peer: '8.8.8.8:5060', method: 'REGISTER' }),
    ])
    expect(model.hosts).toEqual([LOCAL_HOST, '10.0.0.1:5060', '8.8.8.8:5060'])
    expect(model.hops.map((h) => [h.fromIdx, h.toIdx, h.label])).toEqual([
      [1, 0, 'INVITE'],
      [0, 1, '200 INVITE'],
      [2, 0, 'REGISTER'],
    ])
  })

  it('skips debug and RTP rows', () => {
    const model = buildSipCallflow([
      sip({ seq: 1, dir: 'app', kind: 'app', summary: 'started' }),
      sip({ seq: 2, dir: 'send', kind: 'rtp', peer: '1.2.3.4:5004', summary: 'RTP' }),
      sip({ seq: 3, dir: 'recv', peer: '10.0.0.1:5060', method: 'ACK' }),
    ])
    expect(model.hops).toHaveLength(1)
    expect(model.hops[0].label).toBe('ACK')
  })

  it('labels status vs method and tones', () => {
    expect(flowLabel(sip({ seq: 1, dir: 'send', method: 'BYE' }))).toBe('BYE')
    expect(flowLabel(sip({ seq: 2, dir: 'send', status: 180, method: 'INVITE' }))).toBe('180 INVITE')
    expect(hopTone(0)).toBe('request')
    expect(hopTone(180)).toBe('provisional')
    expect(hopTone(200)).toBe('success')
    expect(hopTone(302)).toBe('redirect')
    expect(hopTone(486)).toBe('failure')
  })
})
