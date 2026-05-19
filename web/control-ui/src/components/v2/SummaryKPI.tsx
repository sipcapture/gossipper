import type { SummaryKPI } from '@/lib/summaryParse'
import { formatRatio } from '@/lib/summaryParse'

export function SummaryKPICards({ kpi }: { kpi: SummaryKPI }) {
  return (
    <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
      <Kpi label="Calls" value={`${kpi.success_calls}/${kpi.total_calls}`} hint={`failed ${kpi.failed_calls}`} />
      <Kpi label="Success" value={formatRatio(kpi.success_ratio)} />
      <Kpi label="CPS" value={kpi.calls_per_second.toFixed(2)} />
      <Kpi
        label="Health"
        value={kpi.health_ok === null ? '—' : kpi.health_ok ? 'PASS' : 'FAIL'}
        hint={kpi.findings[0]}
        bad={kpi.health_ok === false}
        good={kpi.health_ok === true}
      />
      <Kpi label="Retrans" value={String(kpi.retransmits)} />
      <Kpi label="Timeouts" value={String(kpi.timeouts)} />
      <Kpi label="RTP recv" value={String(kpi.rtp_packets_received)} />
      {kpi.duration_ms != null ? <Kpi label="Duration" value={`${(kpi.duration_ms / 1000).toFixed(1)}s`} /> : null}
    </div>
  )
}

function Kpi({
  label,
  value,
  hint,
  bad,
  good,
}: {
  label: string
  value: string
  hint?: string
  bad?: boolean
  good?: boolean
}) {
  return (
    <div
      className={`rounded-md border px-2 py-1.5 ${bad ? 'border-destructive/40 bg-destructive/10' : good ? 'border-success/40 bg-success/10' : 'border-border bg-muted/30'}`}
    >
      <div className="text-muted-foreground text-[10px] uppercase">{label}</div>
      <div className="text-sm font-semibold">{value}</div>
      {hint ? <div className="text-muted-foreground truncate text-[10px]">{hint}</div> : null}
    </div>
  )
}
