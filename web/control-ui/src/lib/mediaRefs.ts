// Scan scenario XML for [[media:kind/name]] aliases (gossipper media injection syntax).

const MEDIA_REF_RE = /\[\[media:([a-z]+)\/([^\]]+)\]\]/gi

export type MediaRef = { kind: string; name: string; raw: string }

export function scanMediaRefs(xml: string): MediaRef[] {
  const out: MediaRef[] = []
  const seen = new Set<string>()
  let m: RegExpExecArray | null
  MEDIA_REF_RE.lastIndex = 0
  while ((m = MEDIA_REF_RE.exec(xml)) !== null) {
    const kind = m[1].toLowerCase()
    const name = m[2].trim()
    const key = `${kind}/${name}`
    if (seen.has(key)) continue
    seen.add(key)
    out.push({ kind, name, raw: m[0] })
  }
  return out
}

export function validateMediaRefs(
  xml: string,
  available: { wav: Set<string>; pcap: Set<string> },
): { missing: MediaRef[]; valid: MediaRef[] } {
  const refs = scanMediaRefs(xml)
  const missing: MediaRef[] = []
  const valid: MediaRef[] = []
  for (const r of refs) {
    const set = r.kind === 'wav' ? available.wav : r.kind === 'pcap' ? available.pcap : null
    if (!set || !set.has(r.name)) missing.push(r)
    else valid.push(r)
  }
  return { missing, valid }
}

export function scenariosReferencingMedia(
  scenarios: Array<{ id: string; xml: string }>,
  kind: string,
  name: string,
): string[] {
  const needle = `[[media:${kind}/${name}]]`
  const lower = needle.toLowerCase()
  return scenarios
    .filter((s) => s.xml.toLowerCase().includes(lower))
    .map((s) => s.id)
}
