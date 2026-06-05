// validateScenarioXML performs a lightweight well-formedness check without DOMParser.
// Authoritative validation is server-side in uistore.PutScenario.
export function validateScenarioXML(xml: string): string | null {
  const trimmed = xml.trim()
  if (trimmed === '') return 'XML is empty'
  if (!trimmed.includes('<')) return 'invalid XML'

  const withoutNoise = stripXmlCommentsAndCDATA(trimmed)

  const tagRe = /<\/?([a-zA-Z_][\w:.-]*)(?:\s[^>/]*)?\s*\/?>/g
  const stack: string[] = []
  let match: RegExpExecArray | null
  while ((match = tagRe.exec(withoutNoise)) !== null) {
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

function stripXmlCommentsAndCDATA(input: string): string {
  let out = ''
  let i = 0
  for (; i < input.length; ) {
    if (input.startsWith('<!--', i)) {
      const end = input.indexOf('-->', i + 4)
      if (end === -1) {
        break
      }
      i = end + 3
      continue
    }
    if (input.startsWith('<![CDATA[', i)) {
      const end = input.indexOf(']]>', i + 9)
      if (end === -1) {
        break
      }
      i = end + 3
      continue
    }
    out += input[i]
    i++
  }
  return out
}
