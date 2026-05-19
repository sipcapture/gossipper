import { useEffect, useState } from 'react'

import { importScenarioFromPCAPJob, listJobs, type Job } from '@/api/v2'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useToast } from '@/lib/toast'

export type PcapImportPanelProps = {
  bearer?: string
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  onImported?: () => void
}

export function PcapImportPanel({ bearer, run, onImported }: PcapImportPanelProps) {
  const { toast } = useToast()
  const [jobs, setJobs] = useState<Job[]>([])
  const [scenarioId, setScenarioId] = useState('')
  const [selectedJob, setSelectedJob] = useState('')

  const refreshJobs = () => {
    void listJobs({ bearer }, 50).then((r) => {
      setJobs(
        (r.jobs ?? []).filter(
          (j) => j.profile_kind === 'tool' && j.profile_id === 'pcap2scenario' && j.status === 'succeeded',
        ),
      )
    })
  }

  useEffect(() => {
    refreshJobs()
  }, [bearer])

  const onAutoImport = (which: 'uac' | 'uas' | 'both') => {
    const jobId = selectedJob.trim()
    const scId = scenarioId.trim()
    if (!jobId || !scId) return
    void run(async () => {
      const out = await importScenarioFromPCAPJob(
        { job_id: jobId, which, scenario_id: scId },
        { bearer },
      )
      toast(`Imported ${out.imported.length} scenario(s) from job ${jobId.slice(0, 8)}…`, 'success')
      onImported?.()
    })
  }

  return (
    <section className="border-border flex flex-col gap-3 rounded-lg border p-3">
      <div>
        <h3 className="text-sm font-medium">Import from pcap2scenario</h3>
        <p className="text-muted-foreground text-xs">
          Auto-import <code>scenario_uac.xml</code> / <code>scenario_uas.xml</code> from a succeeded tool job via{' '}
          <code>POST /api/v2/scenarios/import-from-pcap-job</code>.
        </p>
      </div>
      <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
        <div>
          <Label className="text-[10px]">pcap2scenario job</Label>
          <select
            className="border-input bg-background mt-1 h-8 w-full rounded-md border px-2 text-xs"
            value={selectedJob}
            onChange={(e) => setSelectedJob(e.target.value)}
          >
            <option value="">— select succeeded job —</option>
            {jobs.map((j) => (
              <option key={j.id} value={j.id}>
                {j.id.slice(0, 12)}… · {new Date(j.finished_at ?? j.created_at).toLocaleString()}
              </option>
            ))}
          </select>
        </div>
        <div>
          <Label className="text-[10px]">Base scenario ID (UAC)</Label>
          <Input
            value={scenarioId}
            onChange={(e) => setScenarioId(e.target.value)}
            placeholder="my_call_uac"
            className="mt-1 font-mono text-xs"
          />
          <p className="text-muted-foreground mt-0.5 text-[10px]">UAS defaults to {scenarioId.trim() || '{id}'}_uas</p>
        </div>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button type="button" size="sm" disabled={!selectedJob || !scenarioId.trim()} onClick={() => onAutoImport('uac')}>
          Import UAC
        </Button>
        <Button type="button" size="sm" variant="outline" disabled={!selectedJob || !scenarioId.trim()} onClick={() => onAutoImport('uas')}>
          Import UAS
        </Button>
        <Button type="button" size="sm" variant="secondary" disabled={!selectedJob || !scenarioId.trim()} onClick={() => onAutoImport('both')}>
          Import both
        </Button>
        <Button type="button" size="sm" variant="ghost" onClick={refreshJobs}>
          Refresh jobs
        </Button>
      </div>
      {jobs.length === 0 ? (
        <p className="text-muted-foreground text-xs">No succeeded pcap2scenario jobs — run one from Prep below.</p>
      ) : null}
    </section>
  )
}
