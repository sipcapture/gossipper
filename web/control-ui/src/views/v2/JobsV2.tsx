import { useCallback, useEffect, useMemo, useState } from 'react'

import {
  deleteJobV2,
  getJob,
  listBuiltinScenarios,
  listClients,
  listJobs,
  listRecordings,
  listScenarios,
  listServers,
  jobEventsURL,
  recordingURL,
  startJob,
  stopJob,
  type BuiltinScenarioMeta,
  type ClientProfile,
  type Job,
  type JobArtifact,
  type JobStatus,
  type Recording,
  type ScenarioMeta,
  type ServerProfile,
} from '@/api/v2'
import { JobStatsChart } from '@/components/v2/JobStatsChart'
import { ScenarioPreview, ScenarioSelect } from '@/components/v2/ScenarioSelect'
import { Button } from '@/components/ui/button'
import { DataTable, type Column } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Modal } from '@/components/ui/modal'
import { isProfilePortBlocked, profileHasWebRTC } from '@/lib/profileHelpers'
import { mergeLiveJobs, parseStatsLines, useLiveJobs } from '@/lib/jobsLive'
import { useToast } from '@/lib/toast'

const STATUS_BADGE: Record<JobStatus, string> = {
  pending: 'bg-muted text-foreground/80',
  running: 'bg-success/15 text-success',
  succeeded: 'bg-success/10 text-success',
  failed: 'bg-destructive/15 text-destructive',
  stopped: 'bg-warning/15 text-warning',
}

export type JobsV2Props = {
  bearer?: string
  busy: boolean
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  errorText?: string | null
}

export function JobsV2({ bearer, busy, run, errorText }: JobsV2Props) {
  const { toast } = useToast()
  const { liveJobs, connected } = useLiveJobs(bearer)
  const [rows, setRows] = useState<Job[]>([])
  const [servers, setServers] = useState<ServerProfile[]>([])
  const [clients, setClients] = useState<ClientProfile[]>([])
  const [scenarios, setScenarios] = useState<ScenarioMeta[]>([])
  const [builtins, setBuiltins] = useState<BuiltinScenarioMeta[]>([])
  const [startOpen, setStartOpen] = useState(false)
  const [draft, setDraft] = useState<{
    profile_kind: 'server' | 'client'
    profile_id: string
    scenario_id: string
    record_wav: boolean
    record_wav_duplex: boolean
  }>({
    profile_kind: 'server',
    profile_id: '',
    scenario_id: '',
    record_wav: false,
    record_wav_duplex: false,
  })
  const [detail, setDetail] = useState<
    { job: Job; artifacts: JobArtifact[]; recordings: Recording[] } | null
  >(null)
  const [statusFilter, setStatusFilter] = useState<'all' | JobStatus>('all')
  const [query, setQuery] = useState('')

  const refresh = useCallback(async () => {
    const [r, s, c, sc, bi] = await Promise.all([
      listJobs({ bearer }, 200),
      listServers({ bearer }),
      listClients({ bearer }),
      listScenarios({ bearer }),
      listBuiltinScenarios({ bearer }).catch(() => ({ scenarios: [] as BuiltinScenarioMeta[] })),
    ])
    setRows(r.jobs ?? [])
    setServers(s.servers ?? [])
    setClients(c.clients ?? [])
    setScenarios(sc.scenarios ?? [])
    setBuiltins(bi.scenarios ?? [])
  }, [bearer])

  useEffect(() => {
    void run(() => refresh())
  }, [run, refresh])

  const mergedRows = useMemo(() => mergeLiveJobs(rows, liveJobs), [rows, liveJobs])

  const candidateProfiles = useMemo(
    () => (draft.profile_kind === 'server' ? servers : clients),
    [draft.profile_kind, servers, clients],
  )

  const startBlocked = useMemo(() => {
    if (!draft.profile_id) return { blocked: false, details: [] as string[] }
    return isProfilePortBlocked(draft.profile_kind, draft.profile_id, servers, clients)
  }, [draft.profile_id, draft.profile_kind, servers, clients])

  const scenarioRoleFilter = draft.profile_kind === 'server' ? 'uas' : 'uac'

  const visibleRows = useMemo(() => {
    const q = query.trim().toLowerCase()
    return mergedRows.filter((j) => {
      if (statusFilter !== 'all' && j.status !== statusFilter) return false
      if (!q) return true
      return (
        j.id.toLowerCase().includes(q) ||
        (j.profile_id ?? '').toLowerCase().includes(q) ||
        (j.scenario_id ?? '').toLowerCase().includes(q) ||
        (j.profile_kind ?? '').toLowerCase().includes(q)
      )
    })
  }, [mergedRows, statusFilter, query])

  const statusCounts = useMemo(() => {
    const out: Record<string, number> = { all: mergedRows.length }
    for (const j of mergedRows) out[j.status] = (out[j.status] ?? 0) + 1
    return out
  }, [mergedRows])

  const openStartModal = (prefill?: Partial<typeof draft>) => {
    setDraft({
      profile_kind: 'server',
      profile_id: '',
      scenario_id: '',
      record_wav: false,
      record_wav_duplex: false,
      ...prefill,
    })
    setStartOpen(true)
  }

  const onStart = () => {
    if (!draft.profile_id || startBlocked.blocked) return
    void run(async () => {
      await startJob(
        {
          profile_kind: draft.profile_kind,
          profile_id: draft.profile_id,
          scenario_id: draft.scenario_id || undefined,
          record_wav: draft.record_wav || undefined,
          record_wav_duplex: draft.record_wav && draft.record_wav_duplex ? true : undefined,
        },
        { bearer },
      )
      setStartOpen(false)
      toast('Job started', 'success')
      await refresh()
    })
  }

  const onRestart = (job: Job) => {
    if (!job.profile_id || !job.profile_kind) return
    openStartModal({
      profile_kind: job.profile_kind as 'server' | 'client',
      profile_id: job.profile_id,
      scenario_id: job.scenario_id ?? '',
    })
  }

  const onStop = (id: string) => {
    void run(async () => {
      await stopJob(id, { bearer })
      toast('Stop requested', 'info')
      await refresh()
    })
  }
  const onDelete = (id: string) => {
    if (!window.confirm(`Delete job ${id}?`)) return
    void run(async () => {
      await deleteJobV2(id, { bearer })
      toast('Job deleted', 'success')
      await refresh()
    })
  }
  const onInspect = (id: string) => {
    void run(async () => {
      const [d, recs] = await Promise.all([
        getJob(id, { bearer }),
        listRecordings(id, { bearer }).catch(() => ({ recordings: [] as Recording[] })),
      ])
      setDetail({ ...d, recordings: recs.recordings ?? [] })
    })
  }

  const columns: Column<Job>[] = useMemo(
    () => [
      {
        key: 'id',
        header: 'Job',
        render: (r) => (
          <button
            type="button"
            onClick={() => onInspect(r.id)}
            className="font-mono text-xs underline-offset-2 hover:underline"
          >
            {r.id.slice(0, 8)}…
          </button>
        ),
      },
      {
        key: 'profile',
        header: 'Profile',
        render: (r) => (
          <span className="text-xs">
            <code>{r.profile_kind ?? '?'}</code> · {r.profile_id ?? '—'}
          </span>
        ),
      },
      {
        key: 'scenario',
        header: 'Scenario',
        render: (r) => (r.scenario_id ? <code className="text-xs">{r.scenario_id}</code> : '—'),
      },
      {
        key: 'status',
        header: 'Status',
        render: (r) => (
          <span className="inline-flex items-center gap-1">
            <span
              className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${STATUS_BADGE[r.status] ?? ''}`}
            >
              {r.status}
            </span>
            {(() => {
              const prof =
                r.profile_kind === 'server'
                  ? servers.find((s) => s.id === r.profile_id)
                  : clients.find((c) => c.id === r.profile_id)
              return profileHasWebRTC(prof?.transports) ? (
                <span className="bg-warning/15 text-warning rounded px-1 py-0.5 text-[9px]">WebRTC</span>
              ) : null
            })()}
          </span>
        ),
      },
      {
        key: 'created',
        header: 'Created',
        render: (r) => new Date(r.created_at).toLocaleString(),
      },
      {
        key: 'actions',
        header: '',
        align: 'right',
        render: (r) => (
          <div className="flex justify-end gap-1">
            {r.status === 'failed' || r.status === 'stopped' ? (
              <Button type="button" variant="outline" size="xs" onClick={() => onRestart(r)}>
                Restart
              </Button>
            ) : null}
            {r.status === 'running' || r.status === 'pending' ? (
              <Button type="button" variant="outline" size="xs" onClick={() => onStop(r.id)}>
                Stop
              </Button>
            ) : null}
            <Button type="button" variant="destructive" size="xs" onClick={() => onDelete(r.id)}>
              Delete
            </Button>
          </div>
        ),
      },
    ],
    [clients, onDelete, onInspect, onRestart, onStop, servers],
  )

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold">Jobs</h2>
          <p className="text-muted-foreground text-xs">
            Isolated <code>gossipper worker</code> runs forked from server/client profiles. Status updates
            via live WebSocket{connected ? ' (connected)' : ' (reconnecting…)'}.
          </p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" size="sm" onClick={() => void run(() => refresh())}>
            Refresh
          </Button>
          <Button type="button" size="sm" onClick={() => openStartModal()}>
            + Start job
          </Button>
        </div>
      </div>

      {errorText ? (
        <div className="border-destructive/40 bg-destructive/10 text-destructive rounded-md border px-3 py-2 text-xs">
          {errorText}
        </div>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <div className="flex flex-wrap gap-1">
          {(['all', 'running', 'pending', 'succeeded', 'failed', 'stopped'] as const).map((s) => (
            <button
              key={s}
              type="button"
              onClick={() => setStatusFilter(s)}
              className={`rounded px-2 py-0.5 text-[11px] ${
                statusFilter === s
                  ? 'bg-primary text-primary-foreground'
                  : 'border-border bg-background hover:bg-muted border'
              }`}
            >
              {s} <span className="text-[10px] opacity-75">({statusCounts[s] ?? 0})</span>
            </button>
          ))}
        </div>
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="filter by id / profile / scenario…"
          className="h-7 max-w-xs text-xs"
        />
        <span className="text-muted-foreground text-[11px]">
          {visibleRows.length}/{mergedRows.length}
        </span>
      </div>

      <DataTable
        rows={visibleRows}
        columns={columns}
        rowKey={(r) => r.id}
        loading={busy && rows.length === 0}
        empty={rows.length === 0 ? 'No jobs yet.' : 'No jobs match the current filter.'}
      />

      <Modal
        open={startOpen}
        onClose={() => setStartOpen(false)}
        size="md"
        title="Start a new job"
        footer={
          <>
            <Button type="button" variant="outline" size="sm" onClick={() => setStartOpen(false)}>
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={onStart}
              disabled={!draft.profile_id || busy || startBlocked.blocked}
            >
              Start
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-3">
          {startBlocked.blocked ? (
            <div className="border-destructive/40 bg-destructive/10 text-destructive rounded-md border px-2 py-1.5 text-[11px]">
              Port conflict — resolve overlapping binds before starting:{' '}
              {startBlocked.details.join(', ')}
            </div>
          ) : null}
          <div>
            <Label className="text-xs">Profile kind</Label>
            <div className="mt-1 flex gap-2">
              {(['server', 'client'] as const).map((k) => (
                <button
                  key={k}
                  type="button"
                  onClick={() => setDraft({ ...draft, profile_kind: k, profile_id: '' })}
                  className={`rounded-md px-3 py-1.5 text-xs ${
                    draft.profile_kind === k
                      ? 'bg-primary text-primary-foreground'
                      : 'border-border bg-background hover:bg-muted border'
                  }`}
                >
                  {k}
                </button>
              ))}
            </div>
          </div>
          <div>
            <Label className="text-xs">Profile</Label>
            <select
              value={draft.profile_id}
              onChange={(e) => setDraft({ ...draft, profile_id: e.target.value })}
              className="border-input bg-background mt-1 w-full rounded-md border px-2 py-1.5 text-sm"
            >
              <option value="">— select —</option>
              {candidateProfiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.id} — {p.name}
                </option>
              ))}
            </select>
          </div>
          <div>
            <Label className="text-xs">Scenario override (optional)</Label>
            <ScenarioSelect
              value={draft.scenario_id}
              onChange={(scenario_id) => setDraft({ ...draft, scenario_id })}
              scenarios={scenarios}
              builtins={builtins}
              roleFilter={scenarioRoleFilter}
              allowEmpty
              emptyLabel="(use profile default)"
            />
            <ScenarioPreview
              scenarioId={draft.scenario_id}
              scenarios={scenarios}
              builtins={builtins}
            />
          </div>
          <div className="flex flex-col gap-1 pt-1">
            <label className="text-foreground flex items-center gap-2 text-xs">
              <input
                type="checkbox"
                checked={draft.record_wav}
                onChange={(e) => setDraft({ ...draft, record_wav: e.target.checked })}
              />
              Record received RTP to WAV (one file per call, decoded G.711)
            </label>
            {draft.record_wav ? (
              <label className="text-muted-foreground ml-5 flex items-center gap-2 text-xs">
                <input
                  type="checkbox"
                  checked={draft.record_wav_duplex}
                  onChange={(e) => setDraft({ ...draft, record_wav_duplex: e.target.checked })}
                />
                Stereo (L=sent, R=received)
              </label>
            ) : null}
          </div>
        </div>
      </Modal>

      <Modal
        open={detail !== null}
        onClose={() => setDetail(null)}
        size="lg"
        title={detail ? `Job · ${detail.job.id}` : 'Job'}
      >
        {detail ? (
          <div className="flex flex-col gap-3 text-xs">
            <DescList
              rows={[
                ['Status', detail.job.status],
                ['Profile', `${detail.job.profile_kind ?? '?'} / ${detail.job.profile_id ?? '—'}`],
                ['Scenario', detail.job.scenario_id ?? '—'],
                ['Created', new Date(detail.job.created_at).toLocaleString()],
                ['Started', detail.job.started_at ? new Date(detail.job.started_at).toLocaleString() : '—'],
                ['Finished', detail.job.finished_at ? new Date(detail.job.finished_at).toLocaleString() : '—'],
                ['Exit code', detail.job.exit_code ?? '—'],
                ['PID', detail.job.pid ?? '—'],
                ['Artifacts dir', detail.job.artifacts_dir ?? '—'],
                ['Error', detail.job.error || '—'],
              ]}
            />
            <div>
              <div className="text-muted-foreground mb-1 font-medium">Args</div>
              <pre className="bg-muted/40 max-h-32 overflow-auto rounded p-2 font-mono text-[11px]">
                {detail.job.args_json ?? '{}'}
              </pre>
            </div>
            <div>
              <div className="text-muted-foreground mb-1 font-medium">Artifacts ({detail.artifacts.length})</div>
              {detail.artifacts.length === 0 ? (
                <p className="text-muted-foreground">No files yet.</p>
              ) : (
                <ul className="space-y-1">
                  {detail.artifacts.map((a) => (
                    <li key={a.id} className="font-mono">
                      <span className="text-foreground/80 mr-2">[{a.kind}]</span>
                      {a.path} <span className="text-muted-foreground">({a.size_bytes} B)</span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <JobStatsTail jobId={detail.job.id} bearer={bearer} status={detail.job.status} />
            <div>
              <div className="text-muted-foreground mb-1 font-medium">
                Recordings ({detail.recordings.length})
              </div>
              {detail.recordings.length === 0 ? (
                <p className="text-muted-foreground">
                  No WAV files yet. Enable per-call recording when starting a job.
                </p>
              ) : (
                <ul className="space-y-2">
                  {detail.recordings.map((r) => (
                    <li key={r.name} className="flex flex-wrap items-center gap-3 font-mono">
                      <span className="min-w-[8rem]">{r.name}</span>
                      <span className="text-muted-foreground">{r.size_bytes} B</span>
                      <audio
                        controls
                        className="h-7"
                        src={recordingURL(detail.job.id, r.name, bearer)}
                      />
                      <a
                        href={recordingURL(detail.job.id, r.name, bearer)}
                        download={r.name}
                        className="bg-background border-border hover:bg-muted rounded-md border px-2 py-0.5 text-[11px]"
                      >
                        Download
                      </a>
                    </li>
                  ))}
                </ul>
              )}
            </div>
            {detail.job.status === 'failed' || detail.job.status === 'stopped' ? (
              <Button type="button" size="sm" variant="outline" onClick={() => onRestart(detail.job)}>
                Restart with same profile
              </Button>
            ) : null}
          </div>
        ) : null}
      </Modal>
    </section>
  )
}

function JobStatsTail({
  jobId,
  bearer,
  status,
}: {
  jobId: string
  bearer?: string
  status: string
}) {
  const [lines, setLines] = useState<string[]>([])
  const [error, setError] = useState<string | null>(null)
  const tailN = 25
  useEffect(() => {
    let cancelled = false
    const ctrl = new AbortController()
    const follow = status === 'running' || status === 'pending'
    const url = jobEventsURL(jobId, bearer, { tail: tailN, follow })
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
        if (cancelled) return
        if (err.name === 'AbortError') return
        setError(String(err.message ?? err))
      })
    return () => {
      cancelled = true
      ctrl.abort()
    }
  }, [jobId, bearer, status])

  const chartPoints = useMemo(() => parseStatsLines(lines), [lines])

  return (
    <div>
      <div className="text-muted-foreground mb-1 font-medium">Worker stats</div>
      <JobStatsChart points={chartPoints} />
      {error ? <p className="text-destructive mt-1 text-[11px]">{error}</p> : null}
      <pre className="bg-muted/40 mt-2 max-h-48 overflow-auto rounded p-2 font-mono text-[10px] whitespace-pre-wrap">
        {lines.length === 0 ? 'waiting for worker…' : lines.slice(-tailN).join('\n')}
      </pre>
    </div>
  )
}

function DescList({ rows }: { rows: [string, React.ReactNode][] }) {
  return (
    <dl className="grid grid-cols-3 gap-x-3 gap-y-1">
      {rows.map(([k, v], i) => (
        <div key={i} className="contents">
          <dt className="text-muted-foreground">{k}</dt>
          <dd className="col-span-2 font-mono break-all">{v ?? '—'}</dd>
        </div>
      ))}
    </dl>
  )
}
