// API client for the new admin console (gossipper ui) endpoints.
// Uses /api/v2/* and consumes the same dev proxy declared in vite.config.ts.

export type ApiErrorV2 = { status: number; message: string }

const BASE = '/api/v2'

function url(path: string): string {
  return `${BASE}${path.startsWith('/') ? path : '/' + path}`
}

async function readBody(res: Response): Promise<string> {
  try {
    return await res.text()
  } catch {
    return ''
  }
}

async function parse<T>(res: Response): Promise<T> {
  const text = await readBody(res)
  if (!text.trim()) {
    if (!res.ok) throw { status: res.status, message: res.statusText } satisfies ApiErrorV2
    return {} as T
  }
  let data: unknown
  try {
    data = JSON.parse(text)
  } catch {
    throw { status: res.status, message: text.slice(0, 400) || 'invalid JSON' } satisfies ApiErrorV2
  }
  if (!res.ok) {
    const o = data as { error?: string }
    throw { status: res.status, message: o.error ?? res.statusText } satisfies ApiErrorV2
  }
  return data as T
}

type Opts = { bearer?: string; signal?: AbortSignal }

async function request<T>(
  method: string,
  path: string,
  body: unknown | undefined,
  opts: Opts = {},
): Promise<T> {
  const headers = new Headers()
  headers.set('Accept', 'application/json')
  if (opts.bearer) headers.set('Authorization', 'Bearer ' + opts.bearer)
  const init: RequestInit = { method, headers, signal: opts.signal }
  if (body !== undefined) {
    headers.set('Content-Type', 'application/json')
    init.body = JSON.stringify(body)
  }
  return parse<T>(await fetch(url(path), init))
}

// --------- types mirroring internal/api/v2 and internal/uistore / supervisor ----------

export type TransportSpec = {
  transport: string
  local_ip?: string
  local_port?: number
  enabled: boolean
  tls_cert_file?: string
  tls_key_file?: string
  ws_path?: string
  ice_servers?: string[]
  ice_username?: string
  ice_credential?: string
  prefers_pcma?: boolean
}

export type ProfileSource = 'built-in' | string

export type ProfileRuntimeStatus =
  | 'built-in'
  | 'running'
  | 'pending'
  | 'idle'
  | 'succeeded'
  | 'failed'
  | 'stopped'

export type ProfileRuntime = {
  status: ProfileRuntimeStatus
  job_id?: string
  pid?: number
  started_at?: string
  finished_at?: string
  exit_code?: number
}

export type ServerProfile = {
  id: string
  name: string
  description?: string
  scenario_ref?: string
  transports?: TransportSpec[]
  max_concurrent?: number
  notes?: string
  source?: ProfileSource
  runtime?: ProfileRuntime
  created_at?: string
  updated_at?: string
}

export type ClientProfile = {
  id: string
  name: string
  description?: string
  scenario_ref?: string
  transports?: TransportSpec[]
  remote_ip?: string
  remote_port?: number
  rate?: number
  max_concurrent?: number
  duration_ms?: number
  notes?: string
  source?: ProfileSource
  runtime?: ProfileRuntime
  created_at?: string
  updated_at?: string
}

export type ScenarioMeta = {
  id: string
  name: string
  description?: string
  role?: string
  tags?: string[]
  created_at?: string
  updated_at?: string
}

export type ScenarioBody = { meta: ScenarioMeta; xml: string }

export type ScenarioHistoryEntry = {
  ts: string
  timestamp: string
  size_bytes: number
  meta?: ScenarioMeta
}

export type MediaKind = 'wav' | 'pcap'
export type MediaAsset = { kind: MediaKind; name: string; size_bytes: number; mod_time: string }

export type JobStatus = 'pending' | 'running' | 'succeeded' | 'failed' | 'stopped'

export type Job = {
  id: string
  profile_id?: string
  profile_kind?: string
  scenario_id?: string
  status: JobStatus
  args_json?: string
  artifacts_dir?: string
  created_at: string
  started_at?: string
  finished_at?: string
  exit_code?: number
  error?: string
  created_by?: number
  pid?: number
}

export type JobArtifact = {
  id: number
  job_id: string
  kind: string
  path: string
  size_bytes: number
  created_at: string
}

// --------- endpoints ----------

export type HealthV2 = { status: string; version?: string; auth: 'none' | 'internal' }
export const getHealthV2 = (opts?: Opts) => request<HealthV2>('GET', '/health', undefined, opts)

export type AuthStatusV2 = { auth: 'none' | 'internal' }
export const getAuthStatusV2 = (opts?: Opts) => request<AuthStatusV2>('GET', '/auth/status', undefined, opts)

export type LoginResponse = { token: string; expires_at: number; token_type: string }
export const loginV2 = (username: string, password: string) =>
  request<LoginResponse>('POST', '/auth/login', { username, password })

export type MeV2 = {
  auth: 'none' | 'internal'
  username: string
  user_id?: number
  expires_at?: number
  role?: string
}
export const getMeV2 = (opts: Opts) => request<MeV2>('GET', '/me', undefined, opts)

export const listServers = (opts: Opts) =>
  request<{ servers: ServerProfile[] }>('GET', '/servers', undefined, opts)
export const createServer = (p: ServerProfile, opts: Opts) =>
  request<ServerProfile>('POST', '/servers', p, opts)
export const updateServer = (id: string, p: ServerProfile, opts: Opts) =>
  request<ServerProfile>('PUT', `/servers/${encodeURIComponent(id)}`, p, opts)
export const deleteServer = (id: string, opts: Opts) =>
  request<void>('DELETE', `/servers/${encodeURIComponent(id)}`, undefined, opts)

export const listClients = (opts: Opts) =>
  request<{ clients: ClientProfile[] }>('GET', '/clients', undefined, opts)
export const createClient = (p: ClientProfile, opts: Opts) =>
  request<ClientProfile>('POST', '/clients', p, opts)
export const updateClient = (id: string, p: ClientProfile, opts: Opts) =>
  request<ClientProfile>('PUT', `/clients/${encodeURIComponent(id)}`, p, opts)
export const deleteClient = (id: string, opts: Opts) =>
  request<void>('DELETE', `/clients/${encodeURIComponent(id)}`, undefined, opts)

export const listScenarios = (opts: Opts) =>
  request<{ scenarios: ScenarioMeta[] }>('GET', '/scenarios', undefined, opts)
export const getScenarioV2 = (id: string, opts: Opts) =>
  request<ScenarioBody>('GET', `/scenarios/${encodeURIComponent(id)}`, undefined, opts)
export const createScenarioV2 = (m: ScenarioMeta, xml: string, opts: Opts) =>
  request<ScenarioBody>('POST', '/scenarios', { ...m, xml }, opts)
export const updateScenarioV2 = (id: string, m: ScenarioMeta, xml: string, opts: Opts) =>
  request<ScenarioBody>('PUT', `/scenarios/${encodeURIComponent(id)}`, { ...m, xml }, opts)
export const deleteScenarioV2 = (id: string, opts: Opts) =>
  request<void>('DELETE', `/scenarios/${encodeURIComponent(id)}`, undefined, opts)
export const listScenarioHistory = (id: string, opts: Opts) =>
  request<{ history: ScenarioHistoryEntry[] }>(
    'GET',
    `/scenarios/${encodeURIComponent(id)}/history`,
    undefined,
    opts,
  )
export const getScenarioHistory = (id: string, ts: string, opts: Opts) =>
  request<ScenarioBody>(
    'GET',
    `/scenarios/${encodeURIComponent(id)}/history/${encodeURIComponent(ts)}`,
    undefined,
    opts,
  )
export const deleteScenarioHistory = (id: string, ts: string, opts: Opts) =>
  request<void>(
    'DELETE',
    `/scenarios/${encodeURIComponent(id)}/history/${encodeURIComponent(ts)}`,
    undefined,
    opts,
  )
export const forkScenarioHistory = (
  id: string,
  ts: string,
  meta: Pick<ScenarioMeta, 'id' | 'name' | 'description' | 'role'>,
  opts: Opts,
) =>
  request<ScenarioBody>(
    'POST',
    `/scenarios/${encodeURIComponent(id)}/history/${encodeURIComponent(ts)}/fork`,
    meta,
    opts,
  )

export const listMedia = (kind: MediaKind, opts: Opts) =>
  request<{ media: MediaAsset[]; kind: MediaKind }>('GET', `/media/${kind}`, undefined, opts)
export const deleteMedia = (kind: MediaKind, name: string, opts: Opts) =>
  request<void>('DELETE', `/media/${kind}/${encodeURIComponent(name)}`, undefined, opts)

// Upload uses raw fetch — multipart not needed, the server stores the body as is.
export async function uploadMedia(kind: MediaKind, file: File, opts: Opts): Promise<MediaAsset> {
  const headers = new Headers()
  if (opts.bearer) headers.set('Authorization', 'Bearer ' + opts.bearer)
  const res = await fetch(url(`/media/${kind}/${encodeURIComponent(file.name)}`), {
    method: 'POST',
    headers,
    body: file,
  })
  return parse<MediaAsset>(res)
}

export const downloadMediaURL = (kind: MediaKind, name: string, bearer?: string): string => {
  const u = new URL(url(`/media/${kind}/${encodeURIComponent(name)}`), window.location.origin)
  if (bearer) u.searchParams.set('token', bearer)
  return u.toString()
}

export const listJobs = (opts: Opts, limit?: number) =>
  request<{ jobs: Job[] }>(
    'GET',
    limit ? `/jobs?limit=${limit}` : '/jobs',
    undefined,
    opts,
  )
export const getJob = (id: string, opts: Opts) =>
  request<{ job: Job; artifacts: JobArtifact[] }>(
    'GET',
    `/jobs/${encodeURIComponent(id)}`,
    undefined,
    opts,
  )
export const startJob = (
  body: {
    id?: string
    profile_id: string
    profile_kind: 'server' | 'client'
    scenario_id?: string
    record_wav?: boolean
    record_wav_duplex?: boolean
    engine?: Record<string, unknown>
  },
  opts: Opts,
) => request<Job>('POST', '/jobs', body, opts)

// Per-profile shortcuts. Body fields mirror startJob() minus profile_id /
// profile_kind which are taken from the URL.
export const startServerProfile = (
  id: string,
  opts: Opts,
  body?: { scenario_id?: string; record_wav?: boolean; record_wav_duplex?: boolean },
) => request<Job>('POST', `/servers/${encodeURIComponent(id)}/start`, body ?? {}, opts)
export const stopServerProfile = (id: string, opts: Opts) =>
  request<Job>('POST', `/servers/${encodeURIComponent(id)}/stop`, undefined, opts)
export const startClientProfile = (
  id: string,
  opts: Opts,
  body?: { scenario_id?: string; record_wav?: boolean; record_wav_duplex?: boolean },
) => request<Job>('POST', `/clients/${encodeURIComponent(id)}/start`, body ?? {}, opts)
export const stopClientProfile = (id: string, opts: Opts) =>
  request<Job>('POST', `/clients/${encodeURIComponent(id)}/stop`, undefined, opts)

// jobEventsURL returns the URL for the /jobs/{id}/events JSONL feed; callers
// stream it with fetch() + ReadableStream.
export const jobEventsURL = (id: string, bearer?: string, opts?: { tail?: number; follow?: boolean }) => {
  const u = new URL(`${BASE}/jobs/${encodeURIComponent(id)}/events`, window.location.origin)
  if (opts?.tail) u.searchParams.set('tail', String(opts.tail))
  if (opts?.follow === false) u.searchParams.set('follow', 'false')
  if (bearer) u.searchParams.set('token', bearer) // some servers require auth on streams
  return u.toString()
}

// liveWSURL builds the WebSocket URL for /api/v2/live. Browsers do not send
// Authorization on upgrades, so we hand the bearer over the ?token= param.
export const liveWSURL = (bearer?: string, intervalMs = 1000) => {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const u = new URL(`${BASE}/live`, `${proto}//${window.location.host}`)
  u.searchParams.set('interval_ms', String(intervalMs))
  if (bearer) u.searchParams.set('token', bearer)
  return u.toString()
}
export const stopJob = (id: string, opts: Opts) =>
  request<Job>('POST', `/jobs/${encodeURIComponent(id)}/stop`, undefined, opts)
export const deleteJobV2 = (id: string, opts: Opts) =>
  request<void>('DELETE', `/jobs/${encodeURIComponent(id)}`, undefined, opts)

export type Recording = { name: string; size_bytes: number; mod_time: string }

export const listRecordings = (jobID: string, opts: Opts) =>
  request<{ recordings: Recording[] }>(
    'GET',
    `/jobs/${encodeURIComponent(jobID)}/recordings`,
    undefined,
    opts,
  )

export const recordingURL = (jobID: string, name: string, bearer?: string): string => {
  const u = new URL(
    url(`/jobs/${encodeURIComponent(jobID)}/recordings/${encodeURIComponent(name)}`),
    window.location.origin,
  )
  if (bearer) u.searchParams.set('token', bearer)
  return u.toString()
}

// ---------- users (Phase 5) ----------
export type User = { id: number; username: string; role: string; created_at: string }
export const listUsers = (opts: Opts) =>
  request<{ users: User[] }>('GET', '/users', undefined, opts)
export const createUser = (body: { username: string; password: string; role?: string }, opts: Opts) =>
  request<User>('POST', '/users', body, opts)
export const updateUser = (id: number, body: { password?: string; role?: string }, opts: Opts) =>
  request<User>('PUT', `/users/${id}`, body, opts)
export const deleteUser = (id: number, opts: Opts) =>
  request<void>('DELETE', `/users/${id}`, undefined, opts)

// ---------- audit log ----------
export type AuditEntry = {
  id: number
  ts: string
  user_id?: number
  username?: string
  action: string
  target: string
  payload_json?: string
}
export const listAudit = (opts: Opts, limit = 100) =>
  request<{ audit: AuditEntry[] }>('GET', `/audit?limit=${limit}`, undefined, opts)

export const rotateJwtSecret = (opts: Opts) =>
  request<{ jwt_secret: string; warning: string }>(
    'POST',
    '/settings/rotate-jwt-secret',
    undefined,
    opts,
  )
