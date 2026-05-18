// Helpers for detecting local-bind port conflicts across server/client
// profiles, used to surface inline warnings in the UI before a user tries to
// start a profile and the engine fails on a "bind: address already in use".
//
// We deliberately stay UI-only and conservative: the authoritative check
// happens in the engine on bind(2). These helpers exist purely so the
// operator notices an obvious mistake (e.g. two server profiles both bound
// to 0.0.0.0:5060 on UDP) without leaving the configuration screen.

import type { ClientProfile, ServerProfile, TransportSpec } from '@/api/v2'

// transportFamily collapses gossipper's transport tokens ("u1", "un", "t1",
// "tn", "ws1", "wsn", "tls1", "tlsn", "w1", "wn", "webrtc", …) into a small
// set of buckets that share an OS-level port namespace. Anything we don't
// recognise is bucketed by its first character, which is a safe over-match
// (false-positive warning is OK; false-negative is what we want to avoid).
export function transportFamily(t: string): string {
  const x = t.trim().toLowerCase()
  if (x === '') return 'other'
  if (x === 'webrtc') return 'webrtc'
  // Order matters: longer / more-specific prefixes first.
  if (x.startsWith('wss')) return 'tls'
  if (x.startsWith('tls')) return 'tls'
  if (x.startsWith('ws')) return 'ws'
  if (x.startsWith('s')) return 'tls' // SCTP-over-TLS or "s1"/"sn" — over-match is safe
  if (x.startsWith('u')) return 'udp'
  if (x.startsWith('t')) return 'tcp'
  if (x.startsWith('w')) return 'ws'
  return x.charAt(0)
}

// normaliseIP turns missing/blank addresses into "0.0.0.0" so the conflict
// check treats them as wildcard binds (the engine does the same).
function normaliseIP(ip?: string): string {
  const x = (ip ?? '').trim()
  if (x === '' || x === '*') return '0.0.0.0'
  return x
}

// portKey is the unique tuple used to detect bind collisions.
//
// We don't try to be clever about IPv4-vs-IPv6 wildcards: if two listeners
// both target the same family+port and at least one is on 0.0.0.0, that's a
// conflict; matching concrete IPs are conflicts too. Different concrete IPs
// on the same port are allowed.
function listenerKeys(t: TransportSpec): { family: string; port: number; ip: string } | null {
  if (!t.enabled) return null
  const port = Number(t.local_port ?? 0)
  if (!port || port <= 0) return null // ephemeral / unbound — skip
  return {
    family: transportFamily(t.transport),
    port,
    ip: normaliseIP(t.local_ip),
  }
}

export type ProfileLike = { id: string; transports?: TransportSpec[] }

// findPortConflicts inspects every enabled listener across the given
// profiles and returns the set of profile IDs that share a (family, port)
// tuple with another profile. The result is a flat Set keyed by profile.id
// so the UI can quickly mark a row with a "port conflict" badge.
//
// Conflict rule (per family+port bucket):
//   - 2+ listeners on the same concrete IP → conflict
//   - 1+ wildcard listener AND any other listener on the same port → conflict
//   - different concrete IPs only → OK (multi-homed bind)
export function findPortConflicts(profiles: ProfileLike[]): {
  conflicting: Set<string>
  details: Map<string, string[]>
} {
  type Entry = { id: string; ip: string }
  const buckets = new Map<string, Entry[]>()
  for (const p of profiles) {
    for (const t of p.transports ?? []) {
      const k = listenerKeys(t)
      if (!k) continue
      const key = `${k.family}/${k.port}`
      const list = buckets.get(key) ?? []
      list.push({ id: p.id, ip: k.ip })
      buckets.set(key, list)
    }
  }
  const conflicting = new Set<string>()
  const details = new Map<string, string[]>()
  for (const [key, entries] of buckets) {
    if (entries.length < 2) continue
    const hasWildcard = entries.some((e) => e.ip === '0.0.0.0')
    for (let i = 0; i < entries.length; i++) {
      for (let j = i + 1; j < entries.length; j++) {
        const a = entries[i]
        const b = entries[j]
        if (a.id === b.id) continue // same profile — already an obvious config error elsewhere
        const collision = hasWildcard || a.ip === b.ip
        if (!collision) continue
        conflicting.add(a.id)
        conflicting.add(b.id)
        const addDetail = (self: string, other: string) => {
          const list = details.get(self) ?? []
          const note = `${key} ↔ ${other}`
          if (!list.includes(note)) list.push(note)
          details.set(self, list)
        }
        addDetail(a.id, b.id)
        addDetail(b.id, a.id)
      }
    }
  }
  return { conflicting, details }
}

export function serverPortConflicts(profiles: ServerProfile[]) {
  return findPortConflicts(profiles)
}
export function clientPortConflicts(profiles: ClientProfile[]) {
  return findPortConflicts(profiles)
}
