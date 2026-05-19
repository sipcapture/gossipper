import { useMemo } from 'react'

import type { CallRecordLine } from '@/lib/callRecords'

export type WebRTCDiagnosticsStripProps = {
  lines: string[]
  iceServers?: string[]
  callRecords?: CallRecordLine[]
}

/** Surfaces WebRTC-related worker log lines and profile ICE hints in job monitor. */
export function WebRTCDiagnosticsStrip({ lines, iceServers, callRecords }: WebRTCDiagnosticsStripProps) {
  const webrtcLines = useMemo(
    () => lines.filter((l) => /webrtc|ice_state|dtls|srtp/i.test(l)).slice(-8),
    [lines],
  )
  const records = callRecords ?? []

  return (
    <section className="border-warning/30 bg-warning/5 flex flex-col gap-1 rounded-md border p-2">
      <div className="flex flex-wrap items-center gap-2">
        <span className="bg-warning/15 text-warning rounded px-1.5 py-0.5 text-[10px] font-medium">WebRTC</span>
        <span className="text-muted-foreground text-[10px]">WebRTC bridge — see docs/webrtc.md</span>
      </div>
      {iceServers && iceServers.length > 0 ? (
        <p className="text-muted-foreground text-[10px]">
          ICE: {iceServers.slice(0, 3).join(', ')}
          {iceServers.length > 3 ? ` (+${iceServers.length - 3})` : ''}
        </p>
      ) : (
        <p className="text-warning text-[10px]">No ICE servers on profile — add STUN/TURN for NAT traversal.</p>
      )}
      {records.length > 0 ? (
        <div className="text-muted-foreground flex flex-col gap-0.5 text-[10px]">
          <span className="font-medium">Call records (webrtc)</span>
          {records.slice(-3).map((r) => {
            const w = r.webrtc
            const iceConn = w?.ice_connection ?? w?.ice_state ?? '?'
            const pair =
              w?.selected_local && w?.selected_remote
                ? ` · pair ${w.selected_local.slice(0, 8)}↔${w.selected_remote.slice(0, 8)}`
                : ''
            const auth = w?.turn_auth && w.turn_auth !== 'none' ? ` · TURN ${w.turn_auth}` : ''
            return (
              <span key={`${r.call_id}-${r.call_number}`} className="font-mono">
                {r.call_id?.slice(0, 10) ?? '?'} · {w?.codec ?? '?'} · ICE {iceConn}
                {w?.ice_gathering ? ` · gather ${w.ice_gathering}` : ''}
                {auth}
                {pair} · sent {w?.rtp_packets_sent ?? 0} recv {w?.rtp_packets_recv ?? 0}
                {typeof w?.fraction_lost === 'number' ? ` · loss ${(w.fraction_lost * 100).toFixed(1)}%` : ''}
                {typeof w?.jitter_ms === 'number' ? ` · jitter ${w.jitter_ms.toFixed(1)}ms` : ''}
              </span>
            )
          })}
        </div>
      ) : null}
      {webrtcLines.length > 0 ? (
        <pre className="bg-muted/40 max-h-24 overflow-auto rounded p-1.5 font-mono text-[10px] whitespace-pre-wrap">
          {webrtcLines.join('\n')}
        </pre>
      ) : (
        <p className="text-muted-foreground text-[10px]">Waiting for WebRTC log lines (offer/answer/rtp_stream)…</p>
      )}
    </section>
  )
}
