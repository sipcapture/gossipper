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
  return apiRequest<StatsSummary>('/stats', { method: 'GET', bearer })
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
  return apiRequest<ControlState>('/control', { method: 'GET', bearer })
}

export function postControl(patch: ControlPatch, bearer?: string) {
  return apiRequest<ControlState>('/control', {
    method: 'POST',
    bearer,
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify(patch),
  })
}
