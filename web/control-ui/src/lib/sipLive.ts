import type { SIPTraceCapture, SIPTraceMessage, SIPTraceResponse } from '@/api/v2'

export function sipTraceExportQuery(format: 'text' | 'pcap', scenario?: string): string {
  const q = new URLSearchParams()
  q.set('format', format)
  if (scenario) q.set('scenario', scenario)
  return `/sip/trace/export?${q.toString()}`
}

export function filenameFromDisposition(header: string | null): string | undefined {
  if (!header) return undefined
  const star = /filename\*=(?:UTF-8''|)([^;]+)/i.exec(header)
  if (star) {
    try {
      return decodeURIComponent(star[1].trim().replace(/^"+|"+$/g, ''))
    } catch {
      return star[1].trim()
    }
  }
  const plain = /filename="?([^";]+)"?/i.exec(header)
  return plain?.[1]?.trim()
}

export const LOG_LEVELS = ['debug', 'info', 'warn', 'error'] as const

export function isSIPRow(row: SIPTraceMessage): boolean {
  return row.kind !== 'app'
}

export function levelRank(level?: string): number {
  switch ((level ?? '').toLowerCase()) {
    case 'info':
      return 1
    case 'warn':
    case 'warning':
      return 2
    case 'error':
    case 'err':
      return 3
    default:
      return 0
  }
}

export function rowMatchesCapture(row: SIPTraceMessage, capture: SIPTraceCapture): boolean {
  if (isSIPRow(row)) return capture.sip
  if (!capture.app) return false
  return levelRank(row.level) >= levelRank(capture.level)
}

export function appendSIPTrace(
  prev: SIPTraceMessage[],
  incoming: SIPTraceMessage[],
  max = 400,
): SIPTraceMessage[] {
  if (incoming.length === 0) return prev
  const next = prev.concat(incoming)
  return next.length > max ? next.slice(next.length - max) : next
}

export function applySIPTraceFrame(
  prev: SIPTraceMessage[],
  frame: SIPTraceResponse,
  max = 400,
): SIPTraceMessage[] {
  if (frame.cleared) return []
  return appendSIPTrace(prev, frame.messages ?? [], max)
}
