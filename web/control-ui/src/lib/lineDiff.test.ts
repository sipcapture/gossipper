import { describe, expect, it } from 'vitest'

import { lineDiff, summariseDiff } from './lineDiff'

describe('lineDiff', () => {
  it('returns empty for identical inputs', () => {
    const d = lineDiff('a\nb\nc\n', 'a\nb\nc\n')
    expect(d.every((x) => x.op === 'equal')).toBe(true)
    expect(d.map((x) => x.text)).toEqual(['a', 'b', 'c'])
    expect(summariseDiff(d)).toEqual({ added: 0, removed: 0 })
  })

  it('flags pure additions and assigns newNo', () => {
    const d = lineDiff('', 'x\ny\n')
    expect(d.map((x) => x.op)).toEqual(['add', 'add'])
    expect(d.map((x) => x.newNo)).toEqual([1, 2])
    expect(d.every((x) => x.oldNo === -1)).toBe(true)
    expect(summariseDiff(d)).toEqual({ added: 2, removed: 0 })
  })

  it('flags pure deletions and assigns oldNo', () => {
    const d = lineDiff('x\ny\n', '')
    expect(d.map((x) => x.op)).toEqual(['del', 'del'])
    expect(d.map((x) => x.oldNo)).toEqual([1, 2])
    expect(d.every((x) => x.newNo === -1)).toBe(true)
    expect(summariseDiff(d)).toEqual({ added: 0, removed: 2 })
  })

  it('handles a single replaced line as del + add', () => {
    const d = lineDiff('a\nb\nc\n', 'a\nB\nc\n')
    // Order may be either [equal,del,add,equal] or [equal,add,del,equal]
    // depending on tie-breaks; assert by counts and that we end up equal at
    // the start and end.
    expect(d[0]).toMatchObject({ op: 'equal', text: 'a' })
    expect(d[d.length - 1]).toMatchObject({ op: 'equal', text: 'c' })
    const ops = d.map((x) => x.op)
    expect(ops).toContain('del')
    expect(ops).toContain('add')
    expect(summariseDiff(d)).toEqual({ added: 1, removed: 1 })
  })

  it('preserves equal lines between hunks (interleaved edits)', () => {
    const oldXML = ['<scenario name="v1">', '  <recv request="INVITE"/>', '</scenario>'].join('\n')
    const newXML = [
      '<scenario name="v2">',
      '  <recv request="INVITE"/>',
      '  <send>RESP</send>',
      '</scenario>',
    ].join('\n')
    const d = lineDiff(oldXML, newXML)
    expect(summariseDiff(d)).toEqual({ added: 2, removed: 1 })
    // The unchanged middle line must still appear once as equal.
    const equalMiddle = d.filter((x) => x.op === 'equal' && x.text.includes('INVITE'))
    expect(equalMiddle).toHaveLength(1)
  })

  it('strips at most one trailing newline (preserves intentional blanks)', () => {
    const d = lineDiff('a\n\n', 'a\n')
    expect(summariseDiff(d)).toEqual({ added: 0, removed: 1 })
  })
})
