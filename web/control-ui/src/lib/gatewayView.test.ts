import { describe, expect, it } from 'vitest'

import { parseHashRoute } from '@/lib/routing'
import {
  gatewayAOR,
  gatewayCanOriginate,
  gatewayContactPreview,
  gatewayLocalIPLabel,
  gatewaySaveBody,
  gatewaySaveOkText,
  gatewayStatusLabel,
} from '@/lib/gatewayView'

describe('Gateway view helpers', () => {
  it('labels registrar state', () => {
    expect(gatewayStatusLabel('registered')).toBe('Registered')
    expect(gatewayStatusLabel('failed')).toBe('Failed')
    expect(gatewayStatusLabel('registering')).toBe('Registering')
    expect(gatewayStatusLabel('off')).toBe('Off')
    expect(gatewayStatusLabel(undefined)).toBe('Off')
  })

  it('requires domain, addr and AOR user before originate', () => {
    expect(gatewayCanOriginate({ domain: 'pbx.local', addr: '10.0.0.1:5060', username: '1001' })).toBe(
      true,
    )
    expect(gatewayCanOriginate({ domain: 'pbx.local', addr: '10.0.0.1:5060' })).toBe(false)
    expect(gatewayCanOriginate({ username: '1001' })).toBe(false)
    expect(
      gatewayCanOriginate({
        enabled: false,
        domain: 'pbx.local',
        addr: '10.0.0.1:5060',
        username: '1001',
      }),
    ).toBe(false)
  })

  it('formats AOR from username and domain', () => {
    expect(gatewayAOR({ username: '1001', domain: 'pbx.local' })).toBe('1001@pbx.local')
    expect(gatewayAOR({})).toBe('')
  })

  it('builds Contact URI preview', () => {
    expect(
      gatewayContactPreview({
        username: '1001',
        advertised_ip: '192.168.1.20',
        contact_port: 5060,
      }),
    ).toBe('sip:1001@192.168.1.20:5060')
  })

  it('parses #/gateway hash route', () => {
    expect(parseHashRoute('#/gateway').nav).toBe('gateway')
  })

  it('labels discovered local IPs with iface', () => {
    expect(gatewayLocalIPLabel({ ip: '192.168.1.20', iface: 'eth0' })).toBe('eth0 · 192.168.1.20')
    expect(gatewayLocalIPLabel({ ip: '10.0.0.5' })).toBe('10.0.0.5')
  })

  it('formats the save footer with register state', () => {
    const text = gatewaySaveOkText({ status: { state: 'registered', code: 200 } }, new Date('2026-09-20T18:09:00Z'))
    expect(text.startsWith('Saved ')).toBe(true)
    expect(text).toContain('Registered')
    expect(text).toContain('SIP 200')
  })

  it('puts the selected UAS and UAC defaults on the Save body', () => {
    expect(
      gatewaySaveBody(
        { domain: 'pbx.local', addr: '10.0.0.1:5060' },
        { armId: 'fake_ringing_uas', origId: 'one_way', origTo: '100', origCalls: 1 },
      ),
    ).toEqual({
      domain: 'pbx.local',
      addr: '10.0.0.1:5060',
      armed_scenario_id: 'fake_ringing_uas',
      originate_scenario_id: 'one_way',
      originate_to: '100',
      originate_calls: 1,
    })
    expect(gatewaySaveBody({ domain: 'pbx.local' }, { armId: '  ' }).armed_scenario_id).toBe('uas')
  })
})
