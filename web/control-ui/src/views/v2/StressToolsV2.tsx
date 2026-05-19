import { useCallback, useEffect, useMemo, useState } from 'react'

import { listTools, runTool, type ToolMeta } from '@/api/v2'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Modal } from '@/components/ui/modal'
import { validateJobID } from '@/lib/jobId'
import {
  STRESS_TOOL_GROUPS,
  stressToolStatusLabel,
  type StressToolEntry,
  type StressToolNavTarget,
} from '@/lib/stressToolsCatalog'
import { useToast } from '@/lib/toast'
import { cn } from '@/lib/utils'

export type StressToolsV2Props = {
  bearer?: string
  busy?: boolean
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  onNavigate?: (target: StressToolNavTarget) => void
}

type RunDraft = {
  toolId: string
  title: string
  jobId: string
  argsText: string
}

function exampleArgsText(meta?: ToolMeta, entry?: StressToolEntry): string {
  const example = meta?.example_args ?? {}
  if (Object.keys(example).length > 0) {
    return JSON.stringify(example, null, 2)
  }
  if (entry?.apiToolId === 'infindex') {
    return JSON.stringify({ csv: 'media/inject/users.csv', field: 0 }, null, 2)
  }
  return '{\n  \n}'
}

export function StressToolsV2({ bearer, run, onNavigate }: StressToolsV2Props) {
  const { toast } = useToast()
  const [apiTools, setApiTools] = useState<ToolMeta[]>([])
  const [runOpen, setRunOpen] = useState(false)
  const [runDraft, setRunDraft] = useState<RunDraft | null>(null)
  const [argsError, setArgsError] = useState<string | null>(null)

  const refreshTools = useCallback(async () => {
    const r = await listTools({ bearer })
    setApiTools(r.tools ?? [])
  }, [bearer])

  useEffect(() => {
    void run(() => refreshTools())
  }, [run, refreshTools])

  const apiById = useMemo(() => new Map(apiTools.map((t) => [t.id, t])), [apiTools])

  const jobIdError = useMemo(
    () => (runDraft ? validateJobID(runDraft.jobId) : null),
    [runDraft],
  )

  const openRunModal = (entry: StressToolEntry) => {
    if (!entry.apiToolId) return
    const meta = apiById.get(entry.apiToolId)
    setArgsError(null)
    setRunDraft({
      toolId: entry.apiToolId,
      title: entry.title,
      jobId: '',
      argsText: exampleArgsText(meta, entry),
    })
    setRunOpen(true)
  }

  const onSubmitRun = () => {
    if (!runDraft || jobIdError) return
    let args: Record<string, unknown>
    try {
      args = JSON.parse(runDraft.argsText) as Record<string, unknown>
      if (args === null || typeof args !== 'object' || Array.isArray(args)) {
        throw new Error('args must be a JSON object')
      }
      setArgsError(null)
    } catch (err) {
      setArgsError(String(err instanceof Error ? err.message : err))
      return
    }
    void run(async () => {
      const out = await runTool(
        runDraft.toolId,
        { id: runDraft.jobId.trim() || undefined, args },
        { bearer },
      )
      setRunOpen(false)
      setRunDraft(null)
      toast(`Tool job started (${out.job.id.slice(0, 8)}…)`, 'success')
      onNavigate?.('jobs')
    })
  }

  return (
    <section className="flex max-w-4xl flex-col gap-4">
      <header className="flex flex-col gap-1">
        <h2 className="text-base font-semibold tracking-tight">Stress tools</h2>
        <p className="text-muted-foreground text-xs leading-relaxed">
          Sipstress-style utilities integrated into Gossipper. Tools with{' '}
          <strong className="text-foreground/90">Run as job</strong> submit an isolated{' '}
          <code className="text-[11px]">gossipper worker</code> — track status and artifacts on the{' '}
          <button
            type="button"
            className="text-primary underline-offset-2 hover:underline"
            onClick={() => onNavigate?.('jobs')}
          >
            Jobs
          </button>{' '}
          page (same lifecycle as server/client profiles). CLI examples still run on the host binary.
        </p>
      </header>

      {STRESS_TOOL_GROUPS.map((group) => (
        <div key={group.id} className="flex flex-col gap-2">
          <div>
            <h3 className="text-sm font-medium">{group.title}</h3>
            <p className="text-muted-foreground text-xs leading-relaxed">{group.description}</p>
          </div>
          <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
            {group.tools.map((tool) => {
              const apiMeta = tool.apiToolId ? apiById.get(tool.apiToolId) : undefined
              return (
                <Card key={tool.id} className="border-border/80">
                  <CardHeader className="pb-2">
                    <div className="flex items-start justify-between gap-2">
                      <CardTitle className="text-sm font-medium">{tool.title}</CardTitle>
                      <span
                        className={cn(
                          'shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium',
                          tool.status === 'ui'
                            ? 'bg-primary/15 text-primary'
                            : tool.status === 'cli'
                              ? 'bg-muted text-muted-foreground'
                              : 'bg-secondary text-secondary-foreground',
                        )}
                      >
                        {stressToolStatusLabel(tool.status)}
                      </span>
                    </div>
                  </CardHeader>
                  <CardContent className="flex flex-col gap-2 pt-0">
                    <p className="text-muted-foreground text-xs leading-relaxed">
                      {apiMeta?.summary ?? tool.summary}
                    </p>
                    {tool.cli ? (
                      <pre className="bg-muted overflow-x-auto rounded-md p-2 text-[10px] leading-snug">
                        {tool.cli}
                      </pre>
                    ) : null}
                    {apiMeta?.args_schema ? (
                      <details className="text-muted-foreground text-[10px]">
                        <summary className="cursor-pointer select-none">API args schema</summary>
                        <pre className="bg-muted/60 mt-1 overflow-x-auto rounded p-2">
                          {JSON.stringify(apiMeta.args_schema, null, 2)}
                        </pre>
                      </details>
                    ) : null}
                    <div className="flex flex-wrap gap-2">
                      {tool.apiToolId ? (
                        <Button type="button" size="sm" onClick={() => openRunModal(tool)}>
                          Run as job
                        </Button>
                      ) : null}
                      {tool.uiNav && onNavigate ? (
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          onClick={() => onNavigate(tool.uiNav!)}
                        >
                          Open in UI →
                        </Button>
                      ) : null}
                      {tool.docsPath ? (
                        <Button type="button" size="sm" variant="ghost" asChild>
                          <a href={tool.docsPath} target="_blank" rel="noreferrer noopener">
                            Docs
                          </a>
                        </Button>
                      ) : null}
                    </div>
                  </CardContent>
                </Card>
              )
            })}
          </div>
        </div>
      ))}

      <Modal
        open={runOpen}
        onClose={() => {
          setRunOpen(false)
          setRunDraft(null)
          setArgsError(null)
        }}
        title={runDraft ? `Run ${runDraft.title} as job` : 'Run tool'}
        size="md"
      >
        {runDraft ? (
          <div className="flex flex-col gap-3 text-xs">
            <p className="text-muted-foreground leading-relaxed">
              Paths in <code>args</code> must be relative to the UI data directory. Artifacts land under{' '}
              <code>artifacts/jobs/&lt;job-id&gt;/</code>.
            </p>
            <div className="flex flex-col gap-1">
              <Label htmlFor="tool-job-id">Job ID (optional)</Label>
              <Input
                id="tool-job-id"
                value={runDraft.jobId}
                onChange={(e) => setRunDraft({ ...runDraft, jobId: e.target.value })}
                placeholder="auto-generated UUID if empty"
              />
              {jobIdError ? <p className="text-destructive text-[11px]">{jobIdError}</p> : null}
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="tool-args">Args (JSON)</Label>
              <textarea
                id="tool-args"
                className="border-input bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring min-h-[140px] w-full rounded-md border px-3 py-2 font-mono text-[11px] focus-visible:ring-2 focus-visible:outline-none"
                value={runDraft.argsText}
                onChange={(e) => setRunDraft({ ...runDraft, argsText: e.target.value })}
              />
              {argsError ? <p className="text-destructive text-[11px]">{argsError}</p> : null}
            </div>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" size="sm" onClick={() => setRunOpen(false)}>
                Cancel
              </Button>
              <Button type="button" size="sm" onClick={onSubmitRun} disabled={!!jobIdError}>
                Start job
              </Button>
            </div>
          </div>
        ) : null}
      </Modal>
    </section>
  )
}
