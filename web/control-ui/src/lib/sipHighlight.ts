// Tailwind v4 scans this file for class strings:
// text-sky-500 text-violet-500 text-amber-500 text-emerald-500
// text-destructive text-foreground text-muted-foreground font-semibold font-medium

export function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

/** Syntax-highlight a SIP message. Always HTML-escapes payload text first. */
export function highlightSIP(payload: unknown): string {
  if (payload == null || payload === '') return '(no payload)'
  const text = String(payload)
  const lines = text.replace(/\r\n/g, '\n').replace(/\r/g, '\n').split('\n')
  const out: string[] = []
  for (const lineRaw of lines) {
    const line = lineRaw.trimEnd()
    let escaped = escapeHtml(line)
    if (/^SIP\/2\.0\s+\d{3}/i.test(line)) {
      const respMatch = line.match(/^(SIP\/2\.0)\s+(\d{3})\s*(.*)$/i)
      if (respMatch) {
        const code = parseInt(respMatch[2], 10)
        let cls = 'text-foreground font-semibold'
        if (code >= 200 && code < 300) cls = 'text-emerald-500 font-semibold'
        else if (code >= 300 && code < 400) cls = 'text-amber-500 font-semibold'
        else if (code >= 400) cls = 'text-destructive font-semibold'
        escaped = `<span class="${cls}">${escapeHtml(respMatch[1])} ${escapeHtml(respMatch[2])} ${escapeHtml(respMatch[3])}</span>`
        out.push(escaped)
        continue
      }
    }
    if (/^[A-Z]+\s+sips?:/i.test(line)) {
      const reqMatch = line.match(/^([A-Z]+)\s+(sips?:[^\s]+)\s+(SIP\/2\.0)$/i)
      if (reqMatch) {
        const userMatch = reqMatch[2].match(/sips?:([^@;>]+)/i)
        const user = userMatch ? userMatch[1] : ''
        let uri = escapeHtml(reqMatch[2])
        if (user) {
          uri = uri.replace(escapeHtml(user), `<span class="text-amber-500">${escapeHtml(user)}</span>`)
        }
        escaped = `<span class="text-violet-500 font-semibold">${escapeHtml(reqMatch[1])}</span> ${uri} ${escapeHtml(reqMatch[3])}`
        out.push(escaped)
        continue
      }
    }
    if (/^Call-ID\s*:/i.test(line)) {
      const m = line.match(/^(Call-ID\s*:)\s*(.*)$/i)
      if (m) {
        escaped = `<span class="text-sky-500 font-medium">${escapeHtml(m[1])}</span> <span class="text-emerald-500">${escapeHtml(m[2])}</span>`
      }
      out.push(escaped)
      continue
    }
    if (/^From\s*:/i.test(line)) {
      const fromUserMatch = line.match(/sips?:([^@;>\s]+)/i)
      escaped = escapeHtml(line)
      if (fromUserMatch) {
        escaped = escaped.replace(
          escapeHtml(fromUserMatch[1]),
          `<span class="text-emerald-500">${escapeHtml(fromUserMatch[1])}</span>`,
        )
      }
      escaped = escaped.replace(/^(From\s*:)/i, '<span class="text-sky-500 font-medium">$1</span>')
      out.push(escaped)
      continue
    }
    if (/^To\s*:/i.test(line)) {
      const toUserMatch = line.match(/sips?:([^@;>\s]+)/i)
      escaped = escapeHtml(line)
      if (toUserMatch) {
        escaped = escaped.replace(
          escapeHtml(toUserMatch[1]),
          `<span class="text-emerald-500">${escapeHtml(toUserMatch[1])}</span>`,
        )
      }
      escaped = escaped.replace(/^(To\s*:)/i, '<span class="text-sky-500 font-medium">$1</span>')
      out.push(escaped)
      continue
    }
    if (/^CSeq\s*:/i.test(line)) {
      const m = line.match(/^(CSeq\s*:)\s*(\d+)\s+([A-Z]+)/i)
      if (m) {
        escaped = `<span class="text-sky-500 font-medium">${escapeHtml(m[1])}</span> ${escapeHtml(m[2])} <span class="text-violet-500 font-semibold">${escapeHtml(m[3])}</span>`
      }
      out.push(escaped)
      continue
    }
    const headerMatch = line.match(/^([A-Za-z][A-Za-z0-9-]*\s*:)/)
    if (headerMatch) {
      escaped = escapeHtml(line).replace(
        escapeHtml(headerMatch[1]),
        `<span class="text-sky-500 font-medium">${escapeHtml(headerMatch[1])}</span>`,
      )
    }
    out.push(escaped)
  }
  return out.join('\n')
}
