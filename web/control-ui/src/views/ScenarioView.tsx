import type { ScenarioGetResponse } from '@/api/gossipper'
import { PRESET_OPTIONS_CLIENT, PRESET_OPTIONS_SERVER } from '@/api/presets'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'

type Props = {
  busy: boolean
  scenarioMeta: ScenarioGetResponse | null
  scenarioXml: string
  onScenarioXml: (v: string) => void
  builtin: boolean
  onLoad: () => void
  onSaveFile: () => void
  onSaveApply: () => void
  onApply: () => void
}

export function ScenarioView({
  busy,
  scenarioMeta,
  scenarioXml,
  onScenarioXml,
  builtin,
  onLoad,
  onSaveFile,
  onSaveApply,
  onApply,
}: Props) {
  const xmlTrim = scenarioXml.trim()
  const hasScenarioFile = Boolean(scenarioMeta?.scenario_file?.trim())
  const canSaveToDisk = !builtin && xmlTrim !== ''
  const canApply = xmlTrim !== '' || (hasScenarioFile && !builtin)

  return (
    <div className="flex flex-col gap-4">
      <div className="border-border flex flex-wrap gap-2 border-b pb-3">
        <Button type="button" size="sm" variant="outline" disabled={busy} onClick={onLoad}>
          Load from server
        </Button>
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={busy || !canSaveToDisk}
          title={!canSaveToDisk ? (builtin ? 'Built-in scenario' : 'Editor is empty') : undefined}
          onClick={onSaveFile}
        >
          Write to file
        </Button>
        <Button
          type="button"
          size="sm"
          variant="secondary"
          disabled={busy || !canSaveToDisk}
          title={!canSaveToDisk ? (builtin ? 'Built-in scenario' : 'Editor is empty') : undefined}
          onClick={onSaveApply}
        >
          Write and apply
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={busy || !canApply}
          title={
            !canApply
              ? builtin
                ? 'Built-in: GET has no XML — paste XML or use a preset'
                : 'Empty editor and no -sf scenario file on server'
              : xmlTrim === '' && hasScenarioFile
                ? 'Re-read XML from -sf file on server and hot-reload'
                : undefined
          }
          onClick={onApply}
        >
          Apply (hot reload)
        </Button>
      </div>

      {scenarioMeta ? (
        <p className="text-muted-foreground font-mono text-[11px]">
          file: <span className="text-foreground/90">{scenarioMeta.scenario_file || '—'}</span> · scenario:{' '}
          <span className="text-foreground/90">{scenarioMeta.scenario_name || '—'}</span>
          {builtin ? <span className="text-warning ml-2">(built-in — write disabled)</span> : null}
        </p>
      ) : null}
      {builtin ? (
        <p className="text-muted-foreground max-w-3xl text-[11px] leading-relaxed">
          Built-in scenarios are not returned as XML on <code className="text-foreground/80">GET /scenario</code>. Use
          a preset or paste XML, then Apply — or run with <code className="text-foreground/80">-sf</code> for file-backed
          hot reload.
        </p>
      ) : null}

      <div className="border-border flex min-h-0 min-w-0 flex-1 flex-col border">
        <Tabs defaultValue="editor" className="flex flex-1 flex-col">
          <TabsList variant="line" className="bg-muted/20 shrink-0 rounded-none border-b px-2">
            <TabsTrigger value="editor">XML editor</TabsTrigger>
            <TabsTrigger value="preset-uac">UAC preset</TabsTrigger>
            <TabsTrigger value="preset-uas">UAS preset</TabsTrigger>
          </TabsList>
          <TabsContent value="editor" className="mt-0 flex-1 p-0 data-[state=inactive]:hidden">
            <Label htmlFor="scxml" className="sr-only">
              Scenario XML
            </Label>
            <Textarea
              id="scxml"
              className="font-mono min-h-[min(520px,60vh)] w-full resize-y rounded-none border-0 text-[11px] leading-relaxed focus-visible:ring-0"
              spellCheck={false}
              value={scenarioXml}
              onChange={(e) => onScenarioXml(e.target.value)}
            />
          </TabsContent>
          <TabsContent value="preset-uac" className="text-muted-foreground mt-0 space-y-2 p-3 text-xs data-[state=inactive]:hidden">
            <pre className="border-input bg-muted/20 max-h-64 overflow-auto border p-2 font-mono text-[11px] leading-snug whitespace-pre-wrap">
              {PRESET_OPTIONS_CLIENT}
            </pre>
            <Button type="button" size="sm" variant="secondary" onClick={() => onScenarioXml(PRESET_OPTIONS_CLIENT)}>
              Insert into editor
            </Button>
          </TabsContent>
          <TabsContent value="preset-uas" className="text-muted-foreground mt-0 space-y-2 p-3 text-xs data-[state=inactive]:hidden">
            <pre className="border-input bg-muted/20 max-h-64 overflow-auto border p-2 font-mono text-[11px] leading-snug whitespace-pre-wrap">
              {PRESET_OPTIONS_SERVER}
            </pre>
            <Button type="button" size="sm" variant="secondary" onClick={() => onScenarioXml(PRESET_OPTIONS_SERVER)}>
              Insert into editor
            </Button>
          </TabsContent>
        </Tabs>
      </div>

      <p className="text-muted-foreground max-w-3xl text-[11px] leading-relaxed">
        <code className="text-foreground/80">PUT /scenario</code> writes the file; <code className="text-foreground/80">?apply=true</code> or{' '}
        <code className="text-foreground/80">POST /scenario/apply</code> with <code className="text-foreground/80">Content-Type: application/xml</code>{' '}
        hot-reloads for new calls. With <code className="text-foreground/80">-sf</code>, an empty-body{' '}
        <code className="text-foreground/80">POST /scenario/apply</code> re-reads that file. In-flight dialogs keep the
        XML they started with.
      </p>
    </div>
  )
}
