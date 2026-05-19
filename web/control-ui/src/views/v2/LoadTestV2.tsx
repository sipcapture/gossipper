import { useMemo, useState } from 'react'

import { createClient, listClients, startJob, updateClient } from '@/api/v2'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  buildLoadTestEngine,
  defaultLoadTestDraft,
  INVITE_MEDIA_SCENARIOS,
  LOAD_WIZARD_PROFILE_ID,
  loadTestCliPreview,
  parseDirector,
  type LoadTestDraft,
} from '@/lib/loadTestWizard'
import { validateJobID } from '@/lib/jobId'
import { useToast } from '@/lib/toast'

export type LoadTestV2Props = {
  bearer?: string
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  onNavigate?: (target: 'jobs' | 'reports') => void
}

export function LoadTestV2({ bearer, run, onNavigate }: LoadTestV2Props) {
  const { toast } = useToast()
  const [draft, setDraft] = useState<LoadTestDraft>(() => defaultLoadTestDraft())

  const jobIdError = useMemo(() => validateJobID(draft.job_id), [draft.job_id])
  const directorError = useMemo(() => (parseDirector(draft.director) ? null : 'Invalid director (host:port)'), [draft.director])
  const cliPreview = useMemo(() => loadTestCliPreview(draft), [draft])

  const set = <K extends keyof LoadTestDraft>(key: K, value: LoadTestDraft[K]) => {
    setDraft((d) => ({ ...d, [key]: value }))
  }

  const upsertWizardProfile = async () => {
    const dir = parseDirector(draft.director)
    if (!dir) throw new Error('director')
    const profile = {
      id: LOAD_WIZARD_PROFILE_ID,
      name: 'Load test (wizard)',
      description: 'Auto-updated by the Load test wizard before each run.',
      scenario_ref: draft.scenario_id,
      remote_ip: dir.host,
      remote_port: dir.port,
      rate: draft.rate,
      max_concurrent: draft.max_concurrent,
      duration_ms: draft.run_timeout_ms > 0 ? draft.run_timeout_ms : undefined,
      transports: [{ transport: 'u1', local_ip: '0.0.0.0', local_port: 0, enabled: true }],
    }
    const existing = await listClients({ bearer })
    if (existing.clients?.some((c) => c.id === LOAD_WIZARD_PROFILE_ID)) {
      await updateClient(LOAD_WIZARD_PROFILE_ID, profile, { bearer })
    } else {
      await createClient(profile, { bearer })
    }
  }

  const onStart = () => {
    if (directorError || jobIdError) return
    void run(async () => {
      await upsertWizardProfile()
      const job = await startJob(
        {
          id: draft.job_id.trim() || undefined,
          profile_kind: 'client',
          profile_id: LOAD_WIZARD_PROFILE_ID,
          scenario_id: draft.scenario_id,
          record_wav: draft.record_wav || undefined,
          record_wav_duplex: draft.record_wav && draft.record_wav_duplex ? true : undefined,
          engine: buildLoadTestEngine(draft),
        },
        { bearer },
      )
      toast(`Load test job started (${job.id.slice(0, 8)}…)`, 'success')
      onNavigate?.('jobs')
    })
  }

  return (
    <section className="flex max-w-3xl flex-col gap-4">
      <header className="flex flex-col gap-1">
        <h2 className="text-base font-semibold tracking-tight">Load test</h2>
        <p className="text-muted-foreground text-xs leading-relaxed">
          Sipstress-style <strong className="text-foreground/90">invite_media</strong> runs via supervisor jobs:
          director (SBC), call volume, trunk identity, optional WAV capture, and health gates. Results appear under{' '}
          <button type="button" className="text-primary hover:underline" onClick={() => onNavigate?.('jobs')}>
            Jobs
          </button>{' '}
          and{' '}
          <button type="button" className="text-primary hover:underline" onClick={() => onNavigate?.('reports')}>
            Reports
          </button>{' '}
          (<code className="text-[11px]">summary.json</code>, <code className="text-[11px]">report.html</code>).
        </p>
      </header>

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

        <div className="flex flex-col gap-1">
          <Label htmlFor="lt-calls">Total calls (-m)</Label>
          <Input
            id="lt-calls"
            type="number"
            min={1}
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
          <Label htmlFor="lt-provider">Provider (-sip_provider → X-provider)</Label>
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
            Health gates (exit code 2 on failure; summary.json includes findings)
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
                <Label htmlFor="lt-health-fail">Max failed calls (0 = any failure fails)</Label>
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
      </div>

      <details className="text-muted-foreground text-xs">
        <summary className="cursor-pointer select-none">Equivalent CLI (reference)</summary>
        <pre className="bg-muted mt-2 overflow-x-auto rounded-md p-3 text-[10px] leading-snug">{cliPreview}</pre>
      </details>

      <p className="text-muted-foreground text-[11px] leading-relaxed">
        Compare with Python{' '}
        <a
          href="https://github.com/achrafka/sipstress"
          className="text-primary underline-offset-2 hover:underline"
          target="_blank"
          rel="noreferrer noopener"
        >
          sipstress
        </a>
        : Gossipper uses XML scenarios and aggregate summary JSON (not per-call Plotly dashboards). Scenario prep
        utilities live under <strong className="text-foreground/80">Scenarios → Prep</strong>.
      </p>
    </section>
  )
}
