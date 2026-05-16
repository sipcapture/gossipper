import { useCallback, useEffect, useState } from 'react'

import {
  deleteDynamicClient,
  getTransports,
  postDynamicClient,
  postTransports,
  type ScenarioGetResponse,
  type TransportsResponse,
} from '@/api/gossipper'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

export type TransportsSection = 'servers' | 'sip_clients'

export type SipClientDraftProps = {
  snippet: string
  wantId: string
  onSnippet: (v: string) => void
  onWantId: (v: string) => void
}

export type ServerScenarioControls = {
  builtin: boolean
  scenarioMeta: ScenarioGetResponse | null
  scenarioXml: string
  onScenarioXml: (v: string) => void
  onLoad: () => Promise<void>
  onSaveFile: () => Promise<void>
  onSaveApply: () => Promise<void>
  onApply: () => Promise<void>
}

type Props = {
  section: TransportsSection
  bearer?: string
  busy: boolean
  /** When live WS is on and frame includes transports, prefer this snapshot. */
  liveTransports: TransportsResponse | null | undefined
  liveWs: boolean
  run: <T,>(fn: () => Promise<T>) => Promise<T | undefined>
  /** Primary server scenario XML controls (SIP servers tab). */
  serverScenario?: ServerScenarioControls
  /** Shared draft for POST /clients (SIP clients tab only). */
  sipClientDraft?: SipClientDraftProps
  /** After starting/stopping a dynamic client or external engine change. */
  onClientsMutated?: () => void
}

const defaultCaps = { can_post: false, can_delete: false }

const LISTENER_PROFILE_HINT = `Example (composite JSON): add or extend "server.listeners", then restart gossipper.

{
  "server": {
    "role": "management",
    "scenario_name": "management",
    "listeners": [
      { "transport": "u1", "local_ip": "0.0.0.0", "local_port": 5060 },
      { "transport": "t1", "local_ip": "0.0.0.0", "local_port": 5061 }
    ]
  }
}`

type ServerAddTab = 'listener' | 'client'

export function TransportsView({
  section,
  bearer,
  busy,
  liveTransports,
  liveWs,
  run,
  serverScenario,
  sipClientDraft,
  onClientsMutated,
}: Props) {
  const [rest, setRest] = useState<TransportsResponse | null>(null)
  const [serverAddOpen, setServerAddOpen] = useState(false)
  const [serverAddTab, setServerAddTab] = useState<ServerAddTab>('listener')
  const [clientAddOpen, setClientAddOpen] = useState(false)

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

  useEffect(() => {
    setServerAddOpen(false)
    setClientAddOpen(false)
  }, [section])

  useEffect(() => {
    if (!serverAddOpen && !clientAddOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setServerAddOpen(false)
        setClientAddOpen(false)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [serverAddOpen, clientAddOpen])

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

  const stopDynamicClient = (id: string) => {
    void run(async () => {
      await deleteDynamicClient(id, bearer)
      const t = await getTransports(bearer)
      setRest(t)
      onClientsMutated?.()
      return t
    })
  }

  const submitNewClient = () => {
    const d = sipClientDraft
    if (!d) return
    void run(async () => {
      const wid = d.wantId.trim()
      await postDynamicClient(d.snippet, {
        id: wid === '' ? undefined : wid,
        bearer,
      })
      const t = await getTransports(bearer)
      setRest(t)
      onClientsMutated?.()
      setServerAddOpen(false)
      setClientAddOpen(false)
      return t
    })
  }

  const listeners = effective?.listeners ?? []
  const clients = effective?.clients ?? []
  const caps = effective?.dynamic_client_api ?? defaultCaps

  const bumpTransportTable = async () => {
    const t = await getTransports(bearer)
    setRest(t)
  }

  const isServers = section === 'servers'
  const showSipClientChrome = section === 'sip_clients'

  const ss = serverScenario
  const xmlTrim = ss ? ss.scenarioXml.trim() : ''
  const hasScenarioFile = Boolean(ss?.scenarioMeta?.scenario_file?.trim())
  const canSaveToDisk = ss ? !ss.builtin && xmlTrim !== '' : false
  const canApply = ss ? xmlTrim !== '' || (hasScenarioFile && !ss.builtin) : false

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
        <>
          <div className="border-border overflow-hidden border">
            <div className="bg-muted/40 border-border text-muted-foreground flex items-center justify-between gap-2 border-b px-3 py-1.5">
              <span className="text-[11px] font-medium tracking-wide uppercase">Server listeners (UAS)</span>
              <Button
                type="button"
                size="icon-sm"
                variant="outline"
                className="shrink-0 font-mono"
                aria-label="Add listener or engine"
                title="Add listener or engine"
                onClick={() => {
                  setServerAddTab((effective?.dynamic_client_api ?? defaultCaps).can_post ? 'client' : 'listener')
                  setServerAddOpen(true)
                }}
              >
                +
              </Button>
            </div>
            <table className="w-full border-collapse text-left text-xs">
              <thead>
                <tr className="border-border bg-muted/30 border-b">
                  <th className="px-3 py-2 font-medium">#</th>
                  <th className="px-3 py-2 font-medium">scenario</th>
                  <th className="px-3 py-2 font-medium">transport</th>
                  <th className="px-3 py-2 font-medium">bind</th>
                  <th className="px-3 py-2 font-medium">accept new</th>
                </tr>
              </thead>
              <tbody>
                {listeners.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="text-muted-foreground px-3 py-6">
                      No listeners (client-only process or data not loaded yet).
                    </td>
                  </tr>
                ) : (
                  listeners.map((ln) => (
                    <tr key={ln.index} className="border-border/80 border-b">
                      <td className="px-3 py-2 font-mono">{ln.index}</td>
                      <td className="px-3 py-2 font-mono">{ln.scenario_name || '—'}</td>
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

          {ss ? (
            <div className="border-border flex max-w-4xl flex-col gap-2 border p-3">
              <p className="text-muted-foreground text-[11px] leading-relaxed">
                All binds on the <span className="text-foreground/80 font-medium">primary</span> server engine share one
                live SIP scenario. Use per-listener switches only to stop accepting <span className="italic">new</span>{' '}
                dialogs on a socket. Paste or edit XML below, then apply — same APIs as the{' '}
                <span className="text-foreground/80 font-medium">Scenario</span> tab.
              </p>
              {ss.builtin ? (
                <p className="text-muted-foreground text-[11px] leading-relaxed">
                  Built-in scenario: GET does not return XML — paste a scenario or use a preset, or run with{' '}
                  <code className="text-foreground/80">-sf</code> for file-backed hot reload.
                </p>
              ) : null}
              <div className="flex flex-wrap gap-2 border-b pb-2">
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      await ss.onLoad()
                      await bumpTransportTable()
                    })
                  }
                >
                  Load from server
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={busy || !canSaveToDisk}
                  title={!canSaveToDisk ? (ss.builtin ? 'Built-in scenario' : 'Editor is empty') : undefined}
                  onClick={() =>
                    void run(async () => {
                      await ss.onSaveFile()
                      await bumpTransportTable()
                    })
                  }
                >
                  Write to file
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="secondary"
                  disabled={busy || !canSaveToDisk}
                  title={!canSaveToDisk ? (ss.builtin ? 'Built-in scenario' : 'Editor is empty') : undefined}
                  onClick={() =>
                    void run(async () => {
                      await ss.onSaveApply()
                      await bumpTransportTable()
                    })
                  }
                >
                  Write and apply
                </Button>
                <Button
                  type="button"
                  size="sm"
                  disabled={busy || !canApply}
                  title={
                    !canApply
                      ? ss.builtin
                        ? 'Built-in: paste XML or use -sf for empty apply'
                        : 'Empty editor and no -sf file on server'
                      : xmlTrim === '' && hasScenarioFile
                        ? 'Re-read -sf file and hot-reload'
                        : undefined
                  }
                  onClick={() =>
                    void run(async () => {
                      await ss.onApply()
                      await bumpTransportTable()
                    })
                  }
                >
                  Apply (hot reload)
                </Button>
              </div>
              <Label htmlFor="srv-scen-xml" className="text-[11px]">
                Primary server scenario XML
              </Label>
              <Textarea
                id="srv-scen-xml"
                className="font-mono min-h-[160px] text-xs"
                value={ss.scenarioXml}
                onChange={(e) => ss.onScenarioXml(e.target.value)}
                spellCheck={false}
              />
            </div>
          ) : null}
        </>
      ) : (
        <>
          <div className="border-border overflow-hidden border">
            <div className="bg-muted/40 border-border text-muted-foreground flex items-center justify-between gap-2 border-b px-3 py-1.5">
              <span className="text-[11px] font-medium tracking-wide uppercase">Client engines (UAC / load)</span>
              {showSipClientChrome ? (
                <Button
                  type="button"
                  size="icon-sm"
                  variant="outline"
                  className="shrink-0 font-mono"
                  aria-label="Add client engine"
                  title="Add client engine"
                  onClick={() => setClientAddOpen(true)}
                >
                  +
                </Button>
              ) : null}
            </div>
            <table className="w-full border-collapse text-left text-xs">
              <thead>
                <tr className="border-border bg-muted/30 border-b">
                  <th className="px-3 py-2 font-medium">id</th>
                  <th className="px-3 py-2 font-medium">scenario</th>
                  <th className="px-3 py-2 font-medium">transport</th>
                  <th className="px-3 py-2 font-medium">local bind</th>
                  <th className="px-3 py-2 font-medium">remote</th>
                  <th className="px-3 py-2 font-medium">scheduling</th>
                  <th className="px-3 py-2 font-medium"> </th>
                </tr>
              </thead>
              <tbody>
                {clients.length === 0 ? (
                  <tr>
                    <td colSpan={7} className="text-muted-foreground px-3 py-6">
                      No client engines (server-only process or data not loaded yet).
                      {caps.can_post ? (
                        <> Use the + button to start a dynamic UAC via POST /clients.</>
                      ) : null}
                    </td>
                  </tr>
                ) : (
                  clients.map((c) => (
                    <tr key={c.id} className="border-border/80 border-b">
                      <td className="px-3 py-2 font-mono">{c.id}</td>
                      <td className="px-3 py-2 font-mono">{c.scenario_name || '—'}</td>
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
                          <span className="text-muted-foreground max-w-[12rem] text-[10px] leading-tight">
                            Pause/resume new calls for this engine id (same as load control).
                          </span>
                        </div>
                      </td>
                      <td className="px-3 py-2">
                        {c.dynamic && caps.can_delete ? (
                          <Button
                            type="button"
                            size="sm"
                            variant="destructive"
                            disabled={busy}
                            onClick={() => stopDynamicClient(c.id)}
                          >
                            Stop
                          </Button>
                        ) : c.dynamic ? (
                          <span className="text-muted-foreground text-[10px]">stop N/A</span>
                        ) : (
                          <span className="text-muted-foreground text-[10px]">static</span>
                        )}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </>
      )}

      {serverAddOpen ? (
        <div
          className="fixed inset-0 z-[100] flex items-center justify-center bg-black/60 p-3"
          role="presentation"
          onMouseDown={() => setServerAddOpen(false)}
        >
          <div
            className="border-border bg-background max-h-[88vh] w-full max-w-lg overflow-y-auto border shadow-lg"
            role="dialog"
            aria-modal="true"
            aria-labelledby="srv-add-title"
            onMouseDown={(e) => e.stopPropagation()}
          >
            <div className="border-border flex items-center justify-between gap-2 border-b px-3 py-2">
              <h2 id="srv-add-title" className="text-sm font-semibold tracking-tight">
                Add listener or engine
              </h2>
              <Button type="button" variant="ghost" size="icon-sm" aria-label="Close" onClick={() => setServerAddOpen(false)}>
                ×
              </Button>
            </div>
            <div className="flex flex-col gap-3 p-3">
              {caps.can_post ? (
                <div className="flex flex-wrap gap-1">
                  <Button
                    type="button"
                    size="sm"
                    variant={serverAddTab === 'client' ? 'secondary' : 'outline'}
                    onClick={() => setServerAddTab('client')}
                  >
                    UAC engine (API)
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant={serverAddTab === 'listener' ? 'secondary' : 'outline'}
                    onClick={() => setServerAddTab('listener')}
                  >
                    Server listener (profile)
                  </Button>
                </div>
              ) : null}

              {!caps.can_post || serverAddTab === 'listener' ? (
                <div className="flex flex-col gap-2">
                  <p className="text-muted-foreground text-[11px] leading-relaxed">
                    Extra SIP server sockets are not opened at runtime. Add binds under{' '}
                    <code className="text-foreground/80">server.listeners</code> in the JSON profile (or flat{' '}
                    <code className="text-foreground/80">listeners</code> on the server object), then restart gossipper.
                  </p>
                  <pre className="border-border bg-muted/30 max-h-52 overflow-auto border p-2 font-mono text-[10px] leading-snug whitespace-pre-wrap">
                    {LISTENER_PROFILE_HINT}
                  </pre>
                </div>
              ) : sipClientDraft ? (
                <div className="flex flex-col gap-3">
                  <p className="text-muted-foreground text-[11px] leading-relaxed">
                    Starts another UAC via <code className="text-foreground/80">POST /api/v1/clients</code> (same as Dynamic clients).
                  </p>
                  <div className="grid gap-2">
                    <Label htmlFor="srv-add-cid">Desired id (optional)</Label>
                    <Input
                      id="srv-add-cid"
                      className="font-mono text-xs"
                      value={sipClientDraft.wantId}
                      onChange={(e) => sipClientDraft.onWantId(e.target.value)}
                      placeholder="e.g. load-2"
                    />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="srv-add-snippet">Client JSON snippet</Label>
                    <Textarea
                      id="srv-add-snippet"
                      className="font-mono min-h-[140px] text-xs"
                      value={sipClientDraft.snippet}
                      onChange={(e) => sipClientDraft.onSnippet(e.target.value)}
                      placeholder='{"transport":"udp", ...}'
                    />
                  </div>
                  <div className="flex flex-wrap justify-end gap-2">
                    <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => setServerAddOpen(false)}>
                      Cancel
                    </Button>
                    <Button type="button" size="sm" disabled={busy} onClick={() => void submitNewClient()}>
                      Start engine
                    </Button>
                  </div>
                </div>
              ) : (
                <p className="text-muted-foreground text-[11px]">Client draft is not wired.</p>
              )}
            </div>
          </div>
        </div>
      ) : null}

      {clientAddOpen && showSipClientChrome ? (
        <div
          className="fixed inset-0 z-[100] flex items-center justify-center bg-black/60 p-3"
          role="presentation"
          onMouseDown={() => setClientAddOpen(false)}
        >
          <div
            className="border-border bg-background max-h-[88vh] w-full max-w-lg overflow-y-auto border shadow-lg"
            role="dialog"
            aria-modal="true"
            aria-labelledby="cli-add-title"
            onMouseDown={(e) => e.stopPropagation()}
          >
            <div className="border-border flex items-center justify-between gap-2 border-b px-3 py-2">
              <h2 id="cli-add-title" className="text-sm font-semibold tracking-tight">
                Add client engine
              </h2>
              <Button type="button" variant="ghost" size="icon-sm" aria-label="Close" onClick={() => setClientAddOpen(false)}>
                ×
              </Button>
            </div>
            <div className="flex flex-col gap-3 p-3">
              {!caps.can_post ? (
                <p className="text-muted-foreground text-[11px] leading-relaxed">
                  Dynamic <code className="text-foreground/80">POST /clients</code> is not enabled (needs management{' '}
                  <code className="text-foreground/80">api_addr</code> with load coordinator). Static client engines still appear in the table when
                  defined in the run profile.
                </p>
              ) : sipClientDraft ? (
                <>
                  <p className="text-muted-foreground text-[11px] leading-relaxed">
                    <code className="text-foreground/80">POST /api/v1/clients</code> with a JSON snippet. Only dynamic rows can be stopped from this
                    UI.
                  </p>
                  <div className="grid gap-2">
                    <Label htmlFor="cli-add-cid">Desired id (optional)</Label>
                    <Input
                      id="cli-add-cid"
                      className="font-mono text-xs"
                      value={sipClientDraft.wantId}
                      onChange={(e) => sipClientDraft.onWantId(e.target.value)}
                      placeholder="e.g. load-2"
                    />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="cli-add-snippet">Client JSON snippet</Label>
                    <Textarea
                      id="cli-add-snippet"
                      className="font-mono min-h-[140px] text-xs"
                      value={sipClientDraft.snippet}
                      onChange={(e) => sipClientDraft.onSnippet(e.target.value)}
                      placeholder='{"transport":"udp", ...}'
                    />
                  </div>
                  <div className="flex flex-wrap justify-end gap-2">
                    <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => setClientAddOpen(false)}>
                      Cancel
                    </Button>
                    <Button type="button" size="sm" disabled={busy} onClick={() => void submitNewClient()}>
                      Start engine
                    </Button>
                  </div>
                </>
              ) : (
                <p className="text-muted-foreground text-[11px]">Client draft is not wired.</p>
              )}
            </div>
          </div>
        </div>
      ) : null}
    </div>
  )
}
