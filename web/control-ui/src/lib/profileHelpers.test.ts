import { describe, expect, it } from 'vitest'

import type { ClientProfile, ServerProfile } from '@/api/v2'

import { crossProfilePortConflicts, isProfilePortBlocked, webrtcMissingICE } from '@/lib/profileHelpers'

describe('profileHelpers', () => {
  it('detects cross-profile port conflict', () => {
    const servers: ServerProfile[] = [
      {
        id: 's1',
        name: 's1',
        transports: [{ transport: 'u1', local_ip: '0.0.0.0', local_port: 5060, enabled: true }],
      },
    ]
    const clients: ClientProfile[] = [
      {
        id: 'c1',
        name: 'c1',
        transports: [{ transport: 'u1', local_ip: '0.0.0.0', local_port: 5060, enabled: true }],
      },
    ]
    const { conflicting } = crossProfilePortConflicts(servers, clients)
    expect(conflicting.has('server:s1')).toBe(true)
    expect(conflicting.has('client:c1')).toBe(true)
    expect(isProfilePortBlocked('server', 's1', servers, clients).blocked).toBe(true)
  })

  it('warns when webrtc has no ice servers', () => {
    expect(
      webrtcMissingICE([{ transport: 'webrtc', enabled: true, ice_servers: [] }]),
    ).toBe(true)
    expect(
      webrtcMissingICE([{ transport: 'webrtc', enabled: true, ice_servers: ['stun:stun.example.com'] }]),
    ).toBe(false)
  })
})
