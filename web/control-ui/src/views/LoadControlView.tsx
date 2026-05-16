import type { ControlGetResponse } from '@/api/gossipper'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

type Props = {
  busy: boolean
  liveWs: boolean
  onLiveWs: (v: boolean) => void
  pollStats: boolean
  onPollStats: (v: boolean) => void
  liveStatus: string
  control: ControlGetResponse | null
  primaryPaused: boolean
  rateDraft: string
  onRateDraft: (v: string) => void
  onTogglePause: (v: boolean) => void
  onApplyRate: () => void
  onRefreshControl: () => void
  onRefreshStats: () => void
  isMulti: boolean
  multiEngines: { id: string; rate: number; paused: boolean }[]
}

export function LoadControlView({
  busy,
  liveWs,
  onLiveWs,
  pollStats,
  onPollStats,
  liveStatus,
  control,
  primaryPaused,
  rateDraft,
  onRateDraft,
  onTogglePause,
  onApplyRate,
  onRefreshControl,
  onRefreshStats,
  isMulti,
  multiEngines,
}: Props) {
  return (
    <div className="flex max-w-3xl flex-col gap-6">
      <section className="border-border space-y-3 border p-4">
        <h3 className="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">Data source</h3>
        <div className="flex flex-wrap items-center gap-4">
          <div className="flex items-center gap-2">
            <Switch id="liveon" checked={liveWs} onCheckedChange={onLiveWs} />
            <Label htmlFor="liveon" className="text-sm">
              WebSocket <code className="text-xs">/live</code>
            </Label>
          </div>
          <span className="text-muted-foreground font-mono text-[11px]">{liveStatus}</span>
        </div>
        {!liveWs ? (
          <div className="flex flex-wrap items-center gap-3 border-t border-border/60 pt-3">
            <div className="flex items-center gap-2">
              <Switch id="poll2" checked={pollStats} onCheckedChange={onPollStats} />
              <Label htmlFor="poll2" className="text-sm">
                Poll <code className="text-xs">GET /stats</code> every 2s
              </Label>
            </div>
            <Button type="button" size="sm" variant="outline" disabled={busy} onClick={onRefreshStats}>
              Stats now
            </Button>
            <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={onRefreshControl}>
              Control
            </Button>
          </div>
        ) : null}
      </section>

      <section className="border-border space-y-4 border p-4">
        <h3 className="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">Load control</h3>
        {control ? (
          <>
            <div className="flex flex-wrap items-center gap-4">
              <div className="flex items-center gap-2">
                <Label htmlFor="paused">Pause (all engines)</Label>
                <Switch id="paused" checked={primaryPaused} onCheckedChange={onTogglePause} />
              </div>
            </div>
            <div className="flex flex-wrap items-end gap-2">
              <div className="flex min-w-[140px] flex-col gap-1.5">
                <Label htmlFor="rate">Rate (calls/s), primary</Label>
                <Input
                  id="rate"
                  type="number"
                  step="any"
                  className="font-mono"
                  value={rateDraft}
                  onChange={(e) => onRateDraft(e.target.value)}
                />
              </div>
              <Button type="button" size="sm" variant="secondary" disabled={busy} onClick={onApplyRate}>
                Apply rate (all)
              </Button>
              <Button type="button" size="sm" variant="outline" disabled={busy} onClick={onRefreshControl}>
                Refresh
              </Button>
            </div>
            {isMulti ? (
              <div className="border-border bg-muted/15 max-h-48 overflow-auto border p-3 font-mono text-[11px] leading-relaxed">
                <p className="text-muted-foreground mb-2">Per engine:</p>
                <ul>
                  {multiEngines.map((e) => (
                    <li key={e.id}>
                      {e.id}: rate {e.rate.toFixed(2)}, paused {e.paused ? 'yes' : 'no'}
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
          </>
        ) : (
          <p className="text-muted-foreground text-sm">No control data — refresh or enable live WebSocket.</p>
        )}
      </section>
    </div>
  )
}
