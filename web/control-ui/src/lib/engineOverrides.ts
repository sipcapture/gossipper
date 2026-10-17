export type EngineOverrides = {
  total_calls?: number
  rate?: number
  max_concurrent?: number
  sip_from?: string
  sip_pai?: string
  sip_provider?: string
  remote_host?: string
  remote_port?: number
  global_timeout_ms?: number
  health_min_success_ratio?: number
  health_max_failed_calls?: number
}

export function buildEnginePayload(v: EngineOverrides): Record<string, unknown> | undefined {
  const out: Record<string, unknown> = {}
  for (const [k, val] of Object.entries(v)) {
    if (val !== undefined && val !== '' && !(typeof val === 'number' && Number.isNaN(val))) out[k] = val
  }
  return Object.keys(out).length ? out : undefined
}