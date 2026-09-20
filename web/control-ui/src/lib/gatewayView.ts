export type GatewayStatusState = 'off' | 'registering' | 'registered' | 'failed'

export function gatewayStatusLabel(state: string | undefined): string {
  switch ((state ?? '').trim().toLowerCase()) {
    case 'registered':
      return 'Registered'
    case 'registering':
      return 'Registering'
    case 'failed':
      return 'Failed'
    default:
      return 'Off'
  }
}

export function gatewayCanOriginate(cfg: {
  enabled?: boolean
  domain?: string
  addr?: string
  username?: string
  register_user?: string
}): boolean {
  if (cfg.enabled === false) return false
  const user = (cfg.register_user || cfg.username || '').trim()
  return Boolean((cfg.domain ?? '').trim() && (cfg.addr ?? '').trim() && user)
}

export function gatewayAOR(cfg: {
  domain?: string
  username?: string
  register_user?: string
}): string {
  const user = (cfg.register_user || cfg.username || '').trim()
  const domain = (cfg.domain ?? '').trim()
  if (!user && !domain) return ''
  return `${user || 'user'}@${domain || 'domain'}`
}

export function gatewayContactPreview(cfg: {
  username?: string
  register_user?: string
  advertised_ip?: string
  contact_port?: number
}): string {
  const user = (cfg.register_user || cfg.username || '').trim() || 'user'
  const ip = (cfg.advertised_ip ?? '').trim() || 'advertised_ip'
  const port = cfg.contact_port && cfg.contact_port > 0 ? cfg.contact_port : 5060
  return `sip:${user}@${ip}:${port}`
}

export function gatewayLocalIPLabel(addr: { ip: string; iface?: string }): string {
  const iface = (addr.iface ?? '').trim()
  if (iface) {
    return `${iface} · ${addr.ip}`
  }
  return addr.ip
}

export function gatewaySaveOkText(
  snap: { status?: { state?: string; code?: number } },
  at = new Date(),
): string {
  const st = gatewayStatusLabel(snap.status?.state)
  const code = snap.status?.code ? ` · SIP ${snap.status.code}` : ''
  return `Saved ${at.toLocaleTimeString()} · ${st}${code}`
}

export type GatewaySaveLabs = {
  armId: string
  origId?: string
  origTo?: string
  origCalls?: number
}

export function gatewaySaveBody<T extends object>(
  form: T,
  labs: GatewaySaveLabs,
): T & {
  armed_scenario_id: string
  originate_scenario_id: string
  originate_to: string
  originate_calls: number
} {
  const origCalls = labs.origCalls && labs.origCalls > 0 ? labs.origCalls : 0
  return {
    ...form,
    armed_scenario_id: labs.armId.trim() || 'uas',
    originate_scenario_id: (labs.origId ?? '').trim(),
    originate_to: labs.origTo ?? '',
    originate_calls: origCalls,
  }
}
