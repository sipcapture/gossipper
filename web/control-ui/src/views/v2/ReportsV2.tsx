import { useCallback, useEffect, useMemo, useState } from 'react'

import {
  artifactURL,
  listReports,
  runTool,
  type JobArtifact,
  type ReportRow,
} from '@/api/v2'
import { REPORT_KIND_LABEL, type ReportArtifactKind } from '@/lib/jobArtifacts'
import { Button } from '@/components/ui/button'
import { DataTable, type Column } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'

export type ReportsV2Props = {
  bearer?: string
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  onOpenJob?: (jobId: string) => void
}

export function ReportsV2({ bearer, run, onOpenJob }: ReportsV2Props) {
  const [rows, setRows] = useState<ReportRow[]>([])
  const [query, setQuery] = useState('')

  const refresh = useCallback(async () => {
    const r = await listReports({ bearer }, 200)
    setRows(r.reports ?? [])
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

  const columns: Column<ReportRow>[] = useMemo(
    () => [
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
        key: 'size',
        header: 'Size',
        render: (r) => <span className="text-muted-foreground text-xs">{r.artifact.size_bytes} B</span>,
      },
      {
        key: 'actions',
        header: '',
        align: 'right',
        render: (r) => (
          <div className="flex justify-end gap-1">
            <Button type="button" size="xs" variant="outline" asChild>
              <a
                href={artifactURL(r.job_id, r.artifact.kind, bearer)}
                target="_blank"
                rel="noreferrer noopener"
              >
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
    [bearer, onOpenJob],
  )

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold">Reports</h2>
          <p className="text-muted-foreground text-xs">
            Summary JSON, HTML, and PDF artifacts from completed jobs. Generate HTML/PDF from a job detail
            on the Jobs page, or run stress tools under{' '}
            <code className="text-[11px]">report-html</code> / <code className="text-[11px]">summary-to-pdf</code>.
          </p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={() => void run(() => refresh())}>
          Refresh
        </Button>
      </div>
      <Input
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="Filter by job id, profile, kind…"
        className="max-w-sm text-xs"
      />
      <DataTable
        columns={columns}
        rows={visible}
        rowKey={(r) => `${r.job_id}-${r.artifact.id}`}
        empty="No reports yet — run a load job with summary output."
      />
    </section>
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
