import { useCallback, useEffect, useState } from 'react'

import { getTransports, postTransports, type TransportsResponse } from '@/api/gossipper'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'

type Props = {
  bearer?: string
  busy: boolean
  /** When live WS is on and frame includes transports, show this (read-only sync from parent). */
  liveTransports: TransportsResponse | null | undefined
  liveWs: boolean
  run: <T,>(fn: () => Promise<T>) => Promise<T | undefined>
}

export function TransportsView({ bearer, busy, liveTransports, liveWs, run }: Props) {
  const [rest, setRest] = useState<TransportsResponse | null>(null)

  const load = useCallback(() => {
    void run(async () => {
      const t = await getTransports(bearer)
      setRest(t)
      return t
    })
  }, [bearer, run])

  useEffect(() => {
    void load()
  }, [bearer, load])

  useEffect(() => {
    if (!liveWs) void load()
  }, [liveWs, load])

  const effective: TransportsResponse | null =
    liveWs && liveTransports?.listeners?.length ? liveTransports : rest

  const toggle = (index: number, enabled: boolean) => {
    void run(async () => {
      const t = await postTransports({ index, enabled }, bearer)
      setRest(t)
      return t
    })
  }

  const listeners = effective?.listeners ?? []

  return (
    <div className="flex max-w-4xl flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void load()}>
          Refresh from API
        </Button>
        <span className="text-muted-foreground text-[11px]">
          {liveWs ? 'Source: live WebSocket frame (falls back to GET if empty)' : 'Source: GET /transports'}
        </span>
      </div>

      <div className="border-border overflow-hidden border">
        <table className="w-full border-collapse text-left text-xs">
          <thead>
            <tr className="border-border bg-muted/30 border-b">
              <th className="px-3 py-2 font-medium">#</th>
              <th className="px-3 py-2 font-medium">transport</th>
              <th className="px-3 py-2 font-medium">bind</th>
              <th className="px-3 py-2 font-medium">accept new</th>
            </tr>
          </thead>
          <tbody>
            {listeners.length === 0 ? (
              <tr>
                <td colSpan={4} className="text-muted-foreground px-3 py-6">
                  No listeners (client mode or data not loaded yet).
                </td>
              </tr>
            ) : (
              listeners.map((ln) => (
                <tr key={ln.index} className="border-border/80 border-b">
                  <td className="px-3 py-2 font-mono">{ln.index}</td>
                  <td className="px-3 py-2 font-mono">{ln.transport}</td>
                  <td className="px-3 py-2 font-mono">
                    {ln.local_ip}:{ln.local_port}
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex items-center gap-2">
                      <Switch
                        checked={ln.enabled}
                        disabled={busy}
                        onCheckedChange={(v) => toggle(ln.index, v)}
                      />
                      <span className="text-muted-foreground">{ln.enabled ? 'on' : 'off'}</span>
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
