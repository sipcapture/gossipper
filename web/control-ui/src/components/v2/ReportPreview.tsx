import { artifactURL } from '@/api/v2'
import { SummaryKPICards } from '@/components/v2/SummaryKPI'
import { fetchArtifactJSON, fetchArtifactText } from '@/lib/artifacts'
import { parseSummaryJSON } from '@/lib/summaryParse'
import { useEffect, useState } from 'react'

export type ReportPreviewProps = {
  jobId: string
  bearer?: string
  mode?: 'summary' | 'html' | 'both'
}

export function ReportPreview({ jobId, bearer, mode = 'both' }: ReportPreviewProps) {
  const [summary, setSummary] = useState<ReturnType<typeof parseSummaryJSON>>(null)
  const [html, setHtml] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let alive = true
    void (async () => {
      try {
        if (mode === 'html' || mode === 'both') {
          const text = await fetchArtifactText(jobId, 'report_html', bearer)
          if (alive) setHtml(text)
        }
        if (mode === 'summary' || mode === 'both') {
          const raw = await fetchArtifactJSON(jobId, 'summary', bearer)
          if (alive) setSummary(parseSummaryJSON(raw))
        }
      } catch (e) {
        if (alive) setError(String(e instanceof Error ? e.message : e))
      }
    })()
    return () => {
      alive = false
    }
  }, [jobId, bearer, mode])

  if (error) return <p className="text-muted-foreground text-xs">Preview unavailable: {error}</p>

  return (
    <div className="flex flex-col gap-3">
      {summary ? <SummaryKPICards kpi={summary} /> : null}
      {summary?.findings.length ? (
        <ul className="text-destructive list-inside list-disc text-xs">
          {summary.findings.map((f, i) => (
            <li key={i}>{f}</li>
          ))}
        </ul>
      ) : null}
      {html ? (
        <iframe
          title="report preview"
          className="border-border h-[420px] w-full rounded-md border bg-white"
          srcDoc={html}
          sandbox="allow-same-origin"
        />
      ) : mode !== 'summary' ? (
        <p className="text-muted-foreground text-xs">
          No HTML report yet.{' '}
          <a className="text-primary underline" href={artifactURL(jobId, 'summary', bearer)} target="_blank" rel="noreferrer">
            Open summary
          </a>
        </p>
      ) : null}
    </div>
  )
}
