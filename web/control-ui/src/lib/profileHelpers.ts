import type { ClientProfile, ServerProfile } from '@/api/v2'

import { findPortConflicts } from '@/lib/portConflicts'

export function crossProfilePortConflicts(servers: ServerProfile[], clients: ClientProfile[]) {
  const tagged = [
    ...servers.map((s) => ({ id: `server:${s.id}`, transports: s.transports })),
    ...clients.map((c) => ({ id: `client:${c.id}`, transports: c.transports })),
  ]
  return findPortConflicts(tagged)
}

export function isProfilePortBlocked(
  kind: 'server' | 'client',
  id: string,
  servers: ServerProfile[],
  clients: ClientProfile[],
): { blocked: boolean; details: string[] } {
  const { conflicting, details } = crossProfilePortConflicts(servers, clients)
  const key = `${kind}:${id}`
  return { blocked: conflicting.has(key), details: details.get(key) ?? [] }
}

export function profileHasWebRTC(transports?: { transport: string; enabled: boolean }[]): boolean {
  return (transports ?? []).some(
    (t) => t.enabled && t.transport.trim().toLowerCase() === 'webrtc',
  )
}

export function iceServersFromProfile(
  transports?: { transport: string; enabled: boolean; ice_servers?: string[] }[],
): string[] {
  for (const t of transports ?? []) {
    if (!t.enabled || t.transport.trim().toLowerCase() !== 'webrtc') continue
    if (t.ice_servers?.length) return t.ice_servers
  }
  return []
}

export function webrtcMissingICE(
  transports?: {
    transport: string
    enabled: boolean
    ice_servers?: string[]
  }[],
): boolean {
  for (const t of transports ?? []) {
    if (!t.enabled || t.transport.trim().toLowerCase() !== 'webrtc') continue
    const servers = t.ice_servers ?? []
    if (servers.length === 0 || servers.every((s) => !s.trim())) return true
  }
  return false
}
