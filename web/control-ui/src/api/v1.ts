/** Legacy management API (/api/v1/*) for hybrid gossipper server mode. */

export type V1ControlEngine = { id: string; rate: number; paused: boolean }

export type V1Control = {
  rate?: number
  paused?: boolean
  multi?: boolean
  engines?: V1ControlEngine[]
}

export type V1StatsEngine = {
  id: string
  total_calls?: number
  success_calls?: number
  failed_calls?: number
  active_calls?: number
  success_ratio?: number
  calls_per_second?: number
}

export type V1ClientRow = {
  id: string
  dynamic?: boolean
  transport?: string
  local_port?: number
}

type Opts = { bearer?: string; signal?: AbortSignal }

function headers(bearer?: string): Headers {
  const h = new Headers({ Accept: 'application/json' })
  if (bearer) h.set('Authorization', 'Bearer ' + bearer)
  return h
}

async function v1<T>(path: string, init: RequestInit, bearer?: string): Promise<T> {
  const res = await fetch(path, { ...init, headers: headers(bearer) })
  const text = await res.text()
  if (!res.ok) {
    let msg = res.statusText
    try {
      const o = JSON.parse(text) as { error?: string }
      if (o.error) msg = o.error
    } catch {
      if (text) msg = text.slice(0, 200)
    }
    throw { status: res.status, message: msg }
  }
  if (!text.trim()) return {} as T
  return JSON.parse(text) as T
}

export async function getV1Health(opts: Opts = {}): Promise<{ status?: string }> {
  return v1('/api/v1/health', { method: 'GET' }, opts.bearer)
}

export async function getV1Control(opts: Opts = {}): Promise<V1Control> {
  return v1('/api/v1/control', { method: 'GET' }, opts.bearer)
}

export async function patchV1Control(
  body: { engine_id?: string; id?: string; rate?: number; paused?: boolean; pause?: boolean; resume?: boolean },
  opts: Opts = {},
): Promise<V1Control> {
  return v1(
    '/api/v1/control',
    { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) },
    opts.bearer,
  )
}

export async function getV1Stats(opts: Opts = {}): Promise<{ engines?: V1StatsEngine[] } & V1StatsEngine> {
  return v1('/api/v1/stats', { method: 'GET' }, opts.bearer)
}

export async function listV1Clients(opts: Opts = {}): Promise<{ engines?: V1ClientRow[]; dynamic_client_api?: { can_post?: boolean; can_delete?: boolean } }> {
  return v1('/api/v1/clients', { method: 'GET' }, opts.bearer)
}

export async function addV1Client(body: Record<string, unknown>, id?: string, opts: Opts = {}): Promise<unknown> {
  const q = id ? `?id=${encodeURIComponent(id)}` : ''
  return v1(
    `/api/v1/clients${q}`,
    { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) },
    opts.bearer,
  )
}

export async function deleteV1Client(id: string, opts: Opts = {}): Promise<void> {
  await v1(`/api/v1/clients?id=${encodeURIComponent(id)}`, { method: 'DELETE' }, opts.bearer)
}
