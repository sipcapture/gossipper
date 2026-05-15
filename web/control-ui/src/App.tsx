import { useCallback, useEffect, useMemo, useState, startTransition } from 'react'

import {
  deleteDynamicClient,
  fetchAuthStatus,
  getControl,
  getDynamicClients,
  getHealth,
  getScenario,
  getStats,
  postAuthLogin,
  postControl,
  postDynamicClient,
  postScenarioApply,
  putScenario,
  type ApiErrorShape,
  type ControlGetResponse,
  type ControlState,
  type LiveFrame,
  type ScenarioGetResponse,
  type StatsGetResponse,
  type StatsSummary,
} from '@/api/gossipper'
import { PRESET_OPTIONS_CLIENT, PRESET_OPTIONS_SERVER } from '@/api/presets'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { useGossipperLive } from '@/hooks/useGossipperLive'
import { cn } from '@/lib/utils'

const LS_TOKEN = 'gossipper_control_api_token'
const LS_JWT = 'gossipper_internal_jwt'

function readStoredToken(): string {
  try {
    return localStorage.getItem(LS_TOKEN) ?? ''
  } catch {
    return ''
  }
}

function readStoredJwt(): string {
  try {
    return localStorage.getItem(LS_JWT) ?? ''
  } catch {
    return ''
  }
}

function isApiError(e: unknown): e is ApiErrorShape {
  return (
    typeof e === 'object' &&
    e !== null &&
    'status' in e &&
    'message' in e &&
    typeof (e as ApiErrorShape).message === 'string'
  )
}

function errText(e: unknown): string {
  if (isApiError(e)) return `${e.status}: ${e.message}`
  if (e instanceof Error) return e.message
  return String(e)
}

function isMultiControl(c: ControlGetResponse | null | undefined): c is {
  multi: true
  engines: { id: string; rate: number; paused: boolean }[]
} {
  return !!c && typeof c === 'object' && 'multi' in c && (c as { multi?: boolean }).multi === true
}

function primaryPaused(c: ControlGetResponse | null): boolean {
  if (!c) return false
  if (isMultiControl(c)) {
    return c.engines[0]?.paused ?? false
  }
  return (c as ControlState).paused
}

function primaryRate(c: ControlGetResponse | null): number {
  if (!c) return 0
  if (isMultiControl(c)) {
    return c.engines[0]?.rate ?? 0
  }
  return (c as ControlState).rate
}

type EngineStatRow = { id: string; active: number; total: number; cps: number }

function engineStatsRows(s: StatsGetResponse | undefined | null): EngineStatRow[] {
  if (!s) return []
  if (typeof s === 'object' && 'multi' in s && s.multi === true && Array.isArray((s as { engines?: unknown }).engines)) {
    const eng = (s as { engines: { id: string; stats: StatsSummary }[] }).engines
    return eng.map((r) => ({
      id: r.id,
      active: Number(r.stats.active_calls ?? 0),
      total: Number(r.stats.total_calls ?? 0),
      cps: Number(r.stats.calls_per_second ?? 0),
    }))
  }
  if (typeof s === 'object' && 'multi' in s && s.multi === false) {
    const st = (s as { stats: StatsSummary }).stats
    return [
      {
        id: 'load',
        active: Number(st.active_calls ?? 0),
        total: Number(st.total_calls ?? 0),
        cps: Number(st.calls_per_second ?? 0),
      },
    ]
  }
  const st = s as StatsSummary
  return [
    {
      id: 'single',
      active: Number(st.active_calls ?? 0),
      total: Number(st.total_calls ?? 0),
      cps: Number(st.calls_per_second ?? 0),
    },
  ]
}

function dynamicIdsFromStats(s: StatsGetResponse | undefined | null): string[] {
  if (!s || typeof s !== 'object') return []
  const d = (s as { dynamic_client_ids?: string[] }).dynamic_client_ids
  return Array.isArray(d) ? d : []
}

function applyLiveToState(frame: LiveFrame | null, setStats: (v: StatsGetResponse | null) => void, setControl: (v: ControlGetResponse | null) => void) {
  if (!frame) return
  if (frame.stats !== undefined) {
    setStats(frame.stats ?? null)
  }
  if (frame.control !== undefined) {
    setControl(frame.control ?? null)
  }
}

export default function App() {
  const [bearerDraft, setBearerDraft] = useState(readStoredToken)
  const [bearer, setBearer] = useState<string | undefined>(() => {
    const t = readStoredToken().trim()
    return t === '' ? undefined : t
  })

  const [authKind, setAuthKind] = useState<'none' | 'internal' | null>(null)
  const [jwt, setJwt] = useState(readStoredJwt)
  const [loginUser, setLoginUser] = useState('')
  const [loginPass, setLoginPass] = useState('')

  const effectiveBearer = useMemo(() => {
    if (authKind === 'internal') {
      const t = jwt.trim()
      return t === '' ? undefined : t
    }
    return bearer
  }, [authKind, jwt, bearer])

  const [health, setHealth] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [lastError, setLastError] = useState<string | null>(null)

  const [scenarioMeta, setScenarioMeta] = useState<ScenarioGetResponse | null>(null)
  const [scenarioXml, setScenarioXml] = useState('')

  const [control, setControl] = useState<ControlGetResponse | null>(null)
  const [rateDraft, setRateDraft] = useState('')

  const [stats, setStats] = useState<StatsGetResponse | null>(null)
  const [pollStats, setPollStats] = useState(false)

  const [uiMode, setUiMode] = useState<'server' | 'client'>('server')
  const [liveWs, setLiveWs] = useState(true)

  const { status: liveStatus, last: liveFrame, lastError: liveWsError } = useGossipperLive(
    uiMode === 'server' && liveWs,
    effectiveBearer,
  )

  const [clientSnippet, setClientSnippet] = useState(`{
  "transport": "udp",
  "local_addr": "127.0.0.1:0",
  "remote_addr": "127.0.0.1:5060",
  "scenario": "<?xml version=\\"1.0\\"?><scenario name=\\"opt\\"><send><![CDATA[OPTIONS sip:x SIP/2.0\\r\\n\\r\\n]]></send><recv response=\\"200\\" optional=\\"true\\"/></scenario>"
}`)
  const [clientWantId, setClientWantId] = useState('')
  const [dynamicList, setDynamicList] = useState<string[]>([])

  const saveToken = useCallback(() => {
    const t = bearerDraft.trim()
    if (t) {
      localStorage.setItem(LS_TOKEN, t)
      setBearer(t)
    } else {
      localStorage.removeItem(LS_TOKEN)
      setBearer(undefined)
    }
  }, [bearerDraft])

  const run = useCallback(
    async <T,>(fn: () => Promise<T>): Promise<T | undefined> => {
      setBusy(true)
      setLastError(null)
      try {
        return await fn()
      } catch (e) {
        setLastError(errText(e))
        return undefined
      } finally {
        setBusy(false)
      }
    },
    [],
  )

  const refreshHealth = useCallback(() => {
    void run(async () => {
      const h = await getHealth(effectiveBearer)
      setHealth(h.status)
    })
  }, [effectiveBearer, run])

  const refreshControl = useCallback(() => {
    void run(async () => {
      const c = await getControl(effectiveBearer)
      setControl(c)
      setRateDraft(String(primaryRate(c)))
    })
  }, [effectiveBearer, run])

  const loadScenario = useCallback(() => {
    void run(async () => {
      const s = await getScenario(effectiveBearer)
      setScenarioMeta(s)
      setScenarioXml(s.xml ?? '')
    })
  }, [effectiveBearer, run])

  const refreshStats = useCallback(() => {
    void run(async () => {
      const s = await getStats(effectiveBearer)
      setStats(s)
    })
  }, [effectiveBearer, run])

  const refreshDynamicList = useCallback(() => {
    void run(async () => {
      try {
        const r = await getDynamicClients(effectiveBearer)
        setDynamicList(r.dynamic ?? [])
      } catch {
        setDynamicList([])
      }
    })
  }, [effectiveBearer, run])

  useEffect(() => {
    void (async () => {
      try {
        const s = await fetchAuthStatus()
        setAuthKind(s.auth === 'internal' ? 'internal' : 'none')
      } catch {
        setAuthKind('none')
      }
    })()
  }, [])

  useEffect(() => {
    if (authKind === null) return
    if (authKind === 'internal' && !jwt.trim()) return
    const t = window.setTimeout(() => {
      void refreshHealth()
      void refreshControl()
      void loadScenario()
      void refreshStats()
      void refreshDynamicList()
    }, 0)
    return () => window.clearTimeout(t)
  }, [authKind, jwt, refreshHealth, refreshControl, loadScenario, refreshStats, refreshDynamicList])

  useEffect(() => {
    if (uiMode === 'server' && liveWs) {
      startTransition(() => {
        applyLiveToState(liveFrame, setStats, setControl)
        if (liveFrame?.control) {
          setRateDraft(String(primaryRate(liveFrame.control)))
        }
      })
    }
  }, [liveFrame, liveWs, uiMode])

  useEffect(() => {
    if (!pollStats || (uiMode === 'server' && liveWs)) return
    const id = window.setInterval(() => {
      void refreshStats()
    }, 2000)
    return () => window.clearInterval(id)
  }, [pollStats, refreshStats, uiMode, liveWs])

  const statsJson = useMemo(
    () => (stats ? JSON.stringify(stats, null, 2) : ''),
    [stats],
  )

  const transportsJson = useMemo(
    () => (liveFrame?.transports ? JSON.stringify(liveFrame.transports, null, 2) : ''),
    [liveFrame],
  )

  const builtin = scenarioMeta?.builtin === true
  const engineRows = useMemo(() => engineStatsRows(stats), [stats])
  const dynFromStats = useMemo(() => dynamicIdsFromStats(stats), [stats])

  const submitInternalLogin = useCallback(() => {
    void run(async () => {
      const out = await postAuthLogin(loginUser, loginPass)
      const tok = out.token.trim()
      if (tok) {
        localStorage.setItem(LS_JWT, tok)
        setJwt(tok)
        setLoginPass('')
      }
    })
  }, [loginUser, loginPass, run])

  if (authKind === null) {
    return (
      <div className="bg-background text-foreground flex min-h-screen items-center justify-center p-4">
        <p className="text-muted-foreground text-sm">Checking authentication mode…</p>
      </div>
    )
  }

  if (authKind === 'internal' && !jwt.trim()) {
    return (
      <div className="bg-background text-foreground min-h-screen p-4">
        <div className="mx-auto mt-20 w-full max-w-md">
          <Card>
            <CardHeader>
              <CardTitle>Sign in</CardTitle>
              <CardDescription>
                This server uses <code className="text-foreground/90">auth.type: internal</code> — accounts live in
                SQLite (<code>auth.sqlite_path</code>). Create a user with{' '}
                <code className="text-foreground/90">gossipper auth user-add -config …</code>
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              {lastError ? (
                <div className="border-destructive/50 bg-destructive/10 text-destructive px-3 py-2 text-xs" role="alert">
                  {lastError}
                </div>
              ) : null}
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="lu">Username</Label>
                <Input id="lu" autoComplete="username" value={loginUser} onChange={(e) => setLoginUser(e.target.value)} />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="lp">Password</Label>
                <Input
                  id="lp"
                  type="password"
                  autoComplete="current-password"
                  value={loginPass}
                  onChange={(e) => setLoginPass(e.target.value)}
                />
              </div>
              <Button type="button" disabled={busy} onClick={submitInternalLogin}>
                Sign in
              </Button>
            </CardContent>
          </Card>
        </div>
      </div>
    )
  }

  return (
    <div className="bg-background text-foreground min-h-screen">
      <div className="mx-auto flex max-w-6xl flex-col gap-4 p-4">
        <header className="flex flex-col gap-1 border-b pb-4">
          <h1 className="font-heading text-lg font-medium">Gossipper — Control</h1>
          <p className="text-muted-foreground max-w-3xl text-xs/relaxed">
            Panel for <code className="text-foreground/80">/api/v1</code>: <strong>Server</strong> mode —{' '}
            <code className="text-foreground/80">WebSocket /api/v1/live</code> stream (stats, control, transports).{' '}
            <strong>Clients</strong> mode — dynamic UAC (<code>POST/DELETE /api/v1/clients</code>) and scenario. In dev,
            Vite proxies <code className="text-foreground/80">/api</code> to <code className="text-foreground/80">VITE_API_TARGET</code> (including WS).
          </p>
        </header>

        {(lastError || liveWsError) && (
          <div
            className="border-destructive/50 bg-destructive/10 text-destructive px-3 py-2 text-xs ring-1 ring-destructive/20"
            role="alert"
          >
            {lastError}
            {lastError && liveWsError ? ' · ' : null}
            {liveWsError}
          </div>
        )}

        {authKind === 'internal' ? (
          <Card>
            <CardHeader>
              <CardTitle>Session</CardTitle>
              <CardDescription>
                <code className="text-foreground/90">auth.type: internal</code> is enabled — access is via JWT after
                sign-in. The static <code>api_token</code> from JSON is not used for API in this mode.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-wrap items-center gap-3">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={busy}
                onClick={() => {
                  try {
                    localStorage.removeItem(LS_JWT)
                  } catch {
                    /* ignore */
                  }
                  setJwt('')
                }}
              >
                Sign out
              </Button>
              <Button type="button" variant="outline" size="sm" disabled={busy} onClick={refreshHealth}>
                Health
              </Button>
              <span className="text-muted-foreground text-xs">
                status:{' '}
                <span className={cn(health === 'ok' ? 'text-success' : 'text-foreground')}>
                  {health ?? '—'}
                </span>
              </span>
            </CardContent>
          </Card>
        ) : (
          <Card>
            <CardHeader>
              <CardTitle>Connection</CardTitle>
              <CardDescription>
                With <code>-api_token</code>, the token is sent as Bearer and in the WebSocket query string{' '}
                <code>?token=</code>.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3 sm:flex-row sm:items-end">
              <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                <Label htmlFor="token">API token (optional)</Label>
                <Input
                  id="token"
                  type="password"
                  autoComplete="off"
                  placeholder="Bearer secret"
                  value={bearerDraft}
                  onChange={(e) => setBearerDraft(e.target.value)}
                />
              </div>
              <Button type="button" variant="secondary" disabled={busy} onClick={saveToken}>
                Save token
              </Button>
            </CardContent>
            <CardFooter className="flex flex-wrap gap-2">
              <Button type="button" variant="outline" size="sm" disabled={busy} onClick={refreshHealth}>
                Health
              </Button>
              <span className="text-muted-foreground self-center text-xs">
                status:{' '}
                <span className={cn(health === 'ok' ? 'text-success' : 'text-foreground')}>
                  {health ?? '—'}
                </span>
              </span>
            </CardFooter>
          </Card>
        )}

        <Tabs value={uiMode} onValueChange={(v) => setUiMode(v as 'server' | 'client')} className="w-full">
          <TabsList variant="line" className="w-full max-w-lg">
            <TabsTrigger value="server">Server (live)</TabsTrigger>
            <TabsTrigger value="client">Clients / load</TabsTrigger>
          </TabsList>

          <TabsContent value="server" className="mt-4 flex flex-col gap-4">
            <Card>
              <CardHeader>
                <CardTitle>WebSocket stream</CardTitle>
                <CardDescription>
                  Frames ~750&nbsp;ms: stats, control, transports (primary). Token is passed in the query string{' '}
                  <code>?token=</code> on connect.
                </CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-3">
                <div className="flex flex-wrap items-center gap-3">
                  <Switch id="liveon" checked={liveWs} onCheckedChange={setLiveWs} />
                  <Label htmlFor="liveon">Live WebSocket</Label>
                  <span className="text-muted-foreground text-xs">
                    channel:{' '}
                    <span className="text-foreground/90">
                      {liveWs ? liveStatus : 'off'}
                      {liveFrame?.ts ? ` · ts ${liveFrame.ts}` : ''}
                    </span>
                  </span>
                </div>
                {!liveWs ? (
                  <div className="flex flex-wrap items-center gap-2">
                    <Switch id="poll2" checked={pollStats} onCheckedChange={setPollStats} />
                    <Label htmlFor="poll2">Poll GET /stats every 2s</Label>
                    <Button type="button" size="sm" variant="outline" disabled={busy} onClick={refreshStats}>
                      Stats now
                    </Button>
                    <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={refreshControl}>
                      Control
                    </Button>
                  </div>
                ) : null}
              </CardContent>
            </Card>

            <div className="grid gap-4 lg:grid-cols-2">
              <Card>
                <CardHeader>
                  <CardTitle>Engines</CardTitle>
                  <CardDescription>Summary from the latest stats frame (or polling).</CardDescription>
                </CardHeader>
                <CardContent className="overflow-x-auto">
                  <table className="w-full border-collapse text-left text-xs">
                    <thead>
                      <tr className="border-b">
                        <th className="py-1.5 pr-2 font-medium">id</th>
                        <th className="py-1.5 pr-2 font-medium">active</th>
                        <th className="py-1.5 pr-2 font-medium">total</th>
                        <th className="py-1.5 font-medium">cps</th>
                      </tr>
                    </thead>
                    <tbody>
                      {engineRows.length === 0 ? (
                        <tr>
                          <td colSpan={4} className="text-muted-foreground py-2">
                            No data
                          </td>
                        </tr>
                      ) : (
                        engineRows.map((row) => (
                          <tr key={row.id} className="border-b border-border/60">
                            <td className="py-1.5 pr-2 font-mono">{row.id}</td>
                            <td className="py-1.5 pr-2">{row.active}</td>
                            <td className="py-1.5 pr-2">{row.total}</td>
                            <td className="py-1.5">{row.cps.toFixed(2)}</td>
                          </tr>
                        ))
                      )}
                    </tbody>
                  </table>
                  {dynFromStats.length > 0 ? (
                    <p className="text-muted-foreground mt-2 text-[11px]">
                      Dynamic client ids:{' '}
                      <code className="text-foreground/90">{dynFromStats.join(', ')}</code>
                    </p>
                  ) : null}
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <CardTitle>Control</CardTitle>
                  <CardDescription>
                    <code>POST /api/v1/control</code> applies to all engines. In multi mode, the table below shows the
                    actual per-engine state (from live).
                  </CardDescription>
                </CardHeader>
                <CardContent className="flex flex-col gap-4">
                  {control ? (
                    <div className="flex flex-col gap-3">
                      <div className="flex flex-wrap items-center gap-3">
                        <Label htmlFor="paused" className="shrink-0">
                          Pause (all)
                        </Label>
                        <Switch
                          id="paused"
                          checked={primaryPaused(control)}
                          onCheckedChange={(v) => {
                            void run(async () => {
                              const next = await postControl({ paused: v }, effectiveBearer)
                              setControl(next)
                              setRateDraft(String(primaryRate(next)))
                            })
                          }}
                        />
                      </div>
                      <div className="flex flex-wrap items-end gap-2">
                        <div className="flex min-w-[120px] flex-col gap-1.5">
                          <Label htmlFor="rate">Rate primary (calls/s)</Label>
                          <Input
                            id="rate"
                            type="number"
                            step="any"
                            value={rateDraft}
                            onChange={(e) => setRateDraft(e.target.value)}
                          />
                        </div>
                        <Button
                          type="button"
                          size="sm"
                          variant="secondary"
                          disabled={busy}
                          onClick={() => {
                            const n = Number(rateDraft)
                            if (Number.isNaN(n)) return
                            void run(async () => {
                              const next = await postControl({ rate: n }, effectiveBearer)
                              setControl(next)
                              setRateDraft(String(primaryRate(next)))
                            })
                          }}
                        >
                          Apply rate (all)
                        </Button>
                        <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={refreshControl}>
                          Refresh
                        </Button>
                      </div>
                      {isMultiControl(control) ? (
                        <div className="border-input bg-muted/20 max-h-40 overflow-auto border p-2 text-[11px]">
                          <p className="text-muted-foreground mb-1">Per engine (read-only from API):</p>
                          <ul className="font-mono leading-snug">
                            {control.engines.map((e) => (
                              <li key={e.id}>
                                {e.id}: rate {e.rate.toFixed(2)}, paused {e.paused ? 'yes' : 'no'}
                              </li>
                            ))}
                          </ul>
                        </div>
                      ) : null}
                    </div>
                  ) : (
                    <p className="text-muted-foreground text-xs">control: no data</p>
                  )}
                </CardContent>
              </Card>
            </div>

            <Card>
              <CardHeader>
                <CardTitle>Transports (primary)</CardTitle>
                <CardDescription>From the latest live frame (empty when WS is off).</CardDescription>
              </CardHeader>
              <CardContent>
                <pre className="border-input bg-muted/20 max-h-48 overflow-auto border p-2 font-mono text-[10px] leading-snug">
                  {transportsJson || '—'}
                </pre>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Raw stats JSON</CardTitle>
              </CardHeader>
              <CardContent>
                <pre className="border-input bg-muted/20 max-h-56 overflow-auto border p-2 font-mono text-[10px] leading-snug">
                  {statsJson || '—'}
                </pre>
              </CardContent>
            </Card>
          </TabsContent>

          <TabsContent value="client" className="mt-4 flex flex-col gap-4">
            <Card>
              <CardHeader>
                <CardTitle>Dynamic clients</CardTitle>
                <CardDescription>
                  <code>POST /api/v1/clients</code> — UAC JSON snippet; <code>DELETE ?id=</code> — stop. List:{' '}
                  <code>GET /api/v1/clients</code>.
                </CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-3">
                <div className="flex flex-wrap gap-2">
                  <Button type="button" size="sm" variant="outline" disabled={busy} onClick={refreshDynamicList}>
                    Refresh list
                  </Button>
                </div>
                <ul className="text-sm">
                  {(dynamicList.length ? dynamicList : dynFromStats).length === 0 ? (
                    <li className="text-muted-foreground text-xs">No dynamic clients</li>
                  ) : (
                    (dynamicList.length ? dynamicList : dynFromStats).map((id) => (
                      <li key={id} className="flex items-center justify-between gap-2 border-b border-border/50 py-1">
                        <code className="text-xs">{id}</code>
                        <Button
                          type="button"
                          size="sm"
                          variant="destructive"
                          disabled={busy}
                          onClick={() =>
                            run(async () => {
                              await deleteDynamicClient(id, effectiveBearer)
                              await refreshDynamicList()
                              await refreshStats()
                            })
                          }
                        >
                          Remove
                        </Button>
                      </li>
                    ))
                  )}
                </ul>
                <div className="grid gap-2 sm:grid-cols-2">
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="cid">Desired id (optional)</Label>
                    <Input
                      id="cid"
                      className="font-mono text-xs"
                      value={clientWantId}
                      onChange={(e) => setClientWantId(e.target.value)}
                      placeholder="e.g. extra-1"
                    />
                  </div>
                </div>
                <Label htmlFor="snippet">Client JSON snippet</Label>
                <Textarea
                  id="snippet"
                  className="font-mono min-h-[200px] text-[11px] leading-snug"
                  spellCheck={false}
                  value={clientSnippet}
                  onChange={(e) => setClientSnippet(e.target.value)}
                />
                <Button
                  type="button"
                  disabled={busy}
                  onClick={() =>
                    run(async () => {
                      const wid = clientWantId.trim()
                      await postDynamicClient(clientSnippet, {
                        id: wid === '' ? undefined : wid,
                        bearer: effectiveBearer,
                      })
                      await refreshDynamicList()
                      await refreshStats()
                    })
                  }
                >
                  Start client
                </Button>
                <p className="text-muted-foreground text-[11px]">
                  To change the scenario for a running client: remove its id and create again with the same id (stop +
                  add).
                </p>
              </CardContent>
            </Card>

            <Card className="min-h-[320px]">
              <CardHeader>
                <CardTitle>Scenario XML (primary)</CardTitle>
                <CardDescription>
                  <code>GET/PUT /api/v1/scenario</code>, <code>POST …/apply</code> — same as before.
                </CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-2">
                {scenarioMeta ? (
                  <p className="text-muted-foreground text-xs">
                    file: <code>{scenarioMeta.scenario_file || '—'}</code>, name:{' '}
                    <code>{scenarioMeta.scenario_name || '—'}</code>
                    {builtin ? (
                      <span className="text-warning ml-2">(built-in — PUT disabled)</span>
                    ) : null}
                  </p>
                ) : null}
                <Tabs defaultValue="editor">
                  <TabsList variant="line" className="w-full max-w-md">
                    <TabsTrigger value="editor">Editor</TabsTrigger>
                    <TabsTrigger value="preset-uac">UAC preset</TabsTrigger>
                    <TabsTrigger value="preset-uas">UAS preset</TabsTrigger>
                  </TabsList>
                  <TabsContent value="editor" className="mt-2">
                    <Textarea
                      className="font-mono min-h-[220px] text-[11px] leading-snug"
                      spellCheck={false}
                      value={scenarioXml}
                      onChange={(e) => setScenarioXml(e.target.value)}
                    />
                  </TabsContent>
                  <TabsContent value="preset-uac" className="mt-2">
                    <pre className="border-input bg-muted/30 max-h-48 overflow-auto border p-2 font-mono text-[11px] leading-snug whitespace-pre-wrap">
                      {PRESET_OPTIONS_CLIENT}
                    </pre>
                    <Button
                      type="button"
                      className="mt-2"
                      size="sm"
                      variant="secondary"
                      onClick={() => setScenarioXml(PRESET_OPTIONS_CLIENT)}
                    >
                      Insert into editor
                    </Button>
                  </TabsContent>
                  <TabsContent value="preset-uas" className="mt-2">
                    <pre className="border-input bg-muted/30 max-h-48 overflow-auto border p-2 font-mono text-[11px] leading-snug whitespace-pre-wrap">
                      {PRESET_OPTIONS_SERVER}
                    </pre>
                    <Button
                      type="button"
                      className="mt-2"
                      size="sm"
                      variant="secondary"
                      onClick={() => setScenarioXml(PRESET_OPTIONS_SERVER)}
                    >
                      Insert into editor
                    </Button>
                  </TabsContent>
                </Tabs>
              </CardContent>
              <CardFooter className="flex flex-wrap gap-2">
                <Button type="button" size="sm" variant="outline" disabled={busy} onClick={loadScenario}>
                  Fetch from server
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={busy || builtin}
                  onClick={() =>
                    run(async () => {
                      await putScenario(scenarioXml, { apply: false, bearer: effectiveBearer })
                      await loadScenario()
                    })
                  }
                >
                  Save to file
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="secondary"
                  disabled={busy || builtin}
                  onClick={() =>
                    run(async () => {
                      await putScenario(scenarioXml, { apply: true, bearer: effectiveBearer })
                      await loadScenario()
                      await refreshControl()
                    })
                  }
                >
                  Save + apply
                </Button>
                <Button
                  type="button"
                  size="sm"
                  disabled={busy}
                  onClick={() =>
                    run(async () => {
                      await postScenarioApply(scenarioXml, effectiveBearer)
                      await refreshControl()
                    })
                  }
                >
                  Apply
                </Button>
              </CardFooter>
            </Card>
          </TabsContent>
        </Tabs>
      </div>
    </div>
  )
}
