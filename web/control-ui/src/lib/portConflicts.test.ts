import { describe, expect, it } from 'vitest'

import type { ClientProfile, ServerProfile } from '@/api/v2'
import {
  clientPortConflicts,
  findPortConflicts,
  serverPortConflicts,
  transportFamily,
} from './portConflicts'

const srv = (id: string, transports: ServerProfile['transports']): ServerProfile => ({
  id,
  name: id,
  transports,
})

const cli = (id: string, transports: ClientProfile['transports']): ClientProfile => ({
  id,
  name: id,
  transports,
})

describe('transportFamily', () => {
  it('maps udp/tcp/ws/tls variants', () => {
    expect(transportFamily('u1')).toBe('udp')
    expect(transportFamily('un')).toBe('udp')
    expect(transportFamily('t1')).toBe('tcp')
    expect(transportFamily('tn')).toBe('tcp')
    expect(transportFamily('ws1')).toBe('ws')
    expect(transportFamily('wsn')).toBe('ws')
    expect(transportFamily('w1')).toBe('ws')
    expect(transportFamily('tls1')).toBe('tls')
    expect(transportFamily('s1')).toBe('tls')
    expect(transportFamily('wss1')).toBe('tls')
    expect(transportFamily('webrtc')).toBe('webrtc')
  })

  it('falls back to the first character for unknown tokens', () => {
    // a/k/etc → buckets we've never seen, fine to over-match.
    expect(transportFamily('abc')).toBe('a')
    expect(transportFamily('')).toBe('other')
  })
})

describe('findPortConflicts', () => {
  it('returns empty result for empty input', () => {
    const r = findPortConflicts([])
    expect(r.conflicting.size).toBe(0)
    expect(r.details.size).toBe(0)
  })

  it('flags two server profiles bound to the same wildcard+port', () => {
    const r = serverPortConflicts([
      srv('alpha', [{ transport: 'u1', local_ip: '0.0.0.0', local_port: 5060, enabled: true }]),
      srv('beta', [{ transport: 'u1', local_ip: '0.0.0.0', local_port: 5060, enabled: true }]),
    ])
    expect(Array.from(r.conflicting).sort()).toEqual(['alpha', 'beta'])
    expect(r.details.get('alpha')).toEqual(['udp/5060 ↔ beta'])
    expect(r.details.get('beta')).toEqual(['udp/5060 ↔ alpha'])
  })

  it('flags wildcard vs concrete IP collision on same port', () => {
    const r = serverPortConflicts([
      srv('wild', [{ transport: 'u1', local_ip: '0.0.0.0', local_port: 5060, enabled: true }]),
      srv('lan', [{ transport: 'u1', local_ip: '10.0.0.5', local_port: 5060, enabled: true }]),
    ])
    expect(Array.from(r.conflicting).sort()).toEqual(['lan', 'wild'])
  })

  it('allows different concrete IPs on the same port (multi-homed)', () => {
    const r = serverPortConflicts([
      srv('lan', [{ transport: 'u1', local_ip: '10.0.0.5', local_port: 5060, enabled: true }]),
      srv('mgmt', [{ transport: 'u1', local_ip: '10.0.0.6', local_port: 5060, enabled: true }]),
    ])
    expect(r.conflicting.size).toBe(0)
  })

  it('skips disabled listeners', () => {
    const r = serverPortConflicts([
      srv('a', [{ transport: 'u1', local_ip: '0.0.0.0', local_port: 5060, enabled: true }]),
      srv('b', [{ transport: 'u1', local_ip: '0.0.0.0', local_port: 5060, enabled: false }]),
    ])
    expect(r.conflicting.size).toBe(0)
  })

  it('skips ephemeral (port=0) binds typical for UAC profiles', () => {
    const r = clientPortConflicts([
      cli('a', [{ transport: 'u1', local_ip: '0.0.0.0', local_port: 0, enabled: true }]),
      cli('b', [{ transport: 'u1', local_ip: '0.0.0.0', local_port: 0, enabled: true }]),
    ])
    expect(r.conflicting.size).toBe(0)
  })

  it('does not cross transport families (udp vs tcp on same port is fine)', () => {
    const r = serverPortConflicts([
      srv('udp', [{ transport: 'u1', local_ip: '0.0.0.0', local_port: 5060, enabled: true }]),
      srv('tcp', [{ transport: 't1', local_ip: '0.0.0.0', local_port: 5060, enabled: true }]),
    ])
    expect(r.conflicting.size).toBe(0)
  })

  it('treats blank/star local_ip as wildcard', () => {
    const r = serverPortConflicts([
      srv('blank', [{ transport: 'u1', local_port: 5060, enabled: true }]),
      srv('star', [{ transport: 'u1', local_ip: '*', local_port: 5060, enabled: true }]),
    ])
    expect(Array.from(r.conflicting).sort()).toEqual(['blank', 'star'])
  })

  it('reports cross-profile conflicts with multiple listener buckets', () => {
    const r = serverPortConflicts([
      srv('multi', [
        { transport: 'u1', local_ip: '0.0.0.0', local_port: 5060, enabled: true },
        { transport: 't1', local_ip: '0.0.0.0', local_port: 5060, enabled: true },
      ]),
      srv('udp-only', [
        { transport: 'u1', local_ip: '0.0.0.0', local_port: 5060, enabled: true },
      ]),
      srv('tls-other', [
        { transport: 'tls1', local_ip: '0.0.0.0', local_port: 5061, enabled: true },
      ]),
    ])
    expect(Array.from(r.conflicting).sort()).toEqual(['multi', 'udp-only'])
    expect(r.details.get('multi')).toEqual(['udp/5060 ↔ udp-only'])
    expect(r.details.get('udp-only')).toEqual(['udp/5060 ↔ multi'])
  })

  it('supports tagged cross-kind IDs (server:* vs client:*)', () => {
    const r = findPortConflicts([
      { id: 'server:edge', transports: [{ transport: 'u1', local_ip: '0.0.0.0', local_port: 5060, enabled: true }] },
      { id: 'client:probe', transports: [{ transport: 'u1', local_ip: '0.0.0.0', local_port: 5060, enabled: true }] },
    ])
    expect(Array.from(r.conflicting).sort()).toEqual(['client:probe', 'server:edge'])
    expect(r.details.get('server:edge')?.[0]).toBe('udp/5060 ↔ client:probe')
  })
})
