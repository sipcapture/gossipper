import { describe, expect, it } from 'vitest'

import { validateScenarioXML } from '@/lib/xmlValidate'

describe('validateScenarioXML', () => {
  it('accepts minimal well-formed scenario', () => {
    expect(
      validateScenarioXML(`<?xml version="1.0"?><scenario name="t"><recv request="INVITE"/></scenario>`),
    ).toBeNull()
  })

  it('rejects empty input', () => {
    expect(validateScenarioXML('   ')).toBe('XML is empty')
  })

  it('reports unclosed or mismatched tags', () => {
    expect(validateScenarioXML('<scenario><recv request="INVITE"></scenario>')).toMatch(
      /unclosed|mismatched/i,
    )
  })

  it('reports mismatched close tags', () => {
    expect(validateScenarioXML('<scenario></recv>')).toMatch(/mismatched/i)
  })
})
