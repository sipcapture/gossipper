/** Linear SIPp command blocks ↔ scenario XML. Same canvas idea as kefir Lab. */

import { stripXmlCommentsAndCDATA } from '@/lib/xmlValidate'

export const GRID = 16

export type GraphBlockType = 'start' | 'send' | 'recv' | 'pause' | 'nop' | 'label' | 'timewait' | 'raw'

export interface GraphBlock {
  key: string
  type: GraphBlockType
  name?: string
  text?: string
  retrans?: string
  request?: string
  response?: string
  optional?: boolean
  timeout?: string
  milliseconds?: number
  label?: string
  xml?: string
}

export interface BlockMeta {
  type: GraphBlockType
  label: string
  unique?: boolean
  required?: boolean
}

export const BLOCK_TYPES: BlockMeta[] = [
  { type: 'start', label: 'Start', unique: true, required: true },
  { type: 'send', label: 'Send' },
  { type: 'recv', label: 'Recv' },
  { type: 'pause', label: 'Pause' },
  { type: 'nop', label: 'Nop' },
  { type: 'label', label: 'Label' },
  { type: 'timewait', label: 'Timewait' },
]

const MATERIAL_VIEW = '0 -960 960 960'

export const BLOCK_ICONS: Record<GraphBlockType, { viewBox: string; d: string }> = {
  start: {
    viewBox: MATERIAL_VIEW,
    d: 'M795-120q-122 0-242.5-60T336-336q-96-96-156-216.5T120-795q0-19 13-32t32-13h140q14 0 24 9.5t7 25.5l27 126q2 14-.5 25.5T359-638L259-538q56 93 125.5 162T542-250l95-98q10-11 23-15.5t26-1.5l119 26q15 3 25 15t10 26v135q0 19-13 32t-32 13Z',
  },
  send: {
    viewBox: MATERIAL_VIEW,
    d: 'M120-160v-640l760 320-760 320Zm80-120 474-200-474-200v140l240 60-240 60v140Z',
  },
  recv: {
    viewBox: MATERIAL_VIEW,
    d: 'M280-120 80-320l56-56 144 144V200h80v528L504-376l56 56-280 200Zm400 0v-528l144 144 56-56-280-200-280 200 56 56 144-144v528h80Z',
  },
  pause: {
    viewBox: MATERIAL_VIEW,
    d: 'M520-200v-560h240v560H520Zm-320 0v-560h240v560H200Z',
  },
  nop: {
    viewBox: MATERIAL_VIEW,
    d: 'M480-80q-83 0-156-31.5T197-197q-54-54-85.5-127T80-480q0-83 31.5-156T197-763q54-54 127-85.5T480-880q83 0 156 31.5T763-763q54 54 85.5 127T880-480q0 83-31.5 156T763-197q-54 54-127 85.5T480-80Z',
  },
  label: {
    viewBox: MATERIAL_VIEW,
    d: 'M480-80 240-400l80-80 160 160 400-400 80 80L480-80Z',
  },
  timewait: {
    viewBox: MATERIAL_VIEW,
    d: 'M612-292 470-434v-206h60v181l124 124-42 43ZM480-80q-83 0-156-31.5T197-197q-54-54-85.5-127T80-480q0-83 31.5-156T197-763q54-54 127-85.5T480-880q83 0 156 31.5T763-763q54 54 85.5 127T880-480q0 83-31.5 156T763-197q-54 54-127 85.5T480-80Z',
  },
  raw: {
    viewBox: MATERIAL_VIEW,
    d: 'M160-160v-640h240l80 80h320v560H160Zm80-80h480v-400H447l-80-80H240v480Z',
  },
}

const DEFAULT_INVITE = `INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: sipp <sip:sipp@[local_ip]:[local_port]>;tag=[pid]SIPtag[call_number]
To: sut <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Contact: sip:sipp@[local_ip]:[local_port]
Max-Forwards: 70
Content-Length: 0
`

const DEFAULT_200 = `SIP/2.0 200 OK
[last_Via:]
[last_From:]
[last_To:];tag=[call_number]
[last_Call-ID:]
[last_CSeq:]
Content-Length: 0
`

let seq = 0
function nextKey(type: string): string {
  seq += 1
  return `${type}-${seq}`
}

export function snapToGrid(n: number, grid = GRID): number {
  const g = Number(grid) || GRID
  return Math.round(Number(n) / g) * g
}

export function blockMeta(type: GraphBlockType): BlockMeta {
  return BLOCK_TYPES.find((t) => t.type === type) ?? { type, label: type }
}

export function newBlock(type: GraphBlockType): GraphBlock {
  switch (type) {
    case 'start':
      return { key: nextKey('start'), type: 'start', name: 'my_scenario' }
    case 'send':
      return { key: nextKey('send'), type: 'send', text: DEFAULT_INVITE }
    case 'recv':
      return { key: nextKey('recv'), type: 'recv', request: 'INVITE' }
    case 'pause':
      return { key: nextKey('pause'), type: 'pause', milliseconds: 1000 }
    case 'nop':
      return { key: nextKey('nop'), type: 'nop' }
    case 'label':
      return { key: nextKey('label'), type: 'label', label: 'next' }
    case 'timewait':
      return { key: nextKey('timewait'), type: 'timewait' }
    default:
      return { key: nextKey('raw'), type: 'raw', xml: '<nop />' }
  }
}

export function emptyBlocks(): GraphBlock[] {
  const sendOk = { ...newBlock('send'), text: DEFAULT_200 }
  return [newBlock('start'), newBlock('recv'), sendOk]
}

export function canAddBlock(blocks: GraphBlock[], type: GraphBlockType): boolean {
  const meta = blockMeta(type)
  if (!meta.unique) return true
  return !blocks.some((b) => b.type === type)
}

export function addBlock(blocks: GraphBlock[], type: GraphBlockType): GraphBlock[] {
  if (!canAddBlock(blocks, type)) return blocks
  return [...blocks, newBlock(type)]
}

export function removeBlock(blocks: GraphBlock[], index: number): GraphBlock[] {
  const b = blocks[index]
  if (!b || b.type === 'start') return blocks
  return blocks.filter((_, i) => i !== index)
}

export function moveBlock(blocks: GraphBlock[], index: number, dir: -1 | 1): GraphBlock[] {
  const j = index + dir
  if (index <= 0 || j <= 0 || j >= blocks.length) return blocks
  const next = blocks.slice()
  const tmp = next[index]
  next[index] = next[j]
  next[j] = tmp
  return next
}

export function sipFirstLine(text: string): string {
  const line = (text || '')
    .split(/\r?\n/)
    .map((s) => s.trim())
    .find(Boolean)
  if (!line) return 'send'
  return line.length > 28 ? `${line.slice(0, 28)}…` : line
}

export function cardHint(block: GraphBlock): string {
  switch (block.type) {
    case 'start':
      return block.name || 'scenario'
    case 'send':
      return sipFirstLine(block.text || '')
    case 'recv':
      if (block.request) return block.request
      if (block.response) return block.response
      return 'recv'
    case 'pause':
      return `${Number(block.milliseconds) || 0} ms`
    case 'label':
      return block.label || 'label'
    case 'timewait':
      return 'timewait'
    case 'nop':
      return 'nop'
    case 'raw':
      return (block.xml || '').match(/^<\s*([\w:.-]+)/)?.[1] || 'xml'
    default:
      return ''
  }
}

export function edgeLabel(_from: GraphBlock, to: GraphBlock): string {
  if (!to) return ''
  if (to.type === 'start') return 'start'
  if (to.type === 'pause') return `${Number(to.milliseconds) || 0} ms`
  if (to.type === 'recv') return to.request || to.response || 'recv'
  if (to.type === 'send') {
    const line = sipFirstLine(to.text || '')
    const m = line.match(/^(INVITE|ACK|BYE|CANCEL|OPTIONS|REGISTER|PRACK|UPDATE|INFO|REFER|NOTIFY|SIP\/2\.0\s+\d+)/i)
    return m ? m[1].replace(/\s+/g, ' ') : 'send'
  }
  if (to.type === 'label') return to.label || 'label'
  return to.type
}

export function xmlToBlocks(xml: string): GraphBlock[] {
  const trimmed = (xml || '').trim()
  if (!trimmed) return emptyBlocks()
  const open = trimmed.match(/<scenario\b([^>]*)>/i)
  if (!open) return emptyBlocks()
  const attrs = parseAttrs(open[1] || '')
  const start: GraphBlock = { key: nextKey('start'), type: 'start', name: attrs.name || 'scenario' }
  const close = trimmed.lastIndexOf('</scenario>')
  const innerStart = (open.index ?? 0) + open[0].length
  const inner = close === -1 ? trimmed.slice(innerStart) : trimmed.slice(innerStart, close)
  const kids = splitTopLevel(inner)
  const cmds = kids.map(elementToBlock).filter((b): b is GraphBlock => !!b)
  return [start, ...cmds]
}

export function blocksToXML(blocks: GraphBlock[]): string {
  const start = blocks.find((b) => b.type === 'start')
  const name = escapeAttr(start?.name || 'scenario')
  const body = blocks
    .filter((b) => b.type !== 'start')
    .map(blockToXML)
    .join('\n')
  return `<?xml version="1.0" encoding="UTF-8"?>\n<scenario name="${name}">\n${body}\n</scenario>\n`
}

function parseAttrs(s: string): Record<string, string> {
  const out: Record<string, string> = {}
  const re = /([A-Za-z_:][\w:.-]*)\s*=\s*(?:"([^"]*)"|'([^']*)')/g
  let m: RegExpExecArray | null
  while ((m = re.exec(s)) !== null) {
    out[m[1]] = m[2] ?? m[3] ?? ''
  }
  return out
}

function escapeAttr(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;')
}

interface TopEl {
  tag: string
  attrs: Record<string, string>
  inner: string
  raw: string
  selfClosing: boolean
}

function splitTopLevel(src: string): TopEl[] {
  const out: TopEl[] = []
  let i = 0
  while (i < src.length) {
    i = skipNoise(src, i)
    if (i >= src.length) break
    if (src[i] !== '<') {
      i += 1
      continue
    }
    if (src.startsWith('</', i)) break
    const parsed = readElement(src, i)
    if (!parsed) break
    out.push(parsed.el)
    i = parsed.end
  }
  return out
}

function skipNoise(s: string, i: number): number {
  while (i < s.length) {
    if (s.startsWith('<!--', i)) {
      const e = s.indexOf('-->', i + 4)
      i = e === -1 ? s.length : e + 3
      continue
    }
    if (/\s/.test(s[i])) {
      i += 1
      continue
    }
    break
  }
  return i
}

function readElement(s: string, start: number): { el: TopEl; end: number } | null {
  if (s[start] !== '<') return null
  const gt = indexOfTagEnd(s, start)
  if (gt === -1) return null
  const head = s.slice(start + 1, gt)
  const selfClosing = head.trimEnd().endsWith('/')
  const nameMatch = head.match(/^([\w:.-]+)/)
  if (!nameMatch) return null
  const tag = nameMatch[1]
  const attrSrc = selfClosing ? head.slice(tag.length, head.length - 1) : head.slice(tag.length)
  const attrs = parseAttrs(attrSrc)
  if (selfClosing) {
    const raw = s.slice(start, gt + 1)
    return { el: { tag, attrs, inner: '', raw, selfClosing: true }, end: gt + 1 }
  }
  const close = findMatchingClose(s, gt + 1, tag)
  if (close === -1) {
    const raw = s.slice(start)
    return { el: { tag, attrs, inner: s.slice(gt + 1), raw, selfClosing: false }, end: s.length }
  }
  const inner = s.slice(gt + 1, close)
  const end = close + tag.length + 3
  const raw = s.slice(start, end)
  return { el: { tag, attrs, inner, raw, selfClosing: false }, end }
}

function indexOfTagEnd(s: string, start: number): number {
  let i = start
  while (i < s.length) {
    if (s.startsWith('<![CDATA[', i)) {
      const e = s.indexOf(']]>', i + 9)
      i = e === -1 ? s.length : e + 3
      continue
    }
    if (s[i] === '>') return i
    i += 1
  }
  return -1
}

function findMatchingClose(s: string, from: number, tag: string): number {
  let depth = 1
  let i = from
  const openRe = new RegExp(`<${tag}\\b`, 'i')
  const closeTok = `</${tag}>`
  while (i < s.length) {
    if (s.startsWith('<!--', i)) {
      const e = s.indexOf('-->', i + 4)
      i = e === -1 ? s.length : e + 3
      continue
    }
    if (s.startsWith('<![CDATA[', i)) {
      const e = s.indexOf(']]>', i + 9)
      i = e === -1 ? s.length : e + 3
      continue
    }
    if (s.slice(i, i + closeTok.length).toLowerCase() === closeTok.toLowerCase()) {
      depth -= 1
      if (depth === 0) return i
      i += closeTok.length
      continue
    }
    if (openRe.test(s.slice(i, i + tag.length + 8))) {
      const rest = s.slice(i)
      if (rest.toLowerCase().startsWith(`<${tag.toLowerCase()}`)) {
        const gt = indexOfTagEnd(s, i)
        if (gt !== -1 && !s.slice(i + 1, gt).trimEnd().endsWith('/')) {
          depth += 1
        }
        i = gt === -1 ? i + 1 : gt + 1
        continue
      }
    }
    i += 1
  }
  return -1
}

function innerHasChildElements(inner: string): boolean {
  return /<[A-Za-z_]/.test(stripXmlCommentsAndCDATA(inner))
}

function unwrapCdata(inner: string): string {
  const m = inner.match(/<!\[CDATA\[([\s\S]*?)\]\]>/)
  if (m) return trimSipBody(m[1])
  return trimSipBody(inner)
}

function trimSipBody(s: string): string {
  return s.replace(/^\n/, '').replace(/\s+$/, '')
}

function elementToBlock(el: TopEl): GraphBlock | null {
  const tag = el.tag.toLowerCase()
  if (tag === 'pause' && !innerHasChildElements(el.inner)) {
    return {
      key: nextKey('pause'),
      type: 'pause',
      milliseconds: Number(el.attrs.milliseconds) || 0,
    }
  }
  if (tag === 'nop' && !innerHasChildElements(el.inner)) {
    return { key: nextKey('nop'), type: 'nop' }
  }
  if (tag === 'label' && !innerHasChildElements(el.inner)) {
    return { key: nextKey('label'), type: 'label', label: el.attrs.id || el.attrs.name || '' }
  }
  if (tag === 'timewait' && !innerHasChildElements(el.inner)) {
    return { key: nextKey('timewait'), type: 'timewait' }
  }
  if (tag === 'recv' && !innerHasChildElements(el.inner)) {
    return {
      key: nextKey('recv'),
      type: 'recv',
      request: el.attrs.request || '',
      response: el.attrs.response || '',
      optional: el.attrs.optional === 'true',
      timeout: el.attrs.timeout || '',
    }
  }
  if (tag === 'send' && !innerHasChildElements(el.inner)) {
    return {
      key: nextKey('send'),
      type: 'send',
      text: unwrapCdata(el.inner),
      retrans: el.attrs.retrans || '',
    }
  }
  return { key: nextKey('raw'), type: 'raw', xml: el.raw.trim() }
}

function blockToXML(b: GraphBlock): string {
  switch (b.type) {
    case 'pause':
      return `  <pause milliseconds="${Number(b.milliseconds) || 0}"/>`
    case 'nop':
      return '  <nop />'
    case 'label':
      return `  <label id="${escapeAttr(b.label || 'next')}"/>`
    case 'timewait':
      return '  <timewait />'
    case 'recv': {
      const parts = ['  <recv']
      if (b.request) parts.push(` request="${escapeAttr(b.request)}"`)
      if (b.response) parts.push(` response="${escapeAttr(b.response)}"`)
      if (b.optional) parts.push(' optional="true"')
      if (b.timeout) parts.push(` timeout="${escapeAttr(b.timeout)}"`)
      parts.push('/>')
      return parts.join('')
    }
    case 'send': {
      const retrans = b.retrans ? ` retrans="${escapeAttr(b.retrans)}"` : ''
      const body = b.text || ''
      return `  <send${retrans}>\n    <![CDATA[\n${body}\n]]>\n  </send>`
    }
    case 'raw':
      return b.xml ? indentRaw(b.xml) : '  <nop />'
    default:
      return ''
  }
}

function indentRaw(xml: string): string {
  const t = xml.trim()
  if (t.startsWith('  ')) return t
  return t
    .split('\n')
    .map((line) => (line.startsWith('  ') || line === '' ? line : `  ${line}`))
    .join('\n')
}

export function commandTypes(blocks: GraphBlock[]): string[] {
  return blocks.filter((b) => b.type !== 'start').map((b) => (b.type === 'raw' ? 'raw' : b.type))
}
