import type { SIPTraceMessage } from '@/api/v2'
import { isSIPRow } from '@/lib/sipLive'

export const LOCAL_HOST = 'local'

export interface SipCallflowHop {
  seq: number
  ts: string
  from: string
  to: string
  fromIdx: number
  toIdx: number
  label: string
  status: number
  method?: string
  callId?: string
}

export interface SipCallflowModel {
  hosts: string[]
  hops: SipCallflowHop[]
}

export function flowLabel(m: SIPTraceMessage): string {
  if (m.status && m.status > 0) {
    return m.method ? `${m.status} ${m.method}` : String(m.status)
  }
  if (m.method) return m.method
  const first = (m.summary || '').trim().split(/\s+/)[0]
  return first || 'SIP'
}

/** Build a Homer-style SIP ladder: Local column plus each peer in first-seen order. */
export function buildSipCallflow(rows: SIPTraceMessage[]): SipCallflowModel {
  const hosts: string[] = [LOCAL_HOST]
  const indexOf = (host: string): number => {
    const i = hosts.indexOf(host)
    if (i >= 0) return i
    hosts.push(host)
    return hosts.length - 1
  }

  const hops: SipCallflowHop[] = []
  for (const m of rows) {
    if (!isSIPRow(m)) continue
    const peer = (m.peer || '').trim() || 'unknown'
    const peerIdx = indexOf(peer)
    const outbound = m.dir === 'send'
    hops.push({
      seq: m.seq,
      ts: m.ts,
      from: outbound ? LOCAL_HOST : peer,
      to: outbound ? peer : LOCAL_HOST,
      fromIdx: outbound ? 0 : peerIdx,
      toIdx: outbound ? peerIdx : 0,
      label: flowLabel(m),
      status: m.status ?? 0,
      method: m.method,
      callId: m.call_id,
    })
  }
  return { hosts, hops }
}

export type HopTone = 'request' | 'provisional' | 'success' | 'redirect' | 'failure'

export function hopTone(status: number): HopTone {
  if (status >= 400) return 'failure'
  if (status >= 300) return 'redirect'
  if (status >= 200) return 'success'
  if (status >= 100) return 'provisional'
  return 'request'
}
