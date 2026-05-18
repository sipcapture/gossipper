import { useEffect, useRef, useState } from 'react'

import { listJobs, liveWSURL, type Job } from '@/api/v2'

type LiveSnap = {
  ts: string
  jobs: Array<{
    id: string
    status: string
    profile_id?: string
    profile_kind?: string
    pid?: number
    started_at?: string
    finished_at?: string
    exit_code?: number
  }>
  counts: Record<string, number>
}

/** mergeLiveJobs overlays live WS status onto a REST-fetched job list. */
export function mergeLiveJobs(base: Job[], liveJobs: LiveSnap['jobs']): Job[] {
  if (liveJobs.length === 0) return base
  const byID = new Map(liveJobs.map((j) => [j.id, j]))
  const seen = new Set<string>()
  const merged = base.map((j) => {
    const live = byID.get(j.id)
    if (!live) return j
    seen.add(j.id)
    return {
      ...j,
      status: (live.status as Job['status']) ?? j.status,
      pid: live.pid ?? j.pid,
      started_at: live.started_at ?? j.started_at,
      finished_at: live.finished_at ?? j.finished_at,
      exit_code: live.exit_code ?? j.exit_code,
    }
  })
  for (const live of liveJobs) {
    if (seen.has(live.id)) continue
    merged.unshift({
      id: live.id,
      status: live.status as Job['status'],
      profile_id: live.profile_id,
      profile_kind: live.profile_kind,
      pid: live.pid,
      started_at: live.started_at,
      finished_at: live.finished_at,
      exit_code: live.exit_code,
      created_at: live.started_at ?? new Date().toISOString(),
    })
  }
  return merged
}

export function useLiveJobs(bearer?: string, intervalMs = 1500) {
  const [liveJobs, setLiveJobs] = useState<LiveSnap['jobs']>([])
  const [connected, setConnected] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    let alive = true
    let retryTimer: ReturnType<typeof setTimeout> | null = null
    const connect = () => {
      if (!alive) return
      try {
        const ws = new WebSocket(liveWSURL(bearer, intervalMs))
        wsRef.current = ws
        ws.onopen = () => {
          if (alive) setConnected(true)
        }
        ws.onmessage = (ev) => {
          try {
            const snap = JSON.parse(ev.data as string) as LiveSnap
            setLiveJobs(snap.jobs ?? [])
          } catch {
            /* ignore */
          }
        }
        ws.onclose = () => {
          wsRef.current = null
          setConnected(false)
          if (!alive) return
          retryTimer = setTimeout(connect, 3000)
        }
        ws.onerror = () => ws.close()
      } catch {
        retryTimer = setTimeout(connect, 5000)
      }
    }
    connect()
    return () => {
      alive = false
      if (retryTimer) clearTimeout(retryTimer)
      wsRef.current?.close()
      wsRef.current = null
    }
  }, [bearer, intervalMs])

  return { liveJobs, connected }
}

export async function refreshJobsWithLive(bearer?: string, limit = 200): Promise<Job[]> {
  const r = await listJobs({ bearer }, limit)
  return r.jobs ?? []
}

export type JobTimelineBucket = { hour: string; succeeded: number; failed: number }

/** computeJobTimeline24h buckets finished jobs into hourly succeeded/failed counts. */
export function computeJobTimeline24h(jobs: Job[], now: Date = new Date()): JobTimelineBucket[] {
  const cutoff = now.getTime() - 24 * 3600 * 1000
  const buckets = new Map<string, { succeeded: number; failed: number }>()
  for (let h = 0; h < 24; h++) {
    const t = new Date(now.getTime() - (23 - h) * 3600 * 1000)
    const key = `${t.getFullYear()}-${t.getMonth()}-${t.getDate()}-${t.getHours()}`
    buckets.set(key, { succeeded: 0, failed: 0 })
  }
  const bucketKey = (d: Date) => `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}-${d.getHours()}`
  for (const j of jobs) {
    const ts = Date.parse(j.finished_at ?? j.created_at)
    if (!Number.isFinite(ts) || ts < cutoff) continue
    if (j.status !== 'succeeded' && j.status !== 'failed') continue
    const d = new Date(ts)
    const key = bucketKey(d)
    const b = buckets.get(key)
    if (!b) continue
    if (j.status === 'succeeded' && (j.exit_code === undefined || j.exit_code === 0)) b.succeeded++
    else b.failed++
  }
  return Array.from(buckets.entries()).map(([hour, v]) => ({
    hour,
    succeeded: v.succeeded,
    failed: v.failed,
  }))
}

export type StatsPoint = { ts: number; value: number; label?: string }

/** parseStatsLines extracts chart points from stats.jsonl worker events. */
export function parseStatsLines(lines: string[]): StatsPoint[] {
  const points: StatsPoint[] = []
  for (const line of lines) {
    if (!line.trim()) continue
    try {
      const ev = JSON.parse(line) as Record<string, unknown>
      const ts = Number(ev.ts ?? ev.timestamp ?? Date.now())
      const value =
        Number(ev.total_calls ?? ev.active_calls ?? ev.calls_per_second ?? ev.interval_calls_per_second) ||
        (ev.kind === 'stats' ? 1 : 0)
      if (Number.isFinite(ts) && Number.isFinite(value)) {
        points.push({ ts, value, label: String(ev.kind ?? '') })
      }
    } catch {
      /* skip malformed */
    }
  }
  return points.slice(-60)
}
