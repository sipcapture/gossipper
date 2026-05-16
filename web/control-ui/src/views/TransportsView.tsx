import { useCallback, useEffect, useState } from 'react'

import { getTransports, postTransports, type TransportsResponse } from '@/api/gossipper'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'

export type TransportsSection = 'servers' | 'sip_clients'

type Props = {
  section: TransportsSection
  bearer?: string
  busy: boolean
  /** When live WS is on and frame includes transports, prefer this snapshot. */
  liveTransports: TransportsResponse | null | undefined
  liveWs: boolean
  run: <T,>(fn: () => Promise<T>) => Promise<T | undefined>
}

export function TransportsView({ section, bearer, busy, liveTransports, liveWs, run }: Props) {
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
    liveWs && liveTransports != null ? liveTransports : rest

  const toggleListener = (index: number, enabled: boolean) => {
    void run(async () => {
      const t = await postTransports({ index, enabled }, bearer)
      setRest(t)
      return t
    })
  }

  const toggleClient = (id: string, accepting: boolean) => {
    void run(async () => {
      const t = await postTransports({ clients: [{ id, accepting }] }, bearer)
      setRest(t)
      return t
    })
  }

  const listeners = effective?.listeners ?? []
  const clients = effective?.clients ?? []

  const isServers = section === 'servers'

  return (
    <div className="flex max-w-4xl flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void load()}>
          Refresh from API
        </Button>
        <span className="text-muted-foreground text-[11px]">
          {liveWs ? 'Source: live WebSocket frame (when connected)' : 'Source: GET /transports'}
        </span>
      </div>

      {isServers ? (
        <div className="border-border overflow-hidden border">
          <div className="bg-muted/40 border-border text-muted-foreground border-b px-3 py-1.5 text-[11px] font-medium tracking-wide uppercase">
            Server listeners (UAS)
          </div>
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
                    No listeners (client-only process or data not loaded yet).
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
                          onCheckedChange={(v) => toggleListener(ln.index, v)}
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
      ) : (
        <div className="border-border overflow-hidden border">
          <div className="bg-muted/40 border-border text-muted-foreground border-b px-3 py-1.5 text-[11px] font-medium tracking-wide uppercase">
            Client engines (UAC / load)
          </div>
          <table className="w-full border-collapse text-left text-xs">
            <thead>
              <tr className="border-border bg-muted/30 border-b">
                <th className="px-3 py-2 font-medium">id</th>
                <th className="px-3 py-2 font-medium">transport</th>
                <th className="px-3 py-2 font-medium">local bind</th>
                <th className="px-3 py-2 font-medium">remote</th>
                <th className="px-3 py-2 font-medium">scheduling</th>
              </tr>
            </thead>
            <tbody>
              {clients.length === 0 ? (
                <tr>
                  <td colSpan={5} className="text-muted-foreground px-3 py-6">
                    No client engines (server-only process or data not loaded yet). Use{' '}
                    <span className="text-foreground/80 font-medium">Dynamic clients</span> to add load engines when
                    enabled.
                  </td>
                </tr>
              ) : (
                clients.map((c) => (
                  <tr key={c.id} className="border-border/80 border-b">
                    <td className="px-3 py-2 font-mono">{c.id}</td>
                    <td className="px-3 py-2 font-mono">{c.transport}</td>
                    <td className="px-3 py-2 font-mono">
                      {c.local_ip}:{c.local_port}
                    </td>
                    <td className="px-3 py-2 font-mono">{c.remote_addr}</td>
                    <td className="px-3 py-2">
                      <div className="flex flex-col gap-0.5">
                        <div className="flex items-center gap-2">
                          <Switch
                            checked={c.accepting}
                            disabled={busy}
                            onCheckedChange={(v) => toggleClient(c.id, v)}
                          />
                          <span className="text-muted-foreground">{c.accepting ? 'active' : 'paused'}</span>
                        </div>
                        <span className="text-muted-foreground max-w-[14rem] text-[10px] leading-tight">
                          Pauses new outbound calls (same as load control pause for that engine).
                        </span>
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
