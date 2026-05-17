import { useCallback, useEffect, useRef, useState } from 'react'

import {
  getHealthV2,
  listClients,
  listJobs,
  listMedia,
  listScenarios,
  listServers,
  liveWSURL,
  type HealthV2,
  type Job,
} from '@/api/v2'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

type LiveSnap = {
  ts: string
  jobs: Array<{ id: string; status: string }>
  counts: Record<string, number>
}

export type DashboardV2Props = {
  bearer?: string
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
}

type Counts = {
  servers: number
  clients: number
  scenarios: number
  wav: number
  pcap: number
  jobs: number
  running: number
  succeeded24h: number
  failed24h: number
}

// computeJobOutcomes24h tallies finished jobs from the last 24h. A job is
// "succeeded" when status==="succeeded" with exit_code 0 (or unset);
// "failed" covers status==="failed" or succeeded-with-nonzero-exit. Running /
// pending / stopped jobs and ones older than 24h are skipped.
export function computeJobOutcomes24h(
  jobs: Job[],
  now: Date = new Date(),
): { succeeded: number; failed: number } {
  const cutoff = now.getTime() - 24 * 3600 * 1000
  let succeeded = 0
  let failed = 0
  for (const j of jobs) {
    const ts = Date.parse(j.created_at)
    if (!Number.isFinite(ts) || ts < cutoff) continue
    if (j.status === 'succeeded') {
      if (j.exit_code === undefined || j.exit_code === 0) {
        succeeded++
      } else {
        failed++
      }
    } else if (j.status === 'failed') {
      failed++
    }
  }
  return { succeeded, failed }
}

export function DashboardV2({ bearer, run }: DashboardV2Props) {
  const [health, setHealth] = useState<HealthV2 | null>(null)
  const [counts, setCounts] = useState<Counts | null>(null)
  const [recent, setRecent] = useState<Job[]>([])
  const [live, setLive] = useState<LiveSnap | null>(null)
  const wsRef = useRef<WebSocket | null>(null)

  const refresh = useCallback(async () => {
    const h = await getHealthV2({ bearer })
    setHealth(h)
    const [s, c, sc, w, p, recentJobs, allJobs] = await Promise.all([
      listServers({ bearer }),
      listClients({ bearer }),
      listScenarios({ bearer }),
      listMedia('wav', { bearer }),
      listMedia('pcap', { bearer }),
      listJobs({ bearer }, 5),
      listJobs({ bearer }, 500),
    ])
    const outcomes = computeJobOutcomes24h(allJobs.jobs ?? [])
    setCounts({
      servers: (s.servers ?? []).length,
      clients: (c.clients ?? []).length,
      scenarios: (sc.scenarios ?? []).length,
      wav: (w.media ?? []).length,
      pcap: (p.media ?? []).length,
      jobs: (allJobs.jobs ?? []).length,
      running: (allJobs.jobs ?? []).filter(
        (x) => x.status === 'running' || x.status === 'pending',
      ).length,
      succeeded24h: outcomes.succeeded,
      failed24h: outcomes.failed,
    })
    setRecent(recentJobs.jobs ?? [])
  }, [bearer])

  useEffect(() => {
    void run(() => refresh())
  }, [run, refresh])

  useEffect(() => {
    if (wsRef.current) return
    let alive = true
    let retryTimer: ReturnType<typeof setTimeout> | null = null
    const connect = () => {
      if (!alive) return
      try {
        const url = liveWSURL(bearer, 1500)
        const ws = new WebSocket(url)
        wsRef.current = ws
        ws.onmessage = (ev) => {
          try {
            setLive(JSON.parse(ev.data as string) as LiveSnap)
          } catch {
            // ignore malformed payload
          }
        }
        ws.onclose = () => {
          wsRef.current = null
          if (!alive) return
          retryTimer = setTimeout(connect, 3000)
        }
        ws.onerror = () => {
          ws.close()
        }
      } catch {
        retryTimer = setTimeout(connect, 5000)
      }
    }
    connect()
    return () => {
      alive = false
      if (retryTimer) clearTimeout(retryTimer)
      if (wsRef.current) {
        wsRef.current.close()
        wsRef.current = null
      }
    }
  }, [bearer])

  return (
    <section className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <CountCard label="Server profiles" value={counts?.servers ?? 0} />
        <CountCard label="Client profiles" value={counts?.clients ?? 0} />
        <CountCard label="Scenarios" value={counts?.scenarios ?? 0} />
        <CountCard
          label="Jobs"
          value={counts?.jobs ?? 0}
          hint={counts ? `${counts.running} active` : undefined}
        />
        <CountCard label="WAV files" value={counts?.wav ?? 0} />
        <CountCard label="PCAP files" value={counts?.pcap ?? 0} />
        <CountCard
          label="Succeeded (24h)"
          value={counts?.succeeded24h ?? 0}
          hint="completed jobs with exit 0"
        />
        <CountCard
          label="Failed (24h)"
          value={counts?.failed24h ?? 0}
          hint="status=failed or non-zero exit"
        />
        <CountCard
          label="Auth"
          value={health?.auth ?? '—'}
          hint={health?.version ? `v${health.version.replace(/^Gossipper\s*/i, '')}` : undefined}
        />
        <CountCard label="API" value={health?.status ?? '—'} />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>
            Recent jobs{' '}
            {live ? (
              <span className="text-muted-foreground ml-2 text-[11px]">
                live · {new Date(live.ts).toLocaleTimeString()}
              </span>
            ) : (
              <span className="text-muted-foreground ml-2 text-[11px]">live · connecting…</span>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {recent.length === 0 ? (
            <p className="text-muted-foreground text-xs">No jobs yet. Start one from the Jobs page.</p>
          ) : (
            <ul className="space-y-1 text-xs">
              {recent.map((j) => {
                const liveStatus = live?.jobs.find((x) => x.id === j.id)?.status ?? j.status
                return (
                  <li key={j.id} className="flex justify-between gap-2 font-mono">
                    <span>
                      <span className="text-muted-foreground">[{liveStatus}]</span> {j.id.slice(0, 8)}…
                    </span>
                    <span className="text-muted-foreground">{new Date(j.created_at).toLocaleString()}</span>
                  </li>
                )
              })}
            </ul>
          )}
          {live ? (
            <div className="text-muted-foreground mt-2 text-[11px]">
              Live counts:{' '}
              {Object.entries(live.counts)
                .map(([k, v]) => `${k}=${v}`)
                .join(' · ') || '—'}
            </div>
          ) : null}
        </CardContent>
      </Card>
    </section>
  )
}

function CountCard({ label, value, hint }: { label: string; value: number | string; hint?: string }) {
  return (
    <Card>
      <CardContent className="flex flex-col gap-1 px-4 py-3">
        <div className="text-muted-foreground text-[10px] uppercase tracking-wide">{label}</div>
        <div className="text-foreground text-xl font-semibold">{value}</div>
        {hint ? <div className="text-muted-foreground text-[10px]">{hint}</div> : null}
      </CardContent>
    </Card>
  )
}
