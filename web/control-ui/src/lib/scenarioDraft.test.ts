import { describe, expect, it } from 'vitest'

import {
  peekPendingNewScenario,
  roleMatchesFilter,
  sourceLabel,
  stashPendingNewScenario,
  takePendingNewScenario,
} from '@/lib/scenarioDraft'

describe('pending new scenario draft', () => {
  it('round-trips through an in-memory store', () => {
    const mem: Record<string, string> = {}
    const storage = {
      getItem: (k: string) => (k in mem ? mem[k] : null),
      setItem: (k: string, v: string) => {
        mem[k] = v
      },
      removeItem: (k: string) => {
        delete mem[k]
      },
    }
    stashPendingNewScenario({ id: 'x', name: 'X', xml: '<scenario/>' }, storage)
    expect(peekPendingNewScenario(storage)).toEqual({ id: 'x', name: 'X', xml: '<scenario/>' })
    expect(peekPendingNewScenario(storage)).toEqual({ id: 'x', name: 'X', xml: '<scenario/>' })
    expect(takePendingNewScenario(storage)).toEqual({ id: 'x', name: 'X', xml: '<scenario/>' })
    expect(takePendingNewScenario(storage)).toBeNull()
  })
})

describe('scenario list helpers', () => {
  it('labels sources', () => {
    expect(sourceLabel('store')).toBe('Mine')
    expect(sourceLabel('lab')).toBe('Lab')
    expect(sourceLabel('builtin')).toBe('Built-in')
  })

  it('matches UAS/UAC with server/client filters', () => {
    expect(roleMatchesFilter('uas', 'server')).toBe(true)
    expect(roleMatchesFilter('uac', 'client')).toBe(true)
    expect(roleMatchesFilter('uas', 'client')).toBe(false)
    expect(roleMatchesFilter('', 'either')).toBe(true)
    expect(roleMatchesFilter('server', 'all')).toBe(true)
  })
})
