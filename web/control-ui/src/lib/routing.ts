export type NavId =
  | 'dashboard'
  | 'servers'
  | 'clients'
  | 'gateway'
  | 'sip'
  | 'scenarios'
  | 'jobs'
  | 'reports'
  | 'load'
  | 'media'
  | 'audit'
  | 'users'
  | 'settings'
  | 'about'

const VALID: NavId[] = [
  'dashboard',
  'servers',
  'clients',
  'gateway',
  'sip',
  'scenarios',
  'jobs',
  'reports',
  'load',
  'media',
  'audit',
  'users',
  'settings',
  'about',
]

export type ScenarioRouteKind = 'list' | 'new' | 'edit' | 'builtin'

export type HashRoute = {
  nav: NavId
  jobId?: string
  reportJobId?: string
  scenarioKind?: ScenarioRouteKind
  scenarioId?: string
}

export type SetHashOpts = {
  jobId?: string
  report?: string
  scenarioKind?: ScenarioRouteKind
  scenarioId?: string
}

export function parseHashRoute(hash = window.location.hash): HashRoute {
  const raw = hash.replace(/^#\/?/, '').trim()
  if (!raw) return { nav: 'dashboard' }
  const [path, query] = raw.split('?')
  const parts = path.split('/').filter(Boolean)
  const nav = (parts[0] ?? 'dashboard') as NavId
  const safeNav = VALID.includes(nav) ? nav : 'dashboard'
  const params = new URLSearchParams(query ?? '')
  const reportJobId = params.get('report') ?? undefined
  if (safeNav === 'scenarios') {
    return { nav: 'scenarios', reportJobId, ...parseScenarioPath(parts[1], parts[2]) }
  }
  const jobId =
    safeNav === 'jobs' || safeNav === 'load' ? (parts[1] ?? params.get('job') ?? undefined) : undefined
  return { nav: safeNav, jobId: jobId || undefined, reportJobId: reportJobId || undefined }
}

function parseScenarioPath(a?: string, b?: string): { scenarioKind: ScenarioRouteKind; scenarioId?: string } {
  if (!a) return { scenarioKind: 'list' }
  if (a === 'new') return { scenarioKind: 'new' }
  if (a === 'builtin') {
    const id = decodeURIComponent(b ?? '').trim()
    return id ? { scenarioKind: 'builtin', scenarioId: id } : { scenarioKind: 'list' }
  }
  return { scenarioKind: 'edit', scenarioId: decodeURIComponent(a) }
}

export function scenarioEditorHref(opts: { kind: 'new' | 'edit' | 'builtin'; id?: string }): string {
  if (opts.kind === 'new') return '#/scenarios/new'
  if (opts.kind === 'builtin' && opts.id) return `#/scenarios/builtin/${encodeURIComponent(opts.id)}`
  if (opts.kind === 'edit' && opts.id) return `#/scenarios/${encodeURIComponent(opts.id)}`
  return '#/scenarios'
}

export function scenarioListHref(): string {
  return '#/scenarios'
}

/** Open the scenario editor in a dedicated browser window. Falls back to same-tab hash if popups are blocked. */
export function openScenarioEditorWindow(opts: { kind: 'new' | 'edit' | 'builtin'; id?: string }): boolean {
  const href = scenarioEditorHref(opts)
  const url = `${window.location.pathname}${window.location.search}${href}`
  const name =
    opts.kind === 'new'
      ? 'gossipper-scenario-new'
      : `gossipper-scenario-${opts.kind}-${opts.id ?? 'unknown'}`
  const w = window.open(url, name, 'width=1440,height=900')
  if (w) {
    w.focus()
    return true
  }
  if (window.location.hash !== href) window.location.hash = href
  return false
}

export function buildHashRoute(nav: NavId, opts?: SetHashOpts): string {
  let path = `#/${nav}`
  if (nav === 'scenarios') {
    const kind = opts?.scenarioKind ?? 'list'
    if (kind === 'new') path = '#/scenarios/new'
    else if (kind === 'builtin' && opts?.scenarioId) {
      path = `#/scenarios/builtin/${encodeURIComponent(opts.scenarioId)}`
    } else if (kind === 'edit' && opts?.scenarioId) {
      path = `#/scenarios/${encodeURIComponent(opts.scenarioId)}`
    } else {
      path = '#/scenarios'
    }
  } else if (opts?.jobId && (nav === 'jobs' || nav === 'load')) {
    path += `/${encodeURIComponent(opts.jobId)}`
  }
  const q = new URLSearchParams()
  if (opts?.report) q.set('report', opts.report)
  const qs = q.toString()
  return qs ? `${path}?${qs}` : path
}

export function setHashRoute(nav: NavId, opts?: SetHashOpts) {
  const next = buildHashRoute(nav, opts)
  if (window.location.hash !== next) window.location.hash = next
}
