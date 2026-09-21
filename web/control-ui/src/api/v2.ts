// API client for the new admin console (gossipper ui) endpoints.
// Uses /api/v2/* and consumes the same dev proxy declared in vite.config.ts.

export type ApiErrorV2 = { status: number; message: string }

const BASE = '/api/v2'

let unauthorizedHandler: (() => void) | undefined

/** Called on HTTP 401 from API requests (e.g. expired JWT after secret rotation). */
export function setUnauthorizedHandler(fn: (() => void) | undefined) {
  unauthorizedHandler = fn
}

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
    if (res.status === 401) unauthorizedHandler?.()
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
  ice_auth_secret?: string
  ice_auth_ttl_sec?: number
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

export type ToolMeta = {
  id: string
  title: string
  summary: string
  args_schema?: Record<string, unknown>
  example_args?: Record<string, unknown>
}

// --------- endpoints ----------

export type HealthV2 = { status: string; version?: string; auth: 'none' | 'internal' }
export const getHealthV2 = (opts?: Opts) => request<HealthV2>('GET', '/health', undefined, opts)

export type SettingsV2 = {
  ui_data_dir: string
  scenario_history_keep: number
  disk_usage_bytes?: number
}
export const getSettingsV2 = (opts?: Opts) => request<SettingsV2>('GET', '/settings', undefined, opts)

export type BuiltinScenarioMeta = {
  id: string
  name: string
  role?: string
  description?: string
  source: 'builtin' | 'lab'
}
export const listBuiltinScenarios = (opts: Opts) =>
  request<{ scenarios: BuiltinScenarioMeta[]; source: string }>(
    'GET',
    '/builtin-scenarios',
    undefined,
    opts,
  )
export const getBuiltinScenario = (id: string, opts: Opts) =>
  request<{ meta: BuiltinScenarioMeta; xml: string; source: string }>(
    'GET',
    `/builtin-scenarios/${encodeURIComponent(id)}`,
    undefined,
    opts,
  )

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

export const changeMyPassword = (
  body: { current_password: string; new_password: string },
  opts: Opts,
) => request<{ status: string }>('POST', '/me/password', body, opts)

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

export type ImportPCAPJobBody = {
  job_id: string
  which?: 'uac' | 'uas' | 'both'
  scenario_id: string
  uas_scenario_id?: string
}

export const importScenarioFromPCAPJob = (body: ImportPCAPJobBody, opts: Opts) =>
  request<{ imported: { id: string; role: string; file: string }[]; out_dir: string; job_id: string }>(
    'POST',
    '/scenarios/import-from-pcap-job',
    body,
    opts,
  )

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
    profile_kind: 'server' | 'client' | 'tool'
    scenario_id?: string
    record_wav?: boolean
    record_wav_duplex?: boolean
    engine?: Record<string, unknown>
    tool_args?: Record<string, unknown>
  },
  opts: Opts,
) => request<Job>('POST', '/jobs', body, opts)

export const listTools = (opts: Opts) =>
  request<{ tools: ToolMeta[] }>('GET', '/tools', undefined, opts)

export const runTool = (
  toolId: string,
  body: { id?: string; args: Record<string, unknown> },
  opts: Opts,
) =>
  request<{ job: Job }>(
    'POST',
    `/tools/${encodeURIComponent(toolId)}/run`,
    body,
    opts,
  )

export type LoadTestRunBody = {
  id?: string
  director: string
  scenario_id?: string
  total_calls: number
  rate: number
  max_concurrent: number
  run_timeout_ms?: number
  sip_from?: string
  sip_pai?: string
  sip_provider?: string
  record_wav?: boolean
  record_wav_duplex?: boolean
  health_enabled?: boolean
  health_min_success_ratio?: number
  health_max_failed_calls?: number
}

export type LoadTestSchema = {
  scenarios: string[]
  defaults: Record<string, unknown>
  lifecycle: Record<string, string>
}

export const getLoadTestSchema = (opts: Opts) =>
  request<LoadTestSchema>('GET', '/load-test', undefined, opts)

export const runLoadTest = (body: LoadTestRunBody, opts: Opts) =>
  request<{ job: Job; async: boolean; message: string }>('POST', '/load-test/run', body, opts)

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

export type ReportRow = {
  job_id: string
  profile_kind?: string
  profile_id?: string
  job_status: string
  finished_at?: string
  artifact: JobArtifact
}

export const listReports = (opts: Opts, limit?: number) =>
  request<{ reports: ReportRow[] }>(
    'GET',
    limit ? `/reports?limit=${limit}` : '/reports',
    undefined,
    opts,
  )

export const artifactURL = (jobID: string, kind: string, bearer?: string): string => {
  const u = new URL(
    url(`/jobs/${encodeURIComponent(jobID)}/artifacts/${encodeURIComponent(kind)}`),
    window.location.origin,
  )
  if (bearer) u.searchParams.set('token', bearer)
  return u.toString()
}

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

// ---------- SIP REGISTER gateway ----------
export type GatewayConfig = {
  id?: string
  name?: string
  enabled?: boolean
  domain: string
  addr: string
  transport?: string
  username: string
  password?: string
  register: boolean
  register_user?: string
  advertised_ip?: string
  register_expires?: number
  keepalive_seconds?: number
  contact_port?: number
  armed_scenario_id?: string
  originate_scenario_id?: string
  originate_to?: string
  originate_calls?: number
}

export type GatewayStatus = {
  state: string
  registered?: boolean
  aor?: string
  code?: number
  reason?: string
  error?: string
  trace?: string
}

export type GatewaySnapshot = {
  config: GatewayConfig
  status: GatewayStatus
  armed_scenario_id: string
}

export const getGateway = (opts: Opts) => request<GatewaySnapshot>('GET', '/gateway', undefined, opts)
export const putGateway = (body: GatewayConfig, opts: Opts) =>
  request<GatewaySnapshot>('PUT', '/gateway', body, opts)
export const armGateway = (scenario_id: string, opts: Opts) =>
  request<GatewaySnapshot>('PUT', '/gateway/arm', { scenario_id }, opts)
export const originateGateway = (
  body: { scenario_id: string; to: string; total_calls?: number },
  opts: Opts,
) => request<{ job_id: string }>('POST', '/gateway/originate', body, opts)

export const listGateways = (opts: Opts) =>
  request<{ gateways: GatewaySnapshot[] }>('GET', '/gateways', undefined, opts)
export const createGateway = (body: GatewayConfig, opts: Opts) =>
  request<GatewaySnapshot>('POST', '/gateways', body, opts)
export const getGatewayProfile = (id: string, opts: Opts) =>
  request<GatewaySnapshot>('GET', `/gateways/${encodeURIComponent(id)}`, undefined, opts)
export const updateGateway = (id: string, body: GatewayConfig, opts: Opts) =>
  request<GatewaySnapshot>('PUT', `/gateways/${encodeURIComponent(id)}`, body, opts)
export const deleteGateway = (id: string, opts: Opts) =>
  request<unknown>('DELETE', `/gateways/${encodeURIComponent(id)}`, undefined, opts)
export const armGatewayProfile = (id: string, scenario_id: string, opts: Opts) =>
  request<GatewaySnapshot>('PUT', `/gateways/${encodeURIComponent(id)}/arm`, { scenario_id }, opts)
export const originateGatewayProfile = (
  id: string,
  body: { scenario_id: string; to: string; total_calls?: number },
  opts: Opts,
) => request<{ job_id: string }>('POST', `/gateways/${encodeURIComponent(id)}/originate`, body, opts)

export type GatewayLocalAddr = { ip: string; iface?: string }

export type GatewayIPHints = {
  local: GatewayLocalAddr[]
  external?: string
  external_error?: string
}

export const getGatewayIPs = (opts: Opts) => request<GatewayIPHints>('GET', '/gateway/ips', undefined, opts)

export interface SIPTraceCapture {
  sip: boolean
  app: boolean
  rtp?: boolean
  level?: string
}

export interface SIPTraceMessage {
  seq: number
  ts: string
  kind?: 'sip' | 'app' | 'rtp' | string
  dir: 'send' | 'recv' | 'app' | string
  peer?: string
  scenario?: string
  summary: string
  method?: string
  status?: number
  call_id?: string
  level?: string
  raw: string
}

export interface SIPTraceResponse {
  messages: SIPTraceMessage[]
  next: number
  cleared?: boolean
  capture?: SIPTraceCapture
}

export const getSIPTrace = (opts: Opts & { since?: number }) => {
  const q = new URLSearchParams()
  q.set('since', String(opts.since ?? 0))
  return request<SIPTraceResponse>('GET', `/sip/trace?${q.toString()}`, undefined, opts)
}

export const clearSIPTrace = (opts: Opts) =>
  request<SIPTraceResponse>('POST', '/sip/trace/clear', {}, opts)

export const getSIPTraceCapture = (opts: Opts) =>
  request<SIPTraceCapture>('GET', '/sip/trace/capture', undefined, opts)

export const putSIPTraceCapture = (body: SIPTraceCapture, opts: Opts) =>
  request<SIPTraceCapture>('PUT', '/sip/trace/capture', body, opts)

export async function downloadSIPTrace(
  format: 'text' | 'pcap' | 'zip',
  opts: Opts & { scenario?: string } = {},
): Promise<void> {
  const q = new URLSearchParams()
  q.set('format', format)
  if (opts.scenario) q.set('scenario', opts.scenario)
  const headers = new Headers()
  if (opts.bearer) headers.set('Authorization', 'Bearer ' + opts.bearer)
  const res = await fetch(url(`/sip/trace/export?${q.toString()}`), {
    headers,
    signal: opts.signal,
  })
  await throwIfNotOk(res)
  const fallback = `gossipper-trace.${format === 'zip' ? 'zip' : format === 'pcap' ? 'pcap' : 'txt'}`
  await saveBlobResponse(res, fallback)
}

export async function downloadCallDump(id: string, opts: Opts = {}): Promise<void> {
  const headers = new Headers()
  if (opts.bearer) headers.set('Authorization', 'Bearer ' + opts.bearer)
  const res = await fetch(url(`/calls/${encodeURIComponent(id)}?format=zip`), {
    headers,
    signal: opts.signal,
  })
  await throwIfNotOk(res)
  await saveBlobResponse(res, 'gossipper-call.zip')
}

async function throwIfNotOk(res: Response): Promise<void> {
  if (res.status === 401) unauthorizedHandler?.()
  if (res.ok) return
  const text = await readBody(res)
  let message = res.statusText
  try {
    const parsed = JSON.parse(text) as { error?: string }
    if (parsed.error) message = parsed.error
  } catch {
    if (text.trim()) message = text.slice(0, 400)
  }
  throw { status: res.status, message } satisfies ApiErrorV2
}

async function saveBlobResponse(res: Response, fallbackName: string): Promise<void> {
  const blob = await res.blob()
  const disp = res.headers.get('Content-Disposition')
  const star = /filename\*=(?:UTF-8''|)([^;]+)/i.exec(disp ?? '')
  const plain = /filename="?([^";]+)"?/i.exec(disp ?? '')
  const name =
    (star ? decodeURIComponent(star[1].trim().replace(/^"+|"+$/g, '')) : undefined) ??
    plain?.[1]?.trim() ??
    fallbackName
  const href = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = href
  a.download = name
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(href)
}

// sipTraceWSURL builds the WebSocket URL for /api/v2/sip/trace/ws. Browsers do
// not send Authorization on upgrades, so the bearer goes on ?token=.
export const sipTraceWSURL = (bearer?: string, since = 0) => {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const u = new URL(`${BASE}/sip/trace/ws`, `${proto}//${window.location.host}`)
  if (since) u.searchParams.set('since', String(since))
  if (bearer) u.searchParams.set('token', bearer)
  return u.toString()
}

export interface CallSummary {
  call_id: string
  scenario?: string
  from?: string
  to?: string
  peer?: string
  direction?: string
  state: string
  result?: string
  error?: string
  started_at: string
  ended_at?: string
  duration_ms: number
  sip_count: number
  sip_send: number
  sip_recv: number
  debug_count: number
  rtp_count: number
  rtp_send: number
  rtp_recv: number
  rtp_bytes_send: number
  rtp_bytes_recv: number
  rtp_dest?: string
  rtp_src?: string
  payload_type?: number
  codec?: string
}

export interface CallRTPSample {
  ts: string
  dir: string
  src: string
  dst: string
  pt: number
  seq: number
  size: number
}

export interface CallDetail extends CallSummary {
  sip: SIPTraceMessage[]
  debug: SIPTraceMessage[]
  rtp: CallRTPSample[]
}

export const listCalls = (opts: Opts) => request<{ calls: CallSummary[] }>('GET', '/calls', undefined, opts)

export const getCall = (id: string, opts: Opts) =>
  request<CallDetail>('GET', `/calls/${encodeURIComponent(id)}`, undefined, opts)

export const clearCalls = (opts: Opts) => request<{ calls: CallSummary[] }>('POST', '/calls/clear', {}, opts)
