import { useEffect, useMemo, useState } from 'react'

import { getLoadTestSchema, runLoadTest, type LoadTestRunBody } from '@/api/v2'
import { JobMonitorPanel } from '@/components/v2/JobMonitorPanel'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  defaultLoadTestDraft,
  draftToLoadTestRequest,
  INVITE_MEDIA_SCENARIOS,
  loadTestCliPreview,
  parseDirector,
  type LoadTestDraft,
} from '@/lib/loadTestWizard'
import {
  deleteLoadTestPreset,
  listLoadTestPresets,
  saveLoadTestPreset,
  type LoadTestPreset,
} from '@/lib/loadTestPresets'
import { validateJobID } from '@/lib/jobId'
import { setHashRoute } from '@/lib/routing'
import { useToast } from '@/lib/toast'

export type LoadTestV2Props = {
  bearer?: string
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  onNavigate?: (target: 'jobs' | 'reports', jobId?: string) => void
  initialJobId?: string | null
}

export function LoadTestV2({ bearer, run, onNavigate, initialJobId }: LoadTestV2Props) {
  const { toast } = useToast()
  const [draft, setDraft] = useState<LoadTestDraft>(() => defaultLoadTestDraft())
  const [activeJobId, setActiveJobId] = useState<string | null>(initialJobId ?? null)
  const [presets, setPresets] = useState<LoadTestPreset[]>(() => listLoadTestPresets())
  const [presetName, setPresetName] = useState('')
  const [schemaHint, setSchemaHint] = useState<string | null>(null)

  useEffect(() => {
    if (initialJobId) setActiveJobId(initialJobId)
  }, [initialJobId])

  useEffect(() => {
    void getLoadTestSchema({ bearer })
      .then((s) => {
        const d = s.defaults as Record<string, unknown>
        setDraft((prev) => ({
          ...prev,
          scenario_id: typeof d.scenario_id === 'string' ? d.scenario_id : prev.scenario_id,
          total_calls: typeof d.total_calls === 'number' ? d.total_calls : prev.total_calls,
          rate: typeof d.rate === 'number' ? d.rate : prev.rate,
          max_concurrent: typeof d.max_concurrent === 'number' ? d.max_concurrent : prev.max_concurrent,
          health_enabled: typeof d.health_enabled === 'boolean' ? d.health_enabled : prev.health_enabled,
          health_min_success_ratio:
            typeof d.health_min_success_ratio === 'number' ? d.health_min_success_ratio : prev.health_min_success_ratio,
          health_max_failed_calls:
            typeof d.health_max_failed_calls === 'number' ? d.health_max_failed_calls : prev.health_max_failed_calls,
        }))
        setSchemaHint(typeof d.soak === 'string' ? d.soak : null)
      })
      .catch(() => {})
  }, [bearer])

  const jobIdError = useMemo(() => validateJobID(draft.job_id), [draft.job_id])
  const directorError = useMemo(() => (parseDirector(draft.director) ? null : 'Invalid director (host:port)'), [draft.director])
  const cliPreview = useMemo(() => loadTestCliPreview(draft), [draft])

  const set = <K extends keyof LoadTestDraft>(key: K, value: LoadTestDraft[K]) => {
    setDraft((d) => ({ ...d, [key]: value }))
  }

  const onStart = () => {
    if (directorError || jobIdError) return
    void run(async () => {
      const out = await runLoadTest(draftToLoadTestRequest(draft) as LoadTestRunBody, { bearer })
      setActiveJobId(out.job.id)
      setHashRoute('load', { jobId: out.job.id })
      toast(`Load test running in background (${out.job.id.slice(0, 8)}…)`, 'success')
    })
  }

  const onSavePreset = () => {
    const all = saveLoadTestPreset(presetName, draft)
    setPresets(all)
    setPresetName('')
    toast('Preset saved', 'success')
  }

  return (
    <section className="flex max-w-3xl flex-col gap-4">
      <header className="flex flex-col gap-1">
        <h2 className="text-base font-semibold tracking-tight">Load test</h2>
        <p className="text-muted-foreground text-xs leading-relaxed">
          Background job via <code className="text-[11px]">POST /api/v2/load-test/run</code>. Monitor below or in{' '}
          <button type="button" className="text-primary hover:underline" onClick={() => onNavigate?.('jobs')}>
            Jobs
          </button>
          .
          {schemaHint ? <span className="mt-1 block text-[10px]">{schemaHint}</span> : null}
        </p>
      </header>

      {activeJobId ? (
        <JobMonitorPanel
          jobId={activeJobId}
          bearer={bearer}
          onOpenReports={(id) => onNavigate?.('reports', id)}
          onFinished={(job) => {
            if (job.status === 'succeeded') toast('Load test completed', 'success')
            if (job.status === 'failed') toast('Load test failed — see summary', 'error')
          }}
        />
      ) : null}

      <div className="border-border flex flex-wrap items-end gap-2 rounded-lg border p-3">
        <div className="flex min-w-[10rem] flex-1 flex-col gap-1">
          <Label className="text-[10px]">Presets</Label>
          <select
            className="border-input bg-background h-8 rounded-md border px-2 text-xs"
            defaultValue=""
            onChange={(e) => {
              const p = presets.find((x) => x.name === e.target.value)
              if (p) setDraft({ ...p.draft })
              e.target.value = ''
            }}
          >
            <option value="">Load preset…</option>
            {presets.map((p) => (
              <option key={p.name} value={p.name}>
                {p.name}
              </option>
            ))}
          </select>
        </div>
        <Input
          value={presetName}
          onChange={(e) => setPresetName(e.target.value)}
          placeholder="Preset name"
          className="h-8 max-w-[10rem] text-xs"
        />
        <Button type="button" size="sm" variant="outline" onClick={onSavePreset} disabled={!presetName.trim()}>
          Save preset
        </Button>
        {presets.length ? (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={() => {
              const n = presets[0]?.name
              if (n && window.confirm(`Delete preset "${n}"?`)) setPresets(deleteLoadTestPreset(n))
            }}
          >
            Delete latest
          </Button>
        ) : null}
      </div>

      <div className="border-border grid grid-cols-1 gap-4 rounded-lg border p-4 md:grid-cols-2">
        <div className="flex flex-col gap-1 md:col-span-2">
          <Label htmlFor="lt-job-id">Job ID (optional)</Label>
          <Input
            id="lt-job-id"
            value={draft.job_id}
            onChange={(e) => set('job_id', e.target.value)}
            placeholder="auto UUID"
            className="font-mono text-xs"
          />
          {jobIdError ? <p className="text-destructive text-[11px]">{jobIdError}</p> : null}
        </div>

        <div className="flex flex-col gap-1 md:col-span-2">
          <Label htmlFor="lt-director">Director (SBC) host:port</Label>
          <Input
            id="lt-director"
            value={draft.director}
            onChange={(e) => set('director', e.target.value)}
            placeholder="10.0.0.1:5060"
            className="font-mono text-xs"
          />
          {directorError ? <p className="text-destructive text-[11px]">{directorError}</p> : null}
        </div>

        <div className="flex flex-col gap-1 md:col-span-2">
          <Label htmlFor="lt-scenario">Scenario (built-in)</Label>
          <select
            id="lt-scenario"
            className="border-input bg-background h-9 rounded-md border px-2 text-xs"
            value={draft.scenario_id}
            onChange={(e) => set('scenario_id', e.target.value)}
          >
            {INVITE_MEDIA_SCENARIOS.map((s) => (
              <option key={s.id} value={s.id}>
                {s.label}
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-2 md:col-span-2">
          <label className="flex items-center gap-2 text-xs font-medium">
            <input
              type="checkbox"
              checked={draft.soak_unlimited}
              onChange={(e) => set('soak_unlimited', e.target.checked)}
            />
            Soak mode — unlimited calls until Stop (<code>total_calls=0</code>)
          </label>
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="lt-calls">Total calls (-m)</Label>
          <Input
            id="lt-calls"
            type="number"
            min={1}
            disabled={draft.soak_unlimited}
            value={draft.total_calls}
            onChange={(e) => set('total_calls', Math.max(1, parseInt(e.target.value, 10) || 1))}
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="lt-cps">Rate / CPS (-r)</Label>
          <Input
            id="lt-cps"
            type="number"
            min={0.1}
            step={0.1}
            value={draft.rate}
            onChange={(e) => set('rate', Math.max(0.1, parseFloat(e.target.value) || 1))}
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="lt-conc">Max concurrent (-l)</Label>
          <Input
            id="lt-conc"
            type="number"
            min={1}
            value={draft.max_concurrent}
            onChange={(e) => set('max_concurrent', Math.max(1, parseInt(e.target.value, 10) || 1))}
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="lt-timeout">Run timeout ms (0 = none)</Label>
          <Input
            id="lt-timeout"
            type="number"
            min={0}
            value={draft.run_timeout_ms}
            onChange={(e) => set('run_timeout_ms', Math.max(0, parseInt(e.target.value, 10) || 0))}
          />
        </div>

        <div className="flex flex-col gap-1 md:col-span-2">
          <Label htmlFor="lt-from">SIP From (-sip_from)</Label>
          <Input
            id="lt-from"
            value={draft.sip_from}
            onChange={(e) => set('sip_from', e.target.value)}
            placeholder="sip:+E164@trunk.example"
            className="font-mono text-xs"
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="lt-pai">PAI (-sip_pai)</Label>
          <Input
            id="lt-pai"
            value={draft.sip_pai}
            onChange={(e) => set('sip_pai', e.target.value)}
            className="font-mono text-xs"
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="lt-provider">Provider (-sip_provider)</Label>
          <Input
            id="lt-provider"
            value={draft.sip_provider}
            onChange={(e) => set('sip_provider', e.target.value)}
            className="font-mono text-xs"
          />
        </div>

        <div className="flex flex-col gap-2 md:col-span-2">
          <label className="flex items-center gap-2 text-xs">
            <input type="checkbox" checked={draft.record_wav} onChange={(e) => set('record_wav', e.target.checked)} />
            Record received RTP to WAV (per call)
          </label>
          {draft.record_wav ? (
            <label className="text-muted-foreground ml-5 flex items-center gap-2 text-xs">
              <input
                type="checkbox"
                checked={draft.record_wav_duplex}
                onChange={(e) => set('record_wav_duplex', e.target.checked)}
              />
              Stereo duplex (L=sent, R=received)
            </label>
          ) : null}
        </div>

        <div className="flex flex-col gap-2 md:col-span-2">
          <label className="flex items-center gap-2 text-xs font-medium">
            <input
              type="checkbox"
              checked={draft.health_enabled}
              onChange={(e) => set('health_enabled', e.target.checked)}
            />
            Health gates (exit code 2 on failure)
          </label>
          {draft.health_enabled ? (
            <div className="ml-5 grid grid-cols-1 gap-2 md:grid-cols-2">
              <div className="flex flex-col gap-1">
                <Label htmlFor="lt-health-ratio">Min success ratio</Label>
                <Input
                  id="lt-health-ratio"
                  type="number"
                  min={0}
                  max={1}
                  step={0.01}
                  value={draft.health_min_success_ratio}
                  onChange={(e) => set('health_min_success_ratio', parseFloat(e.target.value) || 0)}
                />
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor="lt-health-fail">Max failed calls</Label>
                <Input
                  id="lt-health-fail"
                  type="number"
                  min={0}
                  value={draft.health_max_failed_calls}
                  onChange={(e) => set('health_max_failed_calls', Math.max(0, parseInt(e.target.value, 10) || 0))}
                />
              </div>
            </div>
          ) : null}
        </div>
      </div>

      <div className="flex flex-wrap gap-2">
        <Button type="button" onClick={onStart} disabled={!!directorError || !!jobIdError}>
          Start load test job
        </Button>
        <Button type="button" variant="outline" onClick={() => setDraft(defaultLoadTestDraft())}>
          Reset form
        </Button>
        {activeJobId ? (
          <Button type="button" variant="ghost" onClick={() => onNavigate?.('jobs', activeJobId)}>
            Open in Jobs
          </Button>
        ) : null}
      </div>

      <details className="text-muted-foreground text-xs">
        <summary className="cursor-pointer select-none">Equivalent CLI (reference)</summary>
        <pre className="bg-muted mt-2 overflow-x-auto rounded-md p-3 text-[10px] leading-snug">{cliPreview}</pre>
      </details>
    </section>
  )
}
