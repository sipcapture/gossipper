import { describe, expect, it } from 'vitest'

import { parseCallRecordsWebRTC } from '@/lib/callRecords'

describe('parseCallRecordsWebRTC', () => {
  it('keeps only lines with webrtc block', () => {
    const jsonl = [
      '{"call_id":"a","success":true}',
      '{"call_id":"b","webrtc":{"codec":"PCMA","ice_state":"connected"}}',
      '{"call_id":"c","webrtc":{"rtp_packets_sent":3}}',
    ].join('\n')
    const rows = parseCallRecordsWebRTC(jsonl)
    expect(rows).toHaveLength(2)
    expect(rows[0].call_id).toBe('b')
    expect(rows[1].webrtc?.rtp_packets_sent).toBe(3)
  })
})
