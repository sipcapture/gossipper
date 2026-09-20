import { describe, expect, it } from 'vitest'

import { stripXmlCommentsAndCDATA, validateScenarioXML } from '@/lib/xmlValidate'

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

  it('ignores XML comments when checking tags', () => {
    expect(
      validateScenarioXML('<scenario><!-- note --><recv request="INVITE"/></scenario>'),
    ).toBeNull()
  })
})

describe('stripXmlCommentsAndCDATA', () => {
  it('drops comments and CDATA with a linear scan', () => {
    expect(stripXmlCommentsAndCDATA('a<!-- x -->b<![CDATA[<c>]]>d')).toBe('abd')
  })

  it('does not leave <!-- after a nested comment opener', () => {
    expect(stripXmlCommentsAndCDATA('<!<!-- comment -->')).toBe('<!')
    expect(stripXmlCommentsAndCDATA('<!<!-- comment -->')).not.toMatch(/<!--/)
  })
})
