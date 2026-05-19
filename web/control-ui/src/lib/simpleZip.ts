/** Minimal ZIP (store, no compression) for bundling report artifacts in the browser. */
export async function buildZip(files: { name: string; data: Uint8Array }[]): Promise<Blob> {
  const parts: Uint8Array[] = []
  let offset = 0
  const central: Uint8Array[] = []

  for (const f of files) {
    const nameBytes = new TextEncoder().encode(f.name)
    const { local, centralDir, localLen } = makeLocalFile(nameBytes, f.data, offset)
    parts.push(local)
    central.push(centralDir)
    offset += localLen
  }

  const centralStart = offset
  for (const c of central) {
    parts.push(c)
    offset += c.length
  }

  const end = makeEndRecord(files.length, offset - centralStart, centralStart)
  parts.push(end)

  const total = parts.reduce((n, p) => n + p.length, 0)
  const out = new Uint8Array(total)
  let pos = 0
  for (const p of parts) {
    out.set(p, pos)
    pos += p.length
  }
  return new Blob([out], { type: 'application/zip' })
}

function makeLocalFile(name: Uint8Array, data: Uint8Array, offset: number) {
  const local = new Uint8Array(30 + name.length + data.length)
  const view = new DataView(local.buffer)
  view.setUint32(0, 0x04034b50, true)
  view.setUint16(8, 0, true)
  view.setUint16(26, name.length, true)
  view.setUint32(18, data.length, true)
  view.setUint32(22, crc32(data), true)
  local.set(name, 30)
  local.set(data, 30 + name.length)
  const centralDir = new Uint8Array(46 + name.length)
  const cv = new DataView(centralDir.buffer)
  cv.setUint32(0, 0x02014b50, true)
  cv.setUint16(10, 0, true)
  cv.setUint16(28, name.length, true)
  cv.setUint32(16, crc32(data), true)
  cv.setUint32(20, data.length, true)
  cv.setUint32(24, data.length, true)
  cv.setUint32(42, offset, true)
  centralDir.set(name, 46)
  return { local, centralDir, localLen: local.length }
}

function makeEndRecord(count: number, centralSize: number, centralOffset: number) {
  const end = new Uint8Array(22)
  const view = new DataView(end.buffer)
  view.setUint32(0, 0x06054b50, true)
  view.setUint16(8, count, true)
  view.setUint16(10, count, true)
  view.setUint32(12, centralSize, true)
  view.setUint32(16, centralOffset, true)
  return end
}

function crc32(data: Uint8Array): number {
  let c = ~0
  for (let i = 0; i < data.length; i++) {
    c ^= data[i]
    for (let k = 0; k < 8; k++) c = c & 1 ? (c >>> 1) ^ 0xedb88320 : c >>> 1
  }
  return ~c >>> 0
}
