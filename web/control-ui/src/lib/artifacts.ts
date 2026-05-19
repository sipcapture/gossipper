import { artifactURL } from '@/api/v2'

export async function fetchArtifactText(jobId: string, kind: string, bearer?: string): Promise<string> {
  const res = await fetch(artifactURL(jobId, kind, bearer), {
    headers: bearer ? { Authorization: 'Bearer ' + bearer } : undefined,
  })
  if (!res.ok) throw new Error(`artifact ${kind}: HTTP ${res.status}`)
  return res.text()
}

export async function fetchArtifactJSON(jobId: string, kind: string, bearer?: string): Promise<unknown> {
  const text = await fetchArtifactText(jobId, kind, bearer)
  return JSON.parse(text) as unknown
}

export async function fetchArtifactBytes(jobId: string, kind: string, bearer?: string): Promise<Uint8Array> {
  const res = await fetch(artifactURL(jobId, kind, bearer), {
    headers: bearer ? { Authorization: 'Bearer ' + bearer } : undefined,
  })
  if (!res.ok) throw new Error(`artifact ${kind}: HTTP ${res.status}`)
  return new Uint8Array(await res.arrayBuffer())
}
