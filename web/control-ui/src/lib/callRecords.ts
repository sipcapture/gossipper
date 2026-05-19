export type CallRecordWebRTC = {
  codec?: string
  ice_state?: string
  rtp_packets_sent?: number
  rtp_packets_recv?: number
  rtp_packets_lost?: number
  jitter_ms?: number
  fraction_lost?: number
  offer_created?: boolean
  answer_accepted?: boolean
}

export type CallRecordLine = {
  schema_version?: string
  call_id?: string
  call_number?: number
  success?: boolean
  webrtc?: CallRecordWebRTC
}

/** Parses JSONL call_records artifact; returns last N records with webrtc block. */
export function parseCallRecordsWebRTC(text: string, limit = 5): CallRecordLine[] {
  const out: CallRecordLine[] = []
  for (const line of text.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    try {
      const rec = JSON.parse(trimmed) as CallRecordLine
      if (rec.webrtc) out.push(rec)
    } catch {
      /* skip malformed */
    }
  }
  return out.slice(-limit)
}
