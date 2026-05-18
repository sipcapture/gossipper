import { useCallback, useEffect, useLayoutEffect, useMemo, useState } from 'react'

import { getAuthStatusV2, loginV2 } from '@/api/v2'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ThemeToggle, type ThemeMode } from '@/components/ThemeToggle'
import { cn } from '@/lib/utils'
import { ToastProvider, useToast } from '@/lib/toast'
import { AboutV2 } from '@/views/v2/AboutV2'
import { AuditV2 } from '@/views/v2/AuditV2'
import { ClientsV2 } from '@/views/v2/ClientsV2'
import { DashboardV2 } from '@/views/v2/DashboardV2'
import { JobsV2 } from '@/views/v2/JobsV2'
import { MediaV2 } from '@/views/v2/MediaV2'
import { ScenariosV2 } from '@/views/v2/ScenariosV2'
import { ServersV2 } from '@/views/v2/ServersV2'
import { SettingsV2 } from '@/views/v2/SettingsV2'
import { UsersV2 } from '@/views/v2/UsersV2'

const LS_JWT = 'gossipper_v2_jwt'
const LS_THEME = 'gossipper_control_theme'

type NavId =
  | 'dashboard'
  | 'servers'
  | 'clients'
  | 'scenarios'
  | 'jobs'
  | 'media'
  | 'audit'
  | 'users'
  | 'settings'
  | 'about'

const NAV: { id: NavId; label: string; hint: string }[] = [
  { id: 'dashboard', label: 'Dashboard', hint: 'overview and recent jobs' },
  { id: 'servers', label: 'Servers', hint: 'UAS profiles' },
  { id: 'clients', label: 'Clients', hint: 'UAC profiles' },
  { id: 'scenarios', label: 'Scenarios', hint: 'XML scenarios + sidecar meta' },
  { id: 'jobs', label: 'Jobs', hint: 'worker runs (start / stop / inspect)' },
  { id: 'media', label: 'Media', hint: 'WAV and PCAP upload library' },
  { id: 'audit', label: 'Audit', hint: 'mutating API actions log' },
  { id: 'users', label: 'Users', hint: 'admin users' },
  { id: 'settings', label: 'Settings', hint: 'system info, theme, sign out' },
  { id: 'about', label: 'About', hint: 'version, capabilities, links' },
]

function readStored(key: string): string {
  try {
    return localStorage.getItem(key) ?? ''
  } catch {
    return ''
  }
}

function readTheme(): ThemeMode {
  const t = readStored(LS_THEME).trim().toLowerCase()
  return t === 'light' ? 'light' : 'dark'
}

export type AdminAppProps = Record<string, never>

export function AdminApp(_: AdminAppProps = {}) {
  return (
    <ToastProvider>
      <AdminAppInner />
    </ToastProvider>
  )
}

function AdminAppInner() {
  const { toast } = useToast()
  const [nav, setNav] = useState<NavId>('dashboard')
  const [theme, setTheme] = useState<ThemeMode>(readTheme)
  const [authKind, setAuthKind] = useState<'none' | 'internal' | null>(null)
  const [jwt, setJwt] = useState(() => readStored(LS_JWT))
  const [loginUser, setLoginUser] = useState('')
  const [loginPass, setLoginPass] = useState('')
  const [busy, setBusy] = useState(false)
  const [lastError, setLastError] = useState<string | null>(null)

  useLayoutEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark')
    try {
      localStorage.setItem(LS_THEME, theme)
    } catch {
      /* ignore */
    }
  }, [theme])

  useEffect(() => {
    void (async () => {
      try {
        const s = await getAuthStatusV2()
        setAuthKind(s.auth === 'internal' ? 'internal' : 'none')
      } catch {
        setAuthKind('none')
      }
    })()
  }, [])

  const bearer = authKind === 'internal' ? (jwt.trim() || undefined) : undefined

  const run = useCallback(async <T,>(fn: () => Promise<T>): Promise<T | undefined> => {
    setBusy(true)
    setLastError(null)
    try {
      return await fn()
    } catch (e) {
      const msg =
        e && typeof e === 'object' && 'message' in e
          ? `${(e as { status?: number }).status ?? ''}: ${(e as { message: string }).message}`
          : e instanceof Error
          ? e.message
          : String(e)
      setLastError(msg)
      toast(msg, 'error')
      return undefined
    } finally {
      setBusy(false)
    }
  }, [])

  const onLogin = () => {
    void run(async () => {
      const r = await loginV2(loginUser, loginPass)
      try {
        localStorage.setItem(LS_JWT, r.token)
      } catch {
        /* ignore */
      }
      setJwt(r.token)
      setLoginPass('')
    })
  }

  const onSignOut = () => {
    try {
      localStorage.removeItem(LS_JWT)
    } catch {
      /* ignore */
    }
    setJwt('')
  }

  const currentLabel = useMemo(() => NAV.find((n) => n.id === nav)?.label ?? nav, [nav])

  if (authKind === null) {
    return (
      <div className="bg-background text-foreground flex min-h-screen items-center justify-center">
        <p className="text-muted-foreground text-sm">Checking authentication mode…</p>
      </div>
    )
  }

  if (authKind === 'internal' && !jwt.trim()) {
    return (
      <div className="bg-background text-foreground min-h-screen p-4">
        <div className="border-border bg-card mx-auto mt-16 w-full max-w-md rounded-lg border p-6">
          <h1 className="mb-1 text-base font-semibold tracking-tight">Sign in</h1>
          <p className="text-muted-foreground mb-4 text-xs leading-relaxed">
            Internal auth (<code>auth.type: internal</code>) is enabled. Create users with{' '}
            <code>gossipper auth user-add -config …</code>.
          </p>
          {lastError ? (
            <div className="border-destructive/50 bg-destructive/10 text-destructive mb-3 rounded-md border px-3 py-2 text-xs">
              {lastError}
            </div>
          ) : null}
          <div className="mb-3">
            <Label htmlFor="lu" className="text-xs">
              Username
            </Label>
            <Input id="lu" value={loginUser} onChange={(e) => setLoginUser(e.target.value)} className="mt-1" />
          </div>
          <div className="mb-4">
            <Label htmlFor="lp" className="text-xs">
              Password
            </Label>
            <Input
              id="lp"
              type="password"
              value={loginPass}
              onChange={(e) => setLoginPass(e.target.value)}
              className="mt-1"
            />
          </div>
          <Button type="button" onClick={onLogin} disabled={busy} size="sm" className="w-full">
            Sign in
          </Button>
          <ThemeToggle value={theme} onChange={setTheme} className="mt-4" />
        </div>
      </div>
    )
  }

  return (
    <div className="bg-background text-foreground flex min-h-screen">
      <aside className="border-border bg-sidebar text-sidebar-foreground flex w-56 shrink-0 flex-col border-r">
        <div className="border-border border-b px-3 py-3">
          <div className="text-sidebar-primary text-xs font-semibold tracking-wide">GOSSIPPER · UI</div>
          <div className="text-muted-foreground mt-0.5 text-[10px] leading-tight">admin console (api v2)</div>
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
          <div className="text-[10px] leading-snug">
            API <code className="text-sidebar-foreground/80">/api/v2</code>
          </div>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="border-border bg-card/50 flex shrink-0 flex-wrap items-center justify-between gap-2 border-b px-4 py-2">
          <h1 className="text-sm font-semibold tracking-tight">{currentLabel}</h1>
          <div className="text-muted-foreground flex flex-wrap items-center gap-2 text-[11px]">
            {busy ? <span className="text-warning">request…</span> : null}
            <span>
              auth: <span className="text-foreground">{authKind}</span>
            </span>
          </div>
        </header>

        {lastError ? (
          <div
            className="border-destructive/40 bg-destructive/10 text-destructive shrink-0 px-4 py-2 text-xs"
            role="alert"
          >
            {lastError}
          </div>
        ) : null}

        <main className="min-h-0 flex-1 overflow-auto p-4">
          {nav === 'dashboard' && <DashboardV2 bearer={bearer} run={run} />}
          {nav === 'servers' && <ServersV2 bearer={bearer} busy={busy} run={run} errorText={lastError} />}
          {nav === 'clients' && <ClientsV2 bearer={bearer} busy={busy} run={run} errorText={lastError} />}
          {nav === 'scenarios' && <ScenariosV2 bearer={bearer} busy={busy} run={run} errorText={lastError} />}
          {nav === 'jobs' && <JobsV2 bearer={bearer} busy={busy} run={run} errorText={lastError} />}
          {nav === 'media' && <MediaV2 bearer={bearer} busy={busy} run={run} errorText={lastError} />}
          {nav === 'audit' && <AuditV2 bearer={bearer} />}
          {nav === 'users' && <UsersV2 bearer={bearer} busy={busy} run={run} errorText={lastError} />}
          {nav === 'settings' && (
            <SettingsV2
              bearer={bearer}
              theme={theme}
              onThemeChange={setTheme}
              onSignOut={authKind === 'internal' ? onSignOut : undefined}
              authKind={authKind}
            />
          )}
          {nav === 'about' && <AboutV2 bearer={bearer} />}
        </main>
      </div>
    </div>
  )
}
