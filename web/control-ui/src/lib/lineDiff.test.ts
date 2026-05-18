import { describe, expect, it } from 'vitest'

import { lineDiff, sideBySideDiff } from '@/lib/lineDiff'

describe('lineDiff', () => {
  it('handles empty inputs', () => {
    expect(lineDiff('', '')).toEqual([])
    expect(lineDiff('a', '')).toEqual([{ op: 'del', text: 'a', oldNo: 1, newNo: -1 }])
  })
})

describe('sideBySideDiff', () => {
  it('pairs add and delete rows', () => {
    const rows = sideBySideDiff('a\nb', 'a\nc')
    expect(rows.some((r) => r.leftOp === 'del' && r.leftText === 'b')).toBe(true)
    expect(rows.some((r) => r.rightOp === 'add' && r.rightText === 'c')).toBe(true)
  })

  it('matches unified diff line count semantics', () => {
    const oldText = 'one\ntwo'
    const newText = 'one\nthree'
    const unified = lineDiff(oldText, newText)
    const side = sideBySideDiff(oldText, newText)
    expect(side.length).toBe(unified.length)
  })
})
