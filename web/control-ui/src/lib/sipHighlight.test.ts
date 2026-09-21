import { describe, expect, it } from 'vitest'

import { escapeHtml, highlightSIP } from '@/lib/sipHighlight'

describe('highlightSIP', () => {
  it('escapes HTML in the payload', () => {
    expect(escapeHtml('<script>')).toBe('&lt;script&gt;')
    expect(highlightSIP('X-Foo: <img>')).toContain('&lt;img&gt;')
    expect(highlightSIP('X-Foo: <img>')).not.toContain('<img>')
  })

  it('colors an INVITE request line', () => {
    const html = highlightSIP('INVITE sip:160@10.0.0.1:5060 SIP/2.0\r\nCall-ID: abc@host\r\n')
    expect(html).toContain('text-violet-500')
    expect(html).toContain('INVITE')
    expect(html).toContain('text-emerald-500')
    expect(html).toContain('abc@host')
  })

  it('colors a 200 OK status line', () => {
    const html = highlightSIP('SIP/2.0 200 OK')
    expect(html).toContain('text-emerald-500')
    expect(html).toContain('200')
  })

  it('returns a placeholder for empty payload', () => {
    expect(highlightSIP('')).toBe('(no payload)')
  })
})
