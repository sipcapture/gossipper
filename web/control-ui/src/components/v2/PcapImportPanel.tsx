import { useEffect, useState } from 'react'

import { createScenarioV2, listJobs, type Job } from '@/api/v2'
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
  const [xml, setXml] = useState('')

  useEffect(() => {
    void listJobs({ bearer }, 50).then((r) => {
      setJobs((r.jobs ?? []).filter((j) => j.profile_kind === 'tool' && j.profile_id === 'pcap2scenario'))
    })
  }, [bearer])

  const onImport = () => {
    const id = scenarioId.trim()
    if (!id || !xml.trim()) return
    void run(async () => {
      await createScenarioV2({ id, name: id, role: 'uac' }, xml, { bearer })
      toast(`Scenario ${id} imported`, 'success')
      setScenarioId('')
      setXml('')
      onImported?.()
    })
  }

  return (
    <section className="border-border flex flex-col gap-3 rounded-lg border p-3">
      <div>
        <h3 className="text-sm font-medium">Import from pcap2scenario</h3>
        <p className="text-muted-foreground text-xs">
          After a <code>pcap2scenario</code> tool job, copy UAC/UAS XML from{' '}
          <code>artifacts/jobs/&lt;id&gt;/…</code> into the textarea and save to the scenario library.
        </p>
      </div>
      {jobs.length > 0 ? (
        <ul className="text-muted-foreground space-y-1 font-mono text-[11px]">
          {jobs.slice(0, 5).map((j) => (
            <li key={j.id}>
              [{j.status}] {j.id.slice(0, 8)}… — {j.args_json?.slice(0, 80)}
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-muted-foreground text-xs">No pcap2scenario jobs yet — run one from Prep below.</p>
      )}
      <div className="grid grid-cols-1 gap-2">
        <div>
          <Label className="text-[10px]">New scenario ID</Label>
          <Input value={scenarioId} onChange={(e) => setScenarioId(e.target.value)} className="mt-1 font-mono text-xs" />
        </div>
        <div>
          <Label className="text-[10px]">XML body</Label>
          <textarea
            className="border-input bg-background mt-1 min-h-[120px] w-full rounded-md border px-2 py-1 font-mono text-[11px]"
            value={xml}
            onChange={(e) => setXml(e.target.value)}
            placeholder="<scenario>…</scenario>"
          />
        </div>
        <Button type="button" size="sm" className="self-start" onClick={onImport} disabled={!scenarioId.trim() || !xml.trim()}>
          Save to library
        </Button>
      </div>
    </section>
  )
}
