import type { StatsGetResponse } from '@/api/gossipper'

export type EngineStatRow = { id: string; active: number; total: number; cps: number }

type Props = {
  stats: StatsGetResponse | null
  engineRows: EngineStatRow[]
  dynamicIds: string[]
  liveWs: boolean
  liveStatus: string
  liveTs?: number
  statsJson: string
}

export function DashboardView({ stats, engineRows, dynamicIds, liveWs, liveStatus, liveTs, statsJson }: Props) {
  return (
    <div className="flex flex-col gap-4">
      <div className="border-border bg-card/80 flex flex-wrap items-center gap-3 border px-4 py-2 text-xs">
        <span className="text-muted-foreground">Live stream:</span>
        <span className={liveWs ? 'text-success' : 'text-muted-foreground'}>{liveWs ? 'on' : 'off'}</span>
        <span className="text-border">|</span>
        <span className="text-muted-foreground">channel:</span>
        <span className="font-mono text-[11px]">{liveStatus}</span>
        {liveTs ? (
          <>
            <span className="text-border">|</span>
            <span className="text-muted-foreground">ts:</span>
            <span className="font-mono text-[11px]">{liveTs}</span>
          </>
        ) : null}
        {!stats ? <span className="text-warning ml-auto">no stats yet</span> : null}
      </div>

      <div className="border-border overflow-hidden border">
        <div className="bg-muted/40 border-border text-muted-foreground border-b px-3 py-1.5 text-[11px] font-medium tracking-wide uppercase">
          Engines
        </div>
        <div className="overflow-x-auto">
          <table className="w-full border-collapse text-left text-xs">
            <thead>
              <tr className="border-border bg-muted/20 border-b">
                <th className="px-3 py-2 font-medium">id</th>
                <th className="px-3 py-2 font-medium">active</th>
                <th className="px-3 py-2 font-medium">total</th>
                <th className="px-3 py-2 font-medium">cps</th>
              </tr>
            </thead>
            <tbody>
              {engineRows.length === 0 ? (
                <tr>
                  <td colSpan={4} className="text-muted-foreground px-3 py-4">
                    No data — enable WebSocket live or stats polling
                  </td>
                </tr>
              ) : (
                engineRows.map((row) => (
                  <tr key={row.id} className="border-border/80 border-b">
                    <td className="px-3 py-2 font-mono">{row.id}</td>
                    <td className="px-3 py-2">{row.active}</td>
                    <td className="px-3 py-2">{row.total}</td>
                    <td className="px-3 py-2 font-mono">{row.cps.toFixed(2)}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
        {dynamicIds.length > 0 ? (
          <div className="text-muted-foreground border-border border-t px-3 py-2 text-[11px]">
            Dynamic clients:{' '}
            <code className="text-foreground/90">{dynamicIds.join(', ')}</code>
          </div>
        ) : null}
      </div>

      <details className="border-border group border">
        <summary className="bg-muted/30 hover:bg-muted/50 cursor-pointer px-3 py-2 text-[11px] font-medium">
          Full stats JSON
        </summary>
        <pre className="border-border max-h-[min(420px,50vh)] overflow-auto border-t p-3 font-mono text-[10px] leading-relaxed">
          {statsJson || '—'}
        </pre>
      </details>
    </div>
  )
}
