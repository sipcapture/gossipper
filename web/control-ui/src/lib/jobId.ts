// validateJobID checks optional custom job ids against uistore safe-id rules.
export function validateJobID(id: string): string | null {
  const x = id.trim()
  if (x === '') return null
  if (x.length > 64) return 'Job ID must be at most 64 characters'
  if (!/^[a-zA-Z0-9._-]+$/.test(x)) {
    return 'Job ID may only contain letters, digits, and . _ -'
  }
  return null
}
