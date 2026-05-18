// Minimal line-oriented diff with no external dependency.
//
// We need a way to render an "old vs new" view for archived scenario XML.
// We deliberately avoid heavy diff libs (react-diff-viewer, diff-match-patch
// etc.) — they pull in 15+ KB and we only need block-level highlighting for
// small SIPp XML scripts (<200 lines). A classic LCS implementation is fast
// enough for that size and ships zero new code into vendor.
//
// Algorithm:
//   1. Split both inputs by '\n'.
//   2. Build an LCS table over the line arrays.
//   3. Walk back through the table emitting equal / del / add hunks.
//
// The output keeps the line order so the consumer can render a unified diff
// directly (one column, "- " / "+ " / "  " prefixes).

export type DiffOp = 'equal' | 'add' | 'del'

export type DiffLine = {
  op: DiffOp
  text: string
  // 1-based line numbers in the corresponding side; -1 when the side has no
  // matching line (e.g. an "add" has oldNo == -1, a "del" has newNo == -1).
  oldNo: number
  newNo: number
}

export function lineDiff(oldText: string, newText: string): DiffLine[] {
  // Empty input → zero lines. `''.split('\n')` returns `['']`, which would
  // otherwise be treated as a single blank line and confuse the diff.
  const splitLines = (s: string): string[] => (s === '' ? [] : s.replace(/\n$/, '').split('\n'))
  const a = splitLines(oldText)
  const b = splitLines(newText)
  const n = a.length
  const m = b.length

  // Edge cases — avoid allocating an (n+1)*(m+1) matrix when one side is
  // empty.
  if (n === 0 && m === 0) return []
  if (n === 0) {
    return b.map((text, i) => ({ op: 'add' as DiffOp, text, oldNo: -1, newNo: i + 1 }))
  }
  if (m === 0) {
    return a.map((text, i) => ({ op: 'del' as DiffOp, text, oldNo: i + 1, newNo: -1 }))
  }

  // LCS length table (O(n*m) memory; fine for scenario-sized inputs).
  const lcs: number[][] = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0))
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      if (a[i] === b[j]) lcs[i][j] = lcs[i + 1][j + 1] + 1
      else lcs[i][j] = Math.max(lcs[i + 1][j], lcs[i][j + 1])
    }
  }

  // Walk the table from (0,0) emitting hunks. We emit "del" before "add" for
  // consistent unified-diff order.
  const out: DiffLine[] = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push({ op: 'equal', text: a[i], oldNo: i + 1, newNo: j + 1 })
      i++
      j++
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      out.push({ op: 'del', text: a[i], oldNo: i + 1, newNo: -1 })
      i++
    } else {
      out.push({ op: 'add', text: b[j], oldNo: -1, newNo: j + 1 })
      j++
    }
  }
  while (i < n) {
    out.push({ op: 'del', text: a[i], oldNo: i + 1, newNo: -1 })
    i++
  }
  while (j < m) {
    out.push({ op: 'add', text: b[j], oldNo: -1, newNo: j + 1 })
    j++
  }
  return out
}

// summariseDiff reports the count of changed lines in either direction.
// Equal lines are not counted.
export function summariseDiff(diff: DiffLine[]): { added: number; removed: number } {
  let added = 0
  let removed = 0
  for (const d of diff) {
    if (d.op === 'add') added++
    else if (d.op === 'del') removed++
  }
  return { added, removed }
}
