export type ApiErrorShape = { status: number; message: string }

function apiBase(): string {
  const raw = import.meta.env.VITE_API_BASE?.trim()
  const b = raw && raw.length > 0 ? raw : '/api/v1'
  return b.replace(/\/$/, '')
}

function joinUrl(path: string): string {
  const base = apiBase()
  const p = path.startsWith('/') ? path : `/${path}`
  return `${base}${p}`
}

/** WebSocket URL for live stats/control (token via query when set). */
export function liveWebSocketURL(token?: string): string {
  const path = `${apiBase()}/live`
  const u = new URL(path, window.location.origin)
  u.protocol = u.protocol === 'https:' ? 'wss:' : 'ws:'
  const t = token?.trim()
  if (t) {
    u.searchParams.set('token', t)
  }
  return u.toString()
}

async function readBody(res: Response): Promise<string> {
  try {
    return await res.text()
  } catch {
    return ''
  }
}

async function parseJson<T>(res: Response, bodyText: string): Promise<T> {
  if (!bodyText.trim()) {
    if (!res.ok) {
      const err: ApiErrorShape = { status: res.status, message: res.statusText }
      throw err
    }
    return {} as T
  }
  let data: unknown
  try {
    data = JSON.parse(bodyText)
  } catch {
    const err: ApiErrorShape = {
      status: res.status,
      message: bodyText.slice(0, 400) || 'invalid JSON',
    }
    throw err
  }
  if (!res.ok) {
    const o = data as { error?: string }
    const err: ApiErrorShape = {
      status: res.status,
      message: o.error ?? res.statusText,
    }
    throw err
  }
  return data as T
}

export type RequestOpts = RequestInit & { bearer?: string }

export async function apiRequest<T>(path: string, init: RequestOpts = {}): Promise<T> {
  const { bearer, headers: hdrInit, ...rest } = init
  const headers = new Headers(hdrInit)
  if (!headers.has('Accept')) {
    headers.set('Accept', 'application/json')
  }
  if (bearer) {
    headers.set('Authorization', `Bearer ${bearer}`)
  }
  const res = await fetch(joinUrl(path), { ...rest, headers })
  const text = await readBody(res)
  return parseJson<T>(res, text)
}

export type HealthResponse = { status: string }

export type ScenarioGetResponse = {
  scenario_file: string
  scenario_name: string
  xml: string
  builtin: boolean
}

export type ScenarioPutResponse = {
  written: string
  applied: boolean
}

export type ScenarioApplyResponse = { applied: boolean }

export type ControlState = {
  rate: number
  paused: boolean
}

export type ControlEngineRow = { id: string; rate: number; paused: boolean }

export type ControlGetResponse =
  | ControlState
  | { multi: true; engines: ControlEngineRow[] }

export type StatsEngineRow = { id: string; stats: Record<string, unknown> }

export type StatsGetResponse =
  | StatsSummary
  | { multi: true; engines: StatsEngineRow[]; dynamic_client_ids?: string[] }
  | { multi: false; stats: StatsSummary; dynamic_client_ids?: string[] }

export type TransportsResponse = { listeners: unknown[] }

/** Payload pushed over GET /api/v1/live (WebSocket). */
export type LiveFrame = {
  ts: number
  stats?: StatsGetResponse
  control?: ControlGetResponse
  transports?: TransportsResponse
}

export type ControlPatch = {
  rate?: number
  paused?: boolean
  pause?: boolean
  resume?: boolean
}

/** Stats snapshot mirrors gossipper `internal/stats.Summary` (loosely typed). */
export type StatsSummary = Record<string, unknown>

export function getHealth(bearer?: string) {
  return apiRequest<HealthResponse>('/health', { method: 'GET', bearer })
}

export function getStats(bearer?: string) {
  return apiRequest<StatsGetResponse>('/stats', { method: 'GET', bearer })
}

export function getScenario(bearer?: string) {
  return apiRequest<ScenarioGetResponse>('/scenario', { method: 'GET', bearer })
}

export function putScenario(
  xml: string,
  opts: { apply?: boolean; bearer?: string },
) {
  const q = opts.apply ? '?apply=true' : ''
  return apiRequest<ScenarioPutResponse>(`/scenario${q}`, {
    method: 'PUT',
    bearer: opts.bearer,
    headers: { 'Content-Type': 'application/xml; charset=utf-8' },
    body: xml,
  })
}

export function postScenarioApply(xml: string | undefined, bearer?: string) {
  if (xml === undefined || xml === '') {
    return apiRequest<ScenarioApplyResponse>('/scenario/apply', {
      method: 'POST',
      bearer,
    })
  }
  return apiRequest<ScenarioApplyResponse>('/scenario/apply', {
    method: 'POST',
    bearer,
    headers: { 'Content-Type': 'application/xml; charset=utf-8' },
    body: xml,
  })
}

export function getControl(bearer?: string) {
  return apiRequest<ControlGetResponse>('/control', { method: 'GET', bearer })
}

export function postControl(patch: ControlPatch, bearer?: string) {
  return apiRequest<ControlGetResponse>('/control', {
    method: 'POST',
    bearer,
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify(patch),
  })
}

export async function getDynamicClients(bearer?: string) {
  return apiRequest<{ dynamic: string[] }>('/clients', { method: 'GET', bearer })
}

export async function postDynamicClient(
  jsonBody: string,
  opts?: { id?: string; bearer?: string },
) {
  const id = opts?.id?.trim()
  const q = id ? `?id=${encodeURIComponent(id)}` : ''
  return apiRequest<{ id: string; started: boolean }>(`/clients${q}`, {
    method: 'POST',
    bearer: opts?.bearer,
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: jsonBody,
  })
}

export async function deleteDynamicClient(id: string, bearer?: string) {
  const path = `/clients?id=${encodeURIComponent(id)}`
  return apiRequest<void>(path, { method: 'DELETE', bearer })
}

export type AuthStatusResponse = { auth: 'none' | 'internal' }

export type LoginResponse = {
  token: string
  expires_at: number
  token_type: string
}

async function fetchJSONPublic<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  if (!headers.has('Accept')) {
    headers.set('Accept', 'application/json')
  }
  const res = await fetch(joinUrl(path), { ...init, headers })
  const text = await readBody(res)
  return parseJson<T>(res, text)
}

export function fetchAuthStatus() {
  return fetchJSONPublic<AuthStatusResponse>('/auth/status', { method: 'GET' })
}

export function postAuthLogin(username: string, password: string) {
  return fetchJSONPublic<LoginResponse>('/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify({ username, password }),
  })
}
