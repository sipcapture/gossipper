import { useCallback, useEffect, useState } from 'react'

import {
  addV1Client,
  deleteV1Client,
  getV1Control,
  getV1Health,
  getV1Stats,
  listV1Clients,
  patchV1Control,
  type V1Control,
  type V1StatsEngine,
} from '@/api/v1'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export type ManagementV1PanelProps = {
  bearer?: string
}

export function ManagementV1Panel({ bearer }: ManagementV1PanelProps) {
  const [available, setAvailable] = useState<boolean | null>(null)
  const [control, setControl] = useState<V1Control | null>(null)
  const [stats, setStats] = useState<(V1StatsEngine & { engines?: V1StatsEngine[] }) | null>(null)
  const [clients, setClients] = useState<Awaited<ReturnType<typeof listV1Clients>> | null>(null)
  const [rateDraft, setRateDraft] = useState('')
  const [newClientJSON, setNewClientJSON] = useState('{"transport":"udp","rate":1}')
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      await getV1Health({ bearer })
      setAvailable(true)
      setControl(await getV1Control({ bearer }))
      setStats(await getV1Stats({ bearer }))
      setClients(await listV1Clients({ bearer }))
      setError(null)
    } catch (e) {
      setAvailable(false)
      setError(String(e instanceof Error ? e.message : e))
    }
  }, [bearer])

  useEffect(() => {
    void refresh()
    const t = setInterval(() => void refresh(), 5000)
    return () => clearInterval(t)
  }, [refresh])

  if (available === null) return <p className="text-muted-foreground text-xs">Probing /api/v1…</p>
  if (!available) {
    return (
      <Card className="border-border/80">
        <CardHeader>
          <CardTitle className="text-sm">Hybrid engine control (/api/v1)</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-muted-foreground text-xs">
            Legacy management API not available on this listener (typical for <code>gossipper ui</code> only).
            Run <code>gossipper server</code> with hybrid config and <code>legacy_api_v1: true</code> for rate/pause
            and dynamic clients.
          </p>
          {error ? <p className="text-muted-foreground mt-2 text-[11px]">{error}</p> : null}
        </CardContent>
      </Card>
    )
  }

  const onSetRate = () => {
    const rate = parseFloat(rateDraft)
    if (!Number.isFinite(rate) || rate <= 0) return
    void patchV1Control({ rate }, { bearer }).then(setControl)
  }

  const onPause = (paused: boolean) => void patchV1Control({ paused }, { bearer }).then(setControl)

  const onAddClient = () => {
    let body: Record<string, unknown>
    try {
      body = JSON.parse(newClientJSON) as Record<string, unknown>
    } catch {
      setError('invalid client JSON')
      return
    }
    void addV1Client(body, undefined, { bearer }).then(() => refresh())
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">Hybrid engine control (/api/v1)</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-xs">
        <div className="flex flex-wrap items-end gap-2">
          <div>
            <Label className="text-[10px]">Set rate (all engines)</Label>
            <Input className="mt-1 h-8 w-28" value={rateDraft} onChange={(e) => setRateDraft(e.target.value)} placeholder="4" />
          </div>
          <Button type="button" size="sm" onClick={onSetRate}>
            Apply rate
          </Button>
          <Button type="button" size="sm" variant="outline" onClick={() => onPause(true)}>
            Pause
          </Button>
          <Button type="button" size="sm" variant="outline" onClick={() => onPause(false)}>
            Resume
          </Button>
          <Button type="button" size="sm" variant="ghost" onClick={() => void refresh()}>
            Refresh
          </Button>
        </div>
        {control?.multi && control.engines ? (
          <ul className="font-mono space-y-0.5">
            {control.engines.map((e) => (
              <li key={e.id}>
                {e.id}: rate={e.rate} {e.paused ? 'PAUSED' : 'running'}
              </li>
            ))}
          </ul>
        ) : control ? (
          <p>
            rate={control.rate ?? '—'} paused={String(control.paused ?? false)}
          </p>
        ) : null}
        {stats?.engines ? (
          <div>
            <div className="text-muted-foreground mb-1 font-medium">Stats</div>
            <ul className="font-mono space-y-0.5">
              {stats.engines.map((e) => (
                <li key={e.id}>
                  {e.id}: {e.success_calls ?? 0}/{e.total_calls ?? 0} active={e.active_calls ?? 0} cps=
                  {(e.calls_per_second ?? 0).toFixed(2)}
                </li>
              ))}
            </ul>
          </div>
        ) : stats ? (
          <p>
            calls {stats.success_calls ?? 0}/{stats.total_calls ?? 0} cps={(stats.calls_per_second ?? 0).toFixed(2)}
          </p>
        ) : null}
        {clients?.dynamic_client_api?.can_post ? (
          <div className="flex flex-col gap-1">
            <Label className="text-[10px]">POST /api/v1/clients JSON</Label>
            <textarea
              className="border-input bg-background min-h-[80px] rounded-md border px-2 py-1 font-mono text-[11px]"
              value={newClientJSON}
              onChange={(e) => setNewClientJSON(e.target.value)}
            />
            <Button type="button" size="sm" className="self-start" onClick={onAddClient}>
              Add dynamic client
            </Button>
          </div>
        ) : null}
        {clients?.engines?.length ? (
          <ul className="space-y-1">
            {clients.engines.map((c) => (
              <li key={c.id} className="flex items-center gap-2 font-mono">
                {c.id} {c.dynamic ? '(dynamic)' : ''}
                {c.dynamic && clients.dynamic_client_api?.can_delete ? (
                  <Button type="button" size="xs" variant="destructive" onClick={() => void deleteV1Client(c.id, { bearer }).then(refresh)}>
                    Remove
                  </Button>
                ) : null}
              </li>
            ))}
          </ul>
        ) : null}
      </CardContent>
    </Card>
  )
}
