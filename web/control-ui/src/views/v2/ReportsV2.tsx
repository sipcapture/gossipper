import { useCallback, useEffect, useMemo, useState } from 'react'

import {
  artifactURL,
  listReports,
  runTool,
  type JobArtifact,
  type ReportRow,
} from '@/api/v2'
import { ReportPreview } from '@/components/v2/ReportPreview'
import { SummaryKPICards } from '@/components/v2/SummaryKPI'
import { Button } from '@/components/ui/button'
import { DataTable, type Column } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { Modal } from '@/components/ui/modal'
import { fetchArtifactBytes, fetchArtifactJSON } from '@/lib/artifacts'
import { REPORT_KIND_LABEL, type ReportArtifactKind } from '@/lib/jobArtifacts'
import { parseSummaryJSON } from '@/lib/summaryParse'
import { buildZip } from '@/lib/simpleZip'
import { formatRatio } from '@/lib/summaryParse'

export type ReportsV2Props = {
  bearer?: string
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  onOpenJob?: (jobId: string) => void
  initialJobFilter?: string | null
}

type RowKPI = { success_ratio?: number; total_calls?: number; health_ok?: boolean | null }

export function ReportsV2({ bearer, run, onOpenJob, initialJobFilter }: ReportsV2Props) {
  const [rows, setRows] = useState<ReportRow[]>([])
  const [query, setQuery] = useState(initialJobFilter ?? '')
  const [kpiMap, setKpiMap] = useState<Record<string, RowKPI>>({})
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [compareA, setCompareA] = useState<string>('')
  const [compareB, setCompareB] = useState<string>('')
  const [previewJobId, setPreviewJobId] = useState<string | null>(null)

  useEffect(() => {
    if (initialJobFilter) setQuery(initialJobFilter)
  }, [initialJobFilter])

  const refresh = useCallback(async () => {
    const r = await listReports({ bearer }, 200)
    setRows(r.reports ?? [])
    const summaries = (r.reports ?? []).filter((x) => x.artifact.kind === 'summary')
    const next: Record<string, RowKPI> = {}
    await Promise.all(
      summaries.map(async (row) => {
        try {
          const raw = await fetchArtifactJSON(row.job_id, 'summary', bearer)
          const k = parseSummaryJSON(raw)
          if (k) next[row.job_id] = { success_ratio: k.success_ratio, total_calls: k.total_calls, health_ok: k.health_ok }
        } catch {
          /* ignore */
        }
      }),
    )
    setKpiMap(next)
  }, [bearer])

  useEffect(() => {
    void run(() => refresh())
  }, [run, refresh])

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return rows
    return rows.filter(
      (r) =>
        r.job_id.toLowerCase().includes(q) ||
        (r.profile_id ?? '').toLowerCase().includes(q) ||
        r.artifact.kind.toLowerCase().includes(q),
    )
  }, [rows, query])

  const toggleSelect = (key: string) => {
    setSelected((prev) => {
      const n = new Set(prev)
      if (n.has(key)) n.delete(key)
      else n.add(key)
      return n
    })
  }

  const onBulkZip = () => {
    void run(async () => {
      const picks = visible.filter((r) => selected.has(`${r.job_id}-${r.artifact.id}`))
      if (picks.length === 0) return
      const files: { name: string; data: Uint8Array }[] = []
      for (const p of picks) {
        const bytes = await fetchArtifactBytes(p.job_id, p.artifact.kind, bearer)
        files.push({ name: `${p.job_id.slice(0, 8)}-${p.artifact.kind}`, data: bytes })
      }
      const blob = await buildZip(files)
      const a = document.createElement('a')
      a.href = URL.createObjectURL(blob)
      a.download = `gossipper-reports-${Date.now()}.zip`
      a.click()
      URL.revokeObjectURL(a.href)
    })
  }

  const columns: Column<ReportRow>[] = useMemo(
    () => [
      {
        key: 'sel',
        header: '',
        render: (r) => (
          <input
            type="checkbox"
            checked={selected.has(`${r.job_id}-${r.artifact.id}`)}
            onChange={() => toggleSelect(`${r.job_id}-${r.artifact.id}`)}
          />
        ),
      },
      {
        key: 'kind',
        header: 'Type',
        render: (r) => (
          <span className="text-xs">
            {REPORT_KIND_LABEL[r.artifact.kind as ReportArtifactKind] ?? r.artifact.kind}
          </span>
        ),
      },
      {
        key: 'kpi',
        header: 'KPI',
        render: (r) => {
          const k = kpiMap[r.job_id]
          if (!k || r.artifact.kind !== 'summary') return <span className="text-muted-foreground">—</span>
          return (
            <span className="text-xs">
              {k.total_calls ?? '—'} calls · {formatRatio(k.success_ratio ?? 0)}
              {k.health_ok === false ? ' · FAIL' : k.health_ok ? ' · OK' : ''}
            </span>
          )
        },
      },
      {
        key: 'job',
        header: 'Job',
        render: (r) => (
          <button
            type="button"
            className="font-mono text-xs underline-offset-2 hover:underline"
            onClick={() => onOpenJob?.(r.job_id)}
          >
            {r.job_id.slice(0, 8)}…
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
        key: 'status',
        header: 'Job status',
        render: (r) => <span className="text-xs">{r.job_status}</span>,
      },
      {
        key: 'created',
        header: 'Generated',
        render: (r) => new Date(r.artifact.created_at).toLocaleString(),
      },
      {
        key: 'actions',
        header: '',
        align: 'right',
        render: (r) => (
          <div className="flex justify-end gap-1">
            {r.artifact.kind === 'report_html' || r.artifact.kind === 'summary' ? (
              <Button type="button" size="xs" variant="secondary" onClick={() => setPreviewJobId(r.job_id)}>
                Preview
              </Button>
            ) : null}
            <Button type="button" size="xs" variant="outline" asChild>
              <a href={artifactURL(r.job_id, r.artifact.kind, bearer)} target="_blank" rel="noreferrer noopener">
                Open
              </a>
            </Button>
            <Button type="button" size="xs" variant="ghost" asChild>
              <a href={artifactURL(r.job_id, r.artifact.kind, bearer)} download>
                Download
              </a>
            </Button>
          </div>
        ),
      },
    ],
    [bearer, kpiMap, onOpenJob, selected],
  )

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold">Reports</h2>
          <p className="text-muted-foreground text-xs">
            Summary JSON, HTML, and PDF from jobs. Select rows for bulk ZIP export; compare two summary jobs below.
          </p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" size="sm" disabled={selected.size === 0} onClick={onBulkZip}>
            Download ZIP ({selected.size})
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={() => void run(() => refresh())}>
            Refresh
          </Button>
        </div>
      </div>
      <Input
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="Filter by job id, profile, kind…"
        className="max-w-sm text-xs"
      />

      <div className="border-border grid grid-cols-1 gap-2 rounded-md border p-3 md:grid-cols-2">
        <div className="flex flex-col gap-1">
          <label className="text-[10px] font-medium">Compare job A</label>
          <Input value={compareA} onChange={(e) => setCompareA(e.target.value)} placeholder="job uuid" className="font-mono text-xs" />
        </div>
        <div className="flex flex-col gap-1">
          <label className="text-[10px] font-medium">Compare job B</label>
          <Input value={compareB} onChange={(e) => setCompareB(e.target.value)} placeholder="job uuid" className="font-mono text-xs" />
        </div>
        {compareA.trim() && compareB.trim() ? <CompareSummaries a={compareA.trim()} b={compareB.trim()} bearer={bearer} /> : null}
      </div>

      <DataTable
        columns={columns}
        rows={visible}
        rowKey={(r) => `${r.job_id}-${r.artifact.id}`}
        empty="No reports yet — run a load job with summary output."
      />

      <Modal open={previewJobId !== null} onClose={() => setPreviewJobId(null)} title="Report preview" size="lg">
        {previewJobId ? <ReportPreview jobId={previewJobId} bearer={bearer} /> : null}
      </Modal>
    </section>
  )
}

function CompareSummaries({ a, b, bearer }: { a: string; b: string; bearer?: string }) {
  const [ka, setKa] = useState<ReturnType<typeof parseSummaryJSON>>(null)
  const [kb, setKb] = useState<ReturnType<typeof parseSummaryJSON>>(null)
  useEffect(() => {
    void (async () => {
      try {
        setKa(parseSummaryJSON(await fetchArtifactJSON(a, 'summary', bearer)))
        setKb(parseSummaryJSON(await fetchArtifactJSON(b, 'summary', bearer)))
      } catch {
        setKa(null)
        setKb(null)
      }
    })()
  }, [a, b, bearer])
  if (!ka || !kb) return <p className="text-muted-foreground col-span-2 text-xs">Load summaries for both jobs…</p>
  return (
    <div className="col-span-2 grid grid-cols-1 gap-3 md:grid-cols-2">
      <div>
        <div className="mb-1 font-mono text-[10px]">{a.slice(0, 12)}…</div>
        <SummaryKPICards kpi={ka} />
      </div>
      <div>
        <div className="mb-1 font-mono text-[10px]">{b.slice(0, 12)}…</div>
        <SummaryKPICards kpi={kb} />
      </div>
    </div>
  )
}

export type JobReportsPanelProps = {
  jobId: string
  artifacts: JobArtifact[]
  bearer?: string
  busy?: boolean
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  onGenerated?: () => void
}

export function JobReportsPanel({
  jobId,
  artifacts,
  bearer,
  run,
  onGenerated,
}: JobReportsPanelProps) {
  const hasSummary = artifacts.some((a) => a.kind === 'summary')
  const hasHTML = artifacts.some((a) => a.kind === 'report_html')
  const hasPDF = artifacts.some((a) => a.kind === 'report_pdf')

  const reportKinds: ReportArtifactKind[] = ['summary', 'report_html', 'report_pdf']

  const onGenerateHTML = () => {
    void run(async () => {
      await runTool(
        'report-html',
        {
          args: {
            in: `artifacts/jobs/${jobId}/summary.json`,
            out: `artifacts/jobs/${jobId}/report.html`,
          },
        },
        { bearer },
      )
      onGenerated?.()
    })
  }

  const onGeneratePDF = () => {
    void run(async () => {
      await runTool(
        'summary-to-pdf',
        {
          args: {
            in: `artifacts/jobs/${jobId}/report.html`,
            out: `artifacts/jobs/${jobId}/report.pdf`,
          },
        },
        { bearer },
      )
      onGenerated?.()
    })
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="text-muted-foreground font-medium">Reports</div>
        <div className="flex flex-wrap gap-1">
          {hasSummary && !hasHTML ? (
            <Button type="button" size="xs" variant="outline" onClick={onGenerateHTML}>
              Generate HTML
            </Button>
          ) : null}
          {hasHTML && !hasPDF ? (
            <Button type="button" size="xs" variant="outline" onClick={onGeneratePDF}>
              Generate PDF
            </Button>
          ) : null}
        </div>
      </div>
      <ul className="space-y-1">
        {reportKinds.map((kind) => {
          const art = artifacts.filter((a) => a.kind === kind).at(-1)
          const label = REPORT_KIND_LABEL[kind]
          if (!art) {
            return (
              <li key={kind} className="text-muted-foreground flex items-center gap-2 text-xs">
                <span className="min-w-[7rem]">{label}</span>
                <span>—</span>
              </li>
            )
          }
          return (
            <li key={kind} className="flex flex-wrap items-center gap-2 text-xs">
              <span className="min-w-[7rem] font-medium">{label}</span>
              <span className="text-muted-foreground font-mono">{art.size_bytes} B</span>
              <Button type="button" size="xs" variant="outline" asChild>
                <a href={artifactURL(jobId, kind, bearer)} target="_blank" rel="noreferrer noopener">
                  Open
                </a>
              </Button>
              <Button type="button" size="xs" variant="ghost" asChild>
                <a href={artifactURL(jobId, kind, bearer)} download>
                  Download
                </a>
              </Button>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
