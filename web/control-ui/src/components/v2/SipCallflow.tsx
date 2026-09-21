import { useMemo } from 'react'

import type { SIPTraceMessage } from '@/api/v2'
import { formatTraceTime } from '@/components/v2/SipMessageModal'
import { LOCAL_HOST, buildSipCallflow, hopTone, type HopTone, type SipCallflowHop } from '@/lib/sipCallflow'
import { cn } from '@/lib/utils'

const TONE: Record<HopTone, string> = {
  request: 'text-violet-600',
  provisional: 'text-muted-foreground',
  success: 'text-emerald-600',
  redirect: 'text-amber-600',
  failure: 'text-destructive',
}

export function SipCallflow({
  rows,
  onSelect,
}: {
  rows: SIPTraceMessage[]
  onSelect?: (msg: SIPTraceMessage) => void
}) {
  const model = useMemo(() => buildSipCallflow(rows), [rows])
  const bySeq = useMemo(() => {
    const m = new Map<number, SIPTraceMessage>()
    for (const row of rows) m.set(row.seq, row)
    return m
  }, [rows])

  if (model.hops.length === 0) {
    return <p className="text-muted-foreground p-3 text-xs">No SIP messages yet. Enable SIP capture and place a call.</p>
  }

  const n = model.hosts.length
  const cols = `5.75rem repeat(${n}, minmax(7.5rem, 1fr))`

  return (
    <div className="min-w-0">
      <div
        className="bg-background/80 border-border sticky top-0 z-10 grid border-b px-1 py-1.5 text-[11px] font-medium"
        style={{ gridTemplateColumns: cols }}
      >
        <div className="text-muted-foreground px-1">Time</div>
        {model.hosts.map((host) => (
          <div key={host} className="min-w-0 truncate px-1 text-center font-mono" title={host}>
            {host === LOCAL_HOST ? 'Local' : host}
          </div>
        ))}
      </div>
      {model.hops.map((hop) => (
        <FlowRow
          key={hop.seq}
          hop={hop}
          hostCount={n}
          cols={cols}
          onClick={() => {
            const msg = bySeq.get(hop.seq)
            if (msg) onSelect?.(msg)
          }}
        />
      ))}
    </div>
  )
}

function FlowRow({
  hop,
  hostCount,
  cols,
  onClick,
}: {
  hop: SipCallflowHop
  hostCount: number
  cols: string
  onClick: () => void
}) {
  const fromPct = ((hop.fromIdx + 0.5) / hostCount) * 100
  const toPct = ((hop.toIdx + 0.5) / hostCount) * 100
  const left = Math.min(fromPct, toPct)
  const width = Math.abs(toPct - fromPct)
  const rightward = toPct > fromPct
  const tone = TONE[hopTone(hop.status)]

  return (
    <button
      type="button"
      onClick={onClick}
      className="hover:bg-background/80 grid w-full items-center border-b last:border-b-0 text-left"
      style={{ gridTemplateColumns: cols }}
    >
      <span className="text-muted-foreground px-1 font-mono text-[11px] tabular-nums">{formatTraceTime(hop.ts)}</span>
      <span className="relative h-8" style={{ gridColumn: '2 / -1' }}>
        {Array.from({ length: hostCount }, (_, i) => (
          <span
            key={i}
            className="bg-border absolute top-0 bottom-0 w-px"
            style={{ left: `${((i + 0.5) / hostCount) * 100}%` }}
          />
        ))}
        <span className={cn('absolute inset-0', tone)}>
          <span
            className="absolute top-1/2 h-px bg-current"
            style={{ left: `${left}%`, width: `${Math.max(width, 1)}%`, transform: 'translateY(-50%)' }}
          />
          <span
            className={cn(
              'absolute top-1/2 h-0 w-0 border-y-[4px] border-y-transparent',
              rightward ? 'border-l-[6px] border-l-current' : 'border-r-[6px] border-r-current',
            )}
            style={{
              left: rightward ? `calc(${toPct}% - 6px)` : `calc(${toPct}% )`,
              transform: 'translateY(-50%)',
            }}
          />
          <span
            className="bg-muted/80 absolute top-0 max-w-[70%] truncate px-1 text-[11px] font-semibold"
            style={{ left: `${(fromPct + toPct) / 2}%`, transform: 'translateX(-50%)' }}
          >
            {hop.label}
          </span>
        </span>
      </span>
    </button>
  )
}
