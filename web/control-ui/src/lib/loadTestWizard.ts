/** Shared client profile id updated by the Load test wizard before each job. */
export const LOAD_WIZARD_PROFILE_ID = '_load_wizard'

export type LoadTestDraft = {
  job_id: string
  director: string
  scenario_id: string
  total_calls: number
  rate: number
  max_concurrent: number
  run_timeout_ms: number
  sip_from: string
  sip_pai: string
  sip_provider: string
  record_wav: boolean
  record_wav_duplex: boolean
  health_enabled: boolean
  health_min_success_ratio: number
  health_max_failed_calls: number
}

export const INVITE_MEDIA_SCENARIOS = [
  { id: 'invite_media', label: 'invite_media — answered media' },
  { id: 'invite_media_early', label: 'invite_media_early — 183 early media' },
  { id: 'invite_media_early_180', label: 'invite_media_early_180 — 180 early media' },
  { id: 'invite_media_savpf', label: 'invite_media_savpf — SAVPF SDP' },
  { id: 'invite_media_scale', label: 'invite_media_scale — high-scale cleartext RTP' },
] as const

export function defaultLoadTestDraft(): LoadTestDraft {
  return {
    job_id: '',
    director: '127.0.0.1:5060',
    scenario_id: 'invite_media',
    total_calls: 10,
    rate: 4,
    max_concurrent: 2,
    run_timeout_ms: 0,
    sip_from: '',
    sip_pai: '',
    sip_provider: '',
    record_wav: false,
    record_wav_duplex: false,
    health_enabled: true,
    health_min_success_ratio: 0.95,
    health_max_failed_calls: 0,
  }
}

/** Parse sipstress-style director host:port (default port 5060). */
export function parseDirector(input: string): { host: string; port: number } | null {
  const raw = input.trim()
  if (!raw) return null
  if (raw.includes('://')) {
    try {
      const u = new URL(raw)
      const port = u.port ? parseInt(u.port, 10) : 5060
      if (!u.hostname || Number.isNaN(port)) return null
      return { host: u.hostname, port }
    } catch {
      return null
    }
  }
  if (raw.startsWith('sip:')) {
    const rest = raw.slice(4)
    const at = rest.indexOf('@')
    const hostPart = at >= 0 ? rest.slice(at + 1) : rest
    const idx = hostPart.lastIndexOf(':')
    if (idx > 0) {
      const host = hostPart.slice(0, idx)
      const port = parseInt(hostPart.slice(idx + 1), 10)
      if (host && !Number.isNaN(port)) return { host, port }
    }
    if (hostPart) return { host: hostPart, port: 5060 }
  }
  const idx = raw.lastIndexOf(':')
  if (idx > 0 && raw.indexOf(':') === idx) {
    const host = raw.slice(0, idx)
    const port = parseInt(raw.slice(idx + 1), 10)
    if (!host || Number.isNaN(port)) return null
    return { host, port }
  }
  return { host: raw, port: 5060 }
}

export function buildLoadTestEngine(draft: LoadTestDraft): Record<string, unknown> {
  const engine: Record<string, unknown> = {
    total_calls: draft.total_calls,
    rate: draft.rate,
    max_concurrent: draft.max_concurrent,
  }
  const dir = parseDirector(draft.director)
  if (dir) {
    engine.remote_host = dir.host
    engine.remote_port = dir.port
  }
  if (draft.run_timeout_ms > 0) {
    engine.global_timeout_ms = draft.run_timeout_ms
  }
  if (draft.sip_from.trim()) engine.sip_from = draft.sip_from.trim()
  if (draft.sip_pai.trim()) engine.sip_pai = draft.sip_pai.trim()
  if (draft.sip_provider.trim()) engine.sip_provider = draft.sip_provider.trim()
  if (draft.health_enabled) {
    if (draft.health_min_success_ratio > 0) {
      engine.health_min_success_ratio = draft.health_min_success_ratio
    }
    engine.health_max_failed_calls = draft.health_max_failed_calls
  }
  return engine
}

export function loadTestCliPreview(draft: LoadTestDraft): string {
  const dir = parseDirector(draft.director)
  const rsa = dir ? `${dir.host}:${dir.port}` : draft.director
  const parts = [
    `gossipper sipp -sn ${draft.scenario_id}`,
    `-rsa ${rsa}`,
    `-m ${draft.total_calls}`,
    `-r ${draft.rate}`,
    `-l ${draft.max_concurrent}`,
  ]
  if (draft.sip_from.trim()) parts.push(`-sip_from '${draft.sip_from.trim()}'`)
  if (draft.sip_pai.trim()) parts.push(`-sip_pai '${draft.sip_pai.trim()}'`)
  if (draft.sip_provider.trim()) parts.push(`-sip_provider ${draft.sip_provider.trim()}`)
  if (draft.record_wav) parts.push('-record_wav_dir ./recordings')
  if (draft.health_enabled && draft.health_min_success_ratio > 0) {
    parts.push(`-health_min_success_ratio ${draft.health_min_success_ratio}`)
  }
  parts.push('-summary_json summary.json -summary_html report.html')
  return parts.join(' \\\n  ')
}
