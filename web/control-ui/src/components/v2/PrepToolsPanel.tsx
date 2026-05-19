import { useMemo, useState } from 'react'

import { listTools, runTool, type ToolMeta } from '@/api/v2'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Modal } from '@/components/ui/modal'
import { PREP_TOOLS, prepExampleArgs, type PrepToolEntry } from '@/lib/prepTools'
import { useToast } from '@/lib/toast'

export type PrepToolsPanelProps = {
  bearer?: string
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  compact?: boolean
}

export function PrepToolsPanel({ bearer, run, compact }: PrepToolsPanelProps) {
  const { toast } = useToast()
  const [apiTools, setApiTools] = useState<ToolMeta[]>([])
  const [runDraft, setRunDraft] = useState<{ entry: PrepToolEntry; argsText: string; jobId: string } | null>(
    null,
  )
  const [argsError, setArgsError] = useState<string | null>(null)

  const apiById = useMemo(() => new Map(apiTools.map((t) => [t.id, t])), [apiTools])

  const ensureTools = () => {
    if (apiTools.length > 0) return
    void listTools({ bearer }).then((r) => setApiTools(r.tools ?? []))
  }

  const openRun = (entry: PrepToolEntry) => {
    ensureTools()
    setArgsError(null)
    setRunDraft({
      entry,
      jobId: '',
      argsText: prepExampleArgs(apiById.get(entry.apiToolId), entry),
    })
  }

  const onSubmit = () => {
    if (!runDraft) return
    let args: Record<string, unknown>
    try {
      args = JSON.parse(runDraft.argsText) as Record<string, unknown>
      if (args === null || typeof args !== 'object' || Array.isArray(args)) {
        throw new Error('args must be a JSON object')
      }
    } catch (err) {
      setArgsError(String(err instanceof Error ? err.message : err))
      return
    }
    void run(async () => {
      await runTool(
        runDraft.entry.apiToolId,
        { id: runDraft.jobId.trim() || undefined, args },
        { bearer },
      )
      setRunDraft(null)
      toast(`${runDraft.entry.title} job started`, 'success')
    })
  }

  return (
    <section className={compact ? 'flex flex-col gap-2' : 'flex flex-col gap-3'}>
      <div>
        <h3 className="text-sm font-medium">Scenario prep</h3>
        <p className="text-muted-foreground text-xs leading-relaxed">
          PCAP → XML and CSV index utilities. Outputs land under{' '}
          <code className="text-[11px]">artifacts/jobs/&lt;id&gt;/</code> when run as jobs.
        </p>
      </div>
      <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
        {PREP_TOOLS.map((tool) => (
          <Card key={tool.id} className="border-border/80">
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium">{tool.title}</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-2 pt-0">
              <p className="text-muted-foreground text-xs">{tool.summary}</p>
              <pre className="bg-muted overflow-x-auto rounded-md p-2 text-[10px]">{tool.cli}</pre>
              <div className="flex flex-wrap gap-2">
                <Button type="button" size="sm" onClick={() => openRun(tool)}>
                  Run as job
                </Button>
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
        ))}
      </div>

      <Modal open={runDraft !== null} onClose={() => setRunDraft(null)} title={runDraft?.entry.title ?? 'Prep tool'}>
        {runDraft ? (
          <div className="flex flex-col gap-3 text-xs">
            <div className="flex flex-col gap-1">
              <Label htmlFor="prep-job-id">Job ID (optional)</Label>
              <Input
                id="prep-job-id"
                value={runDraft.jobId}
                onChange={(e) => setRunDraft({ ...runDraft, jobId: e.target.value })}
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="prep-args">Args (JSON, paths relative to data-dir)</Label>
              <textarea
                id="prep-args"
                className="border-input bg-background min-h-[120px] w-full rounded-md border px-3 py-2 font-mono text-[11px]"
                value={runDraft.argsText}
                onChange={(e) => setRunDraft({ ...runDraft, argsText: e.target.value })}
              />
              {argsError ? <p className="text-destructive">{argsError}</p> : null}
            </div>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" size="sm" onClick={() => setRunDraft(null)}>
                Cancel
              </Button>
              <Button type="button" size="sm" onClick={onSubmit}>
                Start job
              </Button>
            </div>
          </div>
        ) : null}
      </Modal>
    </section>
  )
}
