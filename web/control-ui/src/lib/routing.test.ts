import { describe, expect, it } from 'vitest'

import { buildHashRoute, parseHashRoute, scenarioEditorHref, scenarioListHref } from '@/lib/routing'

describe('parseHashRoute scenarios', () => {
  it('lists scenarios at #/scenarios', () => {
    expect(parseHashRoute('#/scenarios')).toEqual({
      nav: 'scenarios',
      reportJobId: undefined,
      scenarioKind: 'list',
    })
  })

  it('opens new-scenario editor', () => {
    expect(parseHashRoute('#/scenarios/new')).toEqual({
      nav: 'scenarios',
      reportJobId: undefined,
      scenarioKind: 'new',
    })
  })

  it('edits a stored scenario by id', () => {
    expect(parseHashRoute('#/scenarios/one_way_uas')).toEqual({
      nav: 'scenarios',
      reportJobId: undefined,
      scenarioKind: 'edit',
      scenarioId: 'one_way_uas',
    })
  })

  it('decodes builtin clone path', () => {
    expect(parseHashRoute('#/scenarios/builtin/one_way')).toEqual({
      nav: 'scenarios',
      reportJobId: undefined,
      scenarioKind: 'builtin',
      scenarioId: 'one_way',
    })
  })

  it('does not treat scenario id as a job id', () => {
    expect(parseHashRoute('#/scenarios/uac_basic').jobId).toBeUndefined()
  })

  it('still parses jobs and load job ids', () => {
    expect(parseHashRoute('#/jobs/abc').jobId).toBe('abc')
    expect(parseHashRoute('#/load/xyz').jobId).toBe('xyz')
  })

  it('opens Live Trace at #/sip', () => {
    expect(parseHashRoute('#/sip')).toEqual({ nav: 'sip', jobId: undefined, reportJobId: undefined })
    expect(buildHashRoute('sip')).toBe('#/sip')
  })

  it('opens Calls list and detail', () => {
    expect(parseHashRoute('#/calls')).toEqual({ nav: 'calls', callId: undefined, reportJobId: undefined })
    expect(parseHashRoute('#/calls/abc%40host')).toEqual({
      nav: 'calls',
      callId: 'abc@host',
      reportJobId: undefined,
    })
    expect(buildHashRoute('calls')).toBe('#/calls')
    expect(buildHashRoute('calls', { callId: 'abc@host' })).toBe('#/calls/abc%40host')
  })
})

describe('scenario href helpers', () => {
  it('builds editor hashes', () => {
    expect(scenarioListHref()).toBe('#/scenarios')
    expect(scenarioEditorHref({ kind: 'new' })).toBe('#/scenarios/new')
    expect(scenarioEditorHref({ kind: 'edit', id: 'a/b' })).toBe('#/scenarios/a%2Fb')
    expect(scenarioEditorHref({ kind: 'builtin', id: 'one_way' })).toBe('#/scenarios/builtin/one_way')
  })
})

describe('buildHashRoute scenarios', () => {
  it('writes list / new / edit / builtin hashes', () => {
    expect(buildHashRoute('scenarios')).toBe('#/scenarios')
    expect(buildHashRoute('scenarios', { scenarioKind: 'new' })).toBe('#/scenarios/new')
    expect(buildHashRoute('scenarios', { scenarioKind: 'edit', scenarioId: 'uac_basic' })).toBe(
      '#/scenarios/uac_basic',
    )
    expect(buildHashRoute('scenarios', { scenarioKind: 'builtin', scenarioId: 'one_way' })).toBe(
      '#/scenarios/builtin/one_way',
    )
  })
})
