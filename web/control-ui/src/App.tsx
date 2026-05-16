import { useCallback, useEffect, useLayoutEffect, useMemo, useState, startTransition } from 'react'

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
import { ThemeToggle, type ThemeMode } from '@/components/ThemeToggle'
import { useGossipperLive } from '@/hooks/useGossipperLive'
import { cn } from '@/lib/utils'
import { ClientsView } from '@/views/ClientsView'
import { DashboardView } from '@/views/DashboardView'
import { LoadControlView } from '@/views/LoadControlView'
import { ScenarioView } from '@/views/ScenarioView'
import { SessionView } from '@/views/SessionView'
import { TransportsView } from '@/views/TransportsView'

const LS_TOKEN = 'gossipper_control_api_token'
const LS_JWT = 'gossipper_internal_jwt'
/** Current key: `light` | `dark`. Legacy `gossipper_control_dark` is still read once for migration. */
const LS_THEME = 'gossipper_control_theme'
const LS_THEME_LEGACY = 'gossipper_control_dark'

type NavId = 'dashboard' | 'scenario' | 'load' | 'servers' | 'sip_clients' | 'clients' | 'session'

const NAV: { id: NavId; label: string; hint: string }[] = [
  { id: 'dashboard', label: 'Dashboard', hint: 'engine summary and stats' },
  { id: 'scenario', label: 'Scenario', hint: 'XML, save, hot reload' },
  { id: 'load', label: 'Load control', hint: 'pause, rate, live / poll' },
  { id: 'servers', label: 'SIP servers', hint: 'UAS listeners' },
  { id: 'sip_clients', label: 'SIP clients', hint: 'UAC binds and load engines' },
  { id: 'clients', label: 'Dynamic clients', hint: 'start/stop extra UAC engines' },
  { id: 'session', label: 'Session', hint: 'token, theme, health' },
]

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

function readTheme(): ThemeMode {
  try {
    const t = localStorage.getItem(LS_THEME)?.trim().toLowerCase()
    if (t === 'light' || t === 'dark') return t
    const leg = localStorage.getItem(LS_THEME_LEGACY)
    if (leg === '0') return 'light'
    if (leg === '1') return 'dark'
  } catch {
    /* ignore */
  }
  return 'dark'
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
  const [nav, setNav] = useState<NavId>('dashboard')
  const [theme, setTheme] = useState<ThemeMode>(readTheme)

  useLayoutEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark')
    try {
      localStorage.setItem(LS_THEME, theme)
    } catch {
      /* ignore */
    }
  }, [theme])

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

  const [liveWs, setLiveWs] = useState(true)

  const { status: liveStatus, last: liveFrame, lastError: liveWsError } = useGossipperLive(liveWs, effectiveBearer)

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
    if (liveWs) {
      startTransition(() => {
        applyLiveToState(liveFrame, setStats, setControl)
        if (liveFrame?.control) {
          setRateDraft(String(primaryRate(liveFrame.control)))
        }
      })
    }
  }, [liveFrame, liveWs])

  useEffect(() => {
    if (!pollStats || liveWs) return
    const id = window.setInterval(() => {
      void refreshStats()
    }, 2000)
    return () => window.clearInterval(id)
  }, [pollStats, refreshStats, liveWs])

  const statsJson = useMemo(() => (stats ? JSON.stringify(stats, null, 2) : ''), [stats])

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

  const multiEngines = isMultiControl(control) ? control.engines : []

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
        <div className="mx-auto mt-16 w-full max-w-md border border-border p-6">
          <h1 className="mb-1 text-base font-semibold tracking-tight">Sign in</h1>
          <p className="text-muted-foreground mb-4 text-xs leading-relaxed">
            <code className="text-foreground/90">auth.type: internal</code> is enabled. Create users with{' '}
            <code className="text-foreground/90">gossipper auth user-add -config …</code>
          </p>
          {lastError ? (
            <div className="border-destructive/50 bg-destructive/10 text-destructive mb-3 px-3 py-2 text-xs" role="alert">
              {lastError}
            </div>
          ) : null}
          <div className="mb-3 flex flex-col gap-1.5">
            <label className="text-muted-foreground text-xs" htmlFor="lu">
              Username
            </label>
            <input
              id="lu"
              autoComplete="username"
              value={loginUser}
              onChange={(e) => setLoginUser(e.target.value)}
              className="border-input bg-background focus-visible:ring-ring rounded-md border px-3 py-2 text-sm outline-none focus-visible:ring-2"
            />
          </div>
          <div className="mb-4 flex flex-col gap-1.5">
            <label className="text-muted-foreground text-xs" htmlFor="lp">
              Password
            </label>
            <input
              id="lp"
              type="password"
              autoComplete="current-password"
              value={loginPass}
              onChange={(e) => setLoginPass(e.target.value)}
              className="border-input bg-background focus-visible:ring-ring rounded-md border px-3 py-2 text-sm outline-none focus-visible:ring-2"
            />
          </div>
          <button
            type="button"
            disabled={busy}
            onClick={submitInternalLogin}
            className="bg-primary text-primary-foreground hover:bg-primary/90 rounded-md px-4 py-2 text-sm font-medium disabled:opacity-50"
          >
            Sign in
          </button>
          <ThemeToggle value={theme} onChange={setTheme} className="mt-4" />
        </div>
      </div>
    )
  }

  return (
    <div className="bg-background text-foreground flex min-h-screen">
      <aside className="border-border bg-sidebar text-sidebar-foreground flex w-56 shrink-0 flex-col border-r">
        <div className="border-border border-b px-3 py-3">
          <div className="text-sidebar-primary text-xs font-semibold tracking-wide">GOSSIPPER</div>
          <div className="text-muted-foreground mt-0.5 text-[10px] leading-tight">control & scenarios</div>
        </div>
        <nav className="flex flex-1 flex-col gap-0.5 p-2">
          {NAV.map((item) => (
            <button
              key={item.id}
              type="button"
              onClick={() => setNav(item.id)}
              title={item.hint}
              className={cn(
                'rounded-md px-2.5 py-2 text-left text-sm transition-colors',
                nav === item.id
                  ? 'bg-sidebar-accent text-sidebar-accent-foreground font-medium'
                  : 'text-sidebar-foreground/90 hover:bg-sidebar-accent/50',
              )}
            >
              {item.label}
            </button>
          ))}
        </nav>
        <div className="text-muted-foreground border-border mt-auto flex flex-col gap-2 border-t p-2">
          <ThemeToggle value={theme} onChange={setTheme} />
          <div className="text-[10px] leading-snug">
            API <code className="text-sidebar-foreground/80">/api/v1</code>
          </div>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="border-border bg-card/50 flex shrink-0 flex-wrap items-center justify-between gap-2 border-b px-4 py-2">
          <h1 className="text-sm font-semibold tracking-tight">
            {NAV.find((n) => n.id === nav)?.label ?? nav}
          </h1>
          <div className="text-muted-foreground flex flex-wrap items-center gap-2 text-[11px]">
            <span>
              health:{' '}
              <span className={cn(health === 'ok' ? 'text-success' : 'text-foreground')}>{health ?? '—'}</span>
            </span>
            {liveWs ? (
              <span className="font-mono">
                live: <span className="text-foreground/90">{liveStatus}</span>
              </span>
            ) : null}
            {busy ? <span className="text-warning">request…</span> : null}
          </div>
        </header>

        {(lastError || liveWsError) && (
          <div
            className="border-destructive/40 bg-destructive/10 text-destructive shrink-0 px-4 py-2 text-xs"
            role="alert"
          >
            {lastError}
            {lastError && liveWsError ? ' · ' : null}
            {liveWsError}
          </div>
        )}

        <main className="min-h-0 flex-1 overflow-auto p-4">
          {nav === 'dashboard' && (
            <DashboardView
              stats={stats}
              engineRows={engineRows}
              dynamicIds={dynFromStats}
              liveWs={liveWs}
              liveStatus={liveStatus}
              liveTs={liveFrame?.ts}
              statsJson={statsJson}
            />
          )}
          {nav === 'scenario' && (
            <ScenarioView
              busy={busy}
              scenarioMeta={scenarioMeta}
              scenarioXml={scenarioXml}
              onScenarioXml={setScenarioXml}
              builtin={builtin}
              onLoad={() => void loadScenario()}
              onSaveFile={() =>
                void run(async () => {
                  await putScenario(scenarioXml, { apply: false, bearer: effectiveBearer })
                  await loadScenario()
                })
              }
              onSaveApply={() =>
                void run(async () => {
                  await putScenario(scenarioXml, { apply: true, bearer: effectiveBearer })
                  await loadScenario()
                  await refreshControl()
                })
              }
              onApply={() =>
                void run(async () => {
                  const t = scenarioXml.trim()
                  const hasFile = Boolean(scenarioMeta?.scenario_file?.trim())
                  await postScenarioApply(t === '' ? undefined : scenarioXml, effectiveBearer, {
                    reloadFromDisk: t === '' && hasFile && !builtin,
                  })
                  await refreshControl()
                })
              }
            />
          )}
          {nav === 'load' && (
            <LoadControlView
              busy={busy}
              liveWs={liveWs}
              onLiveWs={setLiveWs}
              pollStats={pollStats}
              onPollStats={setPollStats}
              liveStatus={liveStatus}
              control={control}
              primaryPaused={primaryPaused(control)}
              rateDraft={rateDraft}
              onRateDraft={setRateDraft}
              onTogglePause={(v) =>
                void run(async () => {
                  const next = await postControl({ paused: v }, effectiveBearer)
                  setControl(next)
                  setRateDraft(String(primaryRate(next)))
                })
              }
              onApplyRate={() => {
                const n = Number(rateDraft)
                if (Number.isNaN(n)) return
                void run(async () => {
                  const next = await postControl({ rate: n }, effectiveBearer)
                  setControl(next)
                  setRateDraft(String(primaryRate(next)))
                })
              }}
              onRefreshControl={() => void refreshControl()}
              onRefreshStats={() => void refreshStats()}
              isMulti={isMultiControl(control)}
              multiEngines={multiEngines}
            />
          )}
          {nav === 'servers' && (
            <TransportsView
              section="servers"
              bearer={effectiveBearer}
              busy={busy}
              liveTransports={liveFrame?.transports}
              liveWs={liveWs}
              run={run}
            />
          )}
          {nav === 'sip_clients' && (
            <TransportsView
              section="sip_clients"
              bearer={effectiveBearer}
              busy={busy}
              liveTransports={liveFrame?.transports}
              liveWs={liveWs}
              run={run}
            />
          )}
          {nav === 'clients' && (
            <ClientsView
              busy={busy}
              clientSnippet={clientSnippet}
              onClientSnippet={setClientSnippet}
              clientWantId={clientWantId}
              onClientWantId={setClientWantId}
              dynamicList={dynamicList}
              dynFromStats={dynFromStats}
              onRefreshList={() => void refreshDynamicList()}
              onStartClient={() =>
                void run(async () => {
                  const wid = clientWantId.trim()
                  await postDynamicClient(clientSnippet, {
                    id: wid === '' ? undefined : wid,
                    bearer: effectiveBearer,
                  })
                  await refreshDynamicList()
                  await refreshStats()
                })
              }
              onRemoveClient={(id) =>
                void run(async () => {
                  await deleteDynamicClient(id, effectiveBearer)
                  await refreshDynamicList()
                  await refreshStats()
                })
              }
            />
          )}
          {nav === 'session' && (
            <SessionView
              authKind={authKind}
              busy={busy}
              health={health}
              bearerDraft={bearerDraft}
              onBearerDraft={setBearerDraft}
              onSaveToken={saveToken}
              onRefreshHealth={() => void refreshHealth()}
              onSignOut={() => {
                try {
                  localStorage.removeItem(LS_JWT)
                } catch {
                  /* ignore */
                }
                setJwt('')
              }}
              theme={theme}
              onThemeChange={setTheme}
            />
          )}
        </main>
      </div>
    </div>
  )
}
