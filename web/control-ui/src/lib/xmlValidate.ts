// validateScenarioXML performs a lightweight well-formedness check without DOMParser
// (avoids CodeQL js/xss-through-dom). Authoritative validation is server-side.
export function validateScenarioXML(xml: string): string | null {
  const trimmed = xml.trim()
  if (trimmed === '') return 'XML is empty'
  if (!trimmed.includes('<')) return 'invalid XML'

  const withoutComments = trimmed
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/<!\[CDATA\[[\s\S]*?\]\]>/g, '')

  const tagRe = /<\/?([a-zA-Z_][\w:.-]*)(?:\s[^>/]*)?\s*\/?>/g
  const stack: string[] = []
  let match: RegExpExecArray | null
  while ((match = tagRe.exec(withoutComments)) !== null) {
    const full = match[0]
    const name = match[1]
    if (full.startsWith('<?') || full.startsWith('<!')) {
      continue
    }
    if (full.startsWith('</')) {
      const open = stack.pop()
      if (open !== name) {
        return open
          ? `invalid XML: mismatched </${name}> (expected </${open}>)`
          : `invalid XML: unexpected </${name}>`
      }
      continue
    }
    if (full.endsWith('/>')) {
      continue
    }
    stack.push(name)
  }
  if (stack.length > 0) {
    return `invalid XML: unclosed <${stack[stack.length - 1]}>`
  }
  return null
}
