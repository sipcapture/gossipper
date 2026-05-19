import { artifactURL, type JobArtifact } from '@/api/v2'
import { REPORT_KIND_LABEL, type ReportArtifactKind } from '@/lib/jobArtifacts'
import { Button } from '@/components/ui/button'

const DOWNLOADABLE = ['summary', 'report_html', 'report_pdf', 'log', 'stats'] as const

export function ArtifactLinks({
  jobId,
  artifacts,
  bearer,
}: {
  jobId: string
  artifacts: JobArtifact[]
  bearer?: string
}) {
  if (artifacts.length === 0) return <p className="text-muted-foreground text-xs">No files yet.</p>
  return (
    <ul className="space-y-1">
      {artifacts.map((a) => {
        const canDownload = (DOWNLOADABLE as readonly string[]).includes(a.kind)
        const label = REPORT_KIND_LABEL[a.kind as ReportArtifactKind] ?? a.kind
        return (
          <li key={a.id} className="flex flex-wrap items-center gap-2 font-mono text-xs">
            <span className="text-foreground/80 min-w-[6rem]">[{label}]</span>
            <span className="text-muted-foreground truncate">{a.path}</span>
            <span className="text-muted-foreground">({a.size_bytes} B)</span>
            {canDownload ? (
              <>
                <Button type="button" size="xs" variant="outline" asChild>
                  <a href={artifactURL(jobId, a.kind, bearer)} target="_blank" rel="noreferrer noopener">
                    Open
                  </a>
                </Button>
                <Button type="button" size="xs" variant="ghost" asChild>
                  <a href={artifactURL(jobId, a.kind, bearer)} download>
                    Download
                  </a>
                </Button>
              </>
            ) : null}
          </li>
        )
      })}
    </ul>
  )
}
