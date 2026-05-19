/** Relative path under ui_data_dir for a job artifact file. */
export function jobArtifactRelPath(jobId: string, filename: string): string {
  return `artifacts/jobs/${jobId}/${filename}`
}

export type ReportArtifactKind = 'summary' | 'report_html' | 'report_pdf' | 'log' | 'stats'

export const REPORT_KIND_LABEL: Record<ReportArtifactKind, string> = {
  summary: 'Summary JSON',
  report_html: 'HTML report',
  report_pdf: 'PDF report',
  log: 'Worker log',
  stats: 'Stats JSONL',
}

export function pickLatestArtifact(artifacts: { kind: string; path: string }[], kind: string) {
  for (let i = artifacts.length - 1; i >= 0; i--) {
    if (artifacts[i].kind === kind) return artifacts[i]
  }
  return undefined
}
