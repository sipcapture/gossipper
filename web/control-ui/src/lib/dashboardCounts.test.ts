import { describe, expect, it } from 'vitest'

import type { GatewaySnapshot } from '@/api/v2'
import { tallyGateways, tallyScenarioCatalog } from '@/lib/dashboardCounts'

function gw(enabled?: boolean): GatewaySnapshot {
  return {
    config: { domain: 'pbx.example', addr: '10.0.0.1:5060', username: 'u', register: true, enabled },
    status: { state: 'idle' },
    armed_scenario_id: '',
  }
}

describe('dashboard catalog counts', () => {
  it('adds user scenarios to builtin+lab for all', () => {
    expect(tallyScenarioCatalog(3, 40)).toEqual({ ours: 3, all: 43 })
  })

  it('clamps negatives', () => {
    expect(tallyScenarioCatalog(-1, -2)).toEqual({ ours: 0, all: 0 })
  })

  it('counts enabled gateways only when enabled is true', () => {
    expect(tallyGateways([gw(true), gw(false), gw(undefined)])).toEqual({ total: 3, enabled: 1 })
  })
})
