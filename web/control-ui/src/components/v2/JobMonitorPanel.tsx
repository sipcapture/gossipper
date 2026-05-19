import { artifactURL, getJob, jobEventsURL, listRecordings, stopJob, type Job, type JobArtifact, type Recording } from '@/api/v2'
import { JobStatsChart } from '@/components/v2/JobStatsChart'
import { SummaryKPICards } from '@/components/v2/SummaryKPI'
import { Button } from '@/components/ui/button'
import { fetchArtifactJSON } from '@/lib/artifacts'
import { parseStatsLines } from '@/lib/jobsLive'
import { parseSummaryJSON } from '@/lib/summaryParse'
import { useCallback, useEffect, useMemo, useState } from 'react'

export type JobMonitorPanelProps = {
  jobId: string
  bearer?: string
  onStop?: () => void
  onFinished?: (job: Job) => void
  onOpenReports?: (jobId: string) => void
  compact?: boolean
}

export function JobMonitorPanel({
  jobId,
  bearer,
  onStop,
  onFinished,
  onOpenReports,
  compact,
}: JobMonitorPanelProps) {
  const [job, setJob] = useState<Job | null>(null)
  const [artifacts, setArtifacts] = useState<JobArtifact[]>([])
  const [recordings, setRecordings] = useState<Recording[]>([])
  const [summary, setSummary] = useState<ReturnType<typeof parseSummaryJSON>>(null)
  const [lines, setLines] = useState<string[]>([])
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    const d = await getJob(jobId, { bearer })
    setJob(d.job)
    setArtifacts(d.artifacts ?? [])
    if (d.artifacts?.some((a) => a.kind === 'summary')) {
      try {
        const raw = await fetchArtifactJSON(jobId, 'summary', bearer)
        setSummary(parseSummaryJSON(raw))
      } catch {
        setSummary(null)
      }
    }
    if (d.job.status === 'running' || d.job.status === 'pending') {
      const recs = await listRecordings(jobId, { bearer }).catch(() => ({ recordings: [] as Recording[] }))
      setRecordings(recs.recordings ?? [])
    } else {
      const recs = await listRecordings(jobId, { bearer }).catch(() => ({ recordings: [] as Recording[] }))
      setRecordings(recs.recordings ?? [])
    }
  }, [jobId, bearer])

  useEffect(() => {
    void refresh().catch((e) => setError(String(e instanceof Error ? e.message : e)))
  }, [refresh])

  useEffect(() => {
    if (!job) return
    if (job.status !== 'running' && job.status !== 'pending') {
      onFinished?.(job)
    }
  }, [job, onFinished])

  useEffect(() => {
    if (!job || (job.status !== 'running' && job.status !== 'pending')) return
    const t = setInterval(() => void refresh().catch(() => {}), 2500)
    return () => clearInterval(t)
  }, [job, refresh])

  useEffect(() => {
    let cancelled = false
    const ctrl = new AbortController()
    const follow = job?.status === 'running' || job?.status === 'pending'
    const url = jobEventsURL(jobId, bearer, { tail: 25, follow })
    fetch(url, { signal: ctrl.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const reader = res.body?.getReader()
        if (!reader) throw new Error('no body')
        const dec = new TextDecoder()
        let buf = ''
        const pump = (): Promise<void> =>
          reader.read().then(({ done, value }) => {
            if (cancelled || done) return
            buf += dec.decode(value, { stream: true })
            const parts = buf.split('\n')
            buf = parts.pop() ?? ''
            if (parts.length > 0) {
              setLines((prev) => {
                const next = [...prev, ...parts.filter((p) => p.length > 0)]
                return next.length > 200 ? next.slice(next.length - 200) : next
              })
            }
            return pump()
          })
        return pump()
      })
      .catch((err) => {
        if (cancelled || err.name === 'AbortError') return
        setError(String(err.message ?? err))
      })
    return () => {
      cancelled = true
      ctrl.abort()
    }
  }, [jobId, bearer, job?.status])

  const chartPoints = useMemo(() => parseStatsLines(lines), [lines])
  const running = job?.status === 'running' || job?.status === 'pending'

  const onStopClick = () => {
    void stopJob(jobId, { bearer }).then(() => {
      onStop?.()
      return refresh()
    })
  }

  if (!job) {
    return <p className="text-muted-foreground text-xs">Loading job {jobId.slice(0, 8)}…</p>
  }

  return (
    <div className={`border-border flex flex-col gap-3 rounded-lg border p-3 ${compact ? 'text-xs' : ''}`}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="font-mono text-xs">
          <span className="text-muted-foreground">Job</span> {job.id.slice(0, 12)}…{' '}
          <span className="rounded bg-muted px-1.5 py-0.5">{job.status}</span>
        </div>
        <div className="flex flex-wrap gap-1">
          {running ? (
            <Button type="button" size="xs" variant="outline" onClick={onStopClick}>
              Stop
            </Button>
          ) : null}
          {job.status === 'succeeded' && onOpenReports ? (
            <Button type="button" size="xs" variant="outline" onClick={() => onOpenReports(jobId)}>
              Open report
            </Button>
          ) : null}
          <Button type="button" size="xs" variant="ghost" onClick={() => void refresh()}>
            Refresh
          </Button>
        </div>
      </div>
      {summary ? <SummaryKPICards kpi={summary} /> : null}
      <JobStatsChart points={chartPoints} />
      {error ? <p className="text-destructive text-[11px]">{error}</p> : null}
      {!compact ? (
        <pre className="bg-muted/40 max-h-32 overflow-auto rounded p-2 font-mono text-[10px] whitespace-pre-wrap">
          {lines.length === 0 ? 'waiting for worker events…' : lines.slice(-15).join('\n')}
        </pre>
      ) : null}
      {recordings.length > 0 ? (
        <p className="text-muted-foreground text-[11px]">{recordings.length} recording(s) available</p>
      ) : null}
      {artifacts.length > 0 ? (
        <p className="text-muted-foreground text-[11px]">{artifacts.length} artifact(s)</p>
      ) : null}
    </div>
  )
}
