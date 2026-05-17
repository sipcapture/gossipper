import { useCallback, useEffect, useMemo, useState } from 'react'

import {
  createClient,
  deleteClient,
  listClients,
  listScenarios,
  startClientProfile,
  stopClientProfile,
  updateClient,
  type ClientProfile,
  type ScenarioMeta,
  type TransportSpec,
} from '@/api/v2'
import { Button } from '@/components/ui/button'
import { DataTable, type Column } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Modal } from '@/components/ui/modal'
import { Textarea } from '@/components/ui/textarea'
import { TransportListEditor } from '@/components/v2/TransportListEditor'

const emptyDraft = (): ClientProfile => ({
  id: '',
  name: '',
  transports: [{ transport: 'u1', local_ip: '0.0.0.0', local_port: 0, enabled: true }],
  rate: 1,
  max_concurrent: 100,
})

export type ClientsV2Props = {
  bearer?: string
  busy: boolean
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  errorText?: string | null
}

export function ClientsV2({ bearer, busy, run, errorText }: ClientsV2Props) {
  const [rows, setRows] = useState<ClientProfile[]>([])
  const [scenarios, setScenarios] = useState<ScenarioMeta[]>([])
  const [draft, setDraft] = useState<ClientProfile | null>(null)
  const [createMode, setCreateMode] = useState(false)

  const refresh = useCallback(async () => {
    const r = await listClients({ bearer })
    setRows(r.clients ?? [])
    const s = await listScenarios({ bearer })
    setScenarios(s.scenarios ?? [])
  }, [bearer])

  useEffect(() => {
    void run(() => refresh())
  }, [run, refresh])

  const onSave = () => {
    if (!draft) return
    void run(async () => {
      if (createMode) {
        await createClient(draft, { bearer })
      } else {
        await updateClient(draft.id, draft, { bearer })
      }
      setDraft(null)
      await refresh()
    })
  }

  const onDelete = (row: ClientProfile) => {
    if (!window.confirm(`Delete client profile "${row.id}"?`)) return
    void run(async () => {
      await deleteClient(row.id, { bearer })
      await refresh()
    })
  }
  const onStart = (row: ClientProfile) => {
    void run(async () => {
      await startClientProfile(row.id, { bearer })
    })
  }
  const onStop = (row: ClientProfile) => {
    void run(async () => {
      try {
        await stopClientProfile(row.id, { bearer })
      } catch (err) {
        console.warn('stop:', err)
      }
    })
  }

  const columns: Column<ClientProfile>[] = useMemo(
    () => [
      { key: 'id', header: 'ID', render: (r) => <code className="text-xs">{r.id}</code> },
      { key: 'name', header: 'Name', render: (r) => r.name },
      {
        key: 'target',
        header: 'Target',
        render: (r) =>
          r.remote_ip ? (
            <code className="text-xs">
              {r.remote_ip}:{r.remote_port ?? '-'}
            </code>
          ) : (
            '—'
          ),
      },
      {
        key: 'load',
        header: 'Load',
        render: (r) => (
          <span className="font-mono text-xs">
            {r.rate ?? 0} cps · {r.max_concurrent ?? 0} max
          </span>
        ),
      },
      {
        key: 'scenario',
        header: 'Scenario',
        render: (r) => (r.scenario_ref ? <code className="text-xs">{r.scenario_ref}</code> : '—'),
      },
      {
        key: 'actions',
        header: '',
        align: 'right',
        render: (r) => (
          <div className="flex justify-end gap-1">
            <Button type="button" variant="outline" size="xs" onClick={() => onStart(r)} disabled={busy}>
              Start
            </Button>
            <Button type="button" variant="outline" size="xs" onClick={() => onStop(r)} disabled={busy}>
              Stop
            </Button>
            <Button
              type="button"
              variant="outline"
              size="xs"
              onClick={() => {
                setDraft({ ...r, transports: r.transports ?? [] })
                setCreateMode(false)
              }}
            >
              Edit
            </Button>
            <Button type="button" variant="destructive" size="xs" onClick={() => onDelete(r)}>
              Delete
            </Button>
          </div>
        ),
      },
    ],
    [onDelete],
  )

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold">Client profiles (UAC)</h2>
          <p className="text-muted-foreground text-xs">
            Templates used by Jobs to launch load-test runs. Stored in <code>profiles/clients/&lt;id&gt;.json</code>.
          </p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" size="sm" onClick={() => void run(() => refresh())}>
            Refresh
          </Button>
          <Button
            type="button"
            size="sm"
            onClick={() => {
              setDraft(emptyDraft())
              setCreateMode(true)
            }}
          >
            + New client profile
          </Button>
        </div>
      </div>

      {errorText ? (
        <div className="border-destructive/40 bg-destructive/10 text-destructive rounded-md border px-3 py-2 text-xs">
          {errorText}
        </div>
      ) : null}

      <DataTable
        rows={rows}
        columns={columns}
        rowKey={(r) => r.id}
        loading={busy && rows.length === 0}
        empty="No client profiles yet — create one above."
      />

      <Modal
        open={draft !== null}
        onClose={() => setDraft(null)}
        size="lg"
        title={createMode ? 'New client profile' : `Edit client profile · ${draft?.id ?? ''}`}
        footer={
          <>
            <Button type="button" variant="outline" size="sm" onClick={() => setDraft(null)}>
              Cancel
            </Button>
            <Button type="button" size="sm" onClick={onSave} disabled={busy}>
              {createMode ? 'Create' : 'Save'}
            </Button>
          </>
        }
      >
        {draft ? (
          <ClientForm
            value={draft}
            onChange={setDraft}
            scenarios={scenarios}
            disableId={!createMode}
          />
        ) : null}
      </Modal>
    </section>
  )
}

function ClientForm({
  value,
  onChange,
  scenarios,
  disableId,
}: {
  value: ClientProfile
  onChange: (v: ClientProfile) => void
  scenarios: ScenarioMeta[]
  disableId: boolean
}) {
  const set = <K extends keyof ClientProfile>(k: K, v: ClientProfile[K]) => onChange({ ...value, [k]: v })
  const setTransports = (t: TransportSpec[]) => onChange({ ...value, transports: t })
  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
        <div>
          <Label className="text-xs">ID</Label>
          <Input
            value={value.id}
            onChange={(e) => set('id', e.target.value)}
            disabled={disableId}
            className="mt-1"
            placeholder="stress-uac"
          />
        </div>
        <div>
          <Label className="text-xs">Name</Label>
          <Input
            value={value.name}
            onChange={(e) => set('name', e.target.value)}
            className="mt-1"
            placeholder="Stress UAC"
          />
        </div>
      </div>
      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        <div>
          <Label className="text-xs">Remote IP</Label>
          <Input
            value={value.remote_ip ?? ''}
            onChange={(e) => set('remote_ip', e.target.value)}
            className="mt-1"
            placeholder="127.0.0.1"
          />
        </div>
        <div>
          <Label className="text-xs">Remote port</Label>
          <Input
            type="number"
            value={value.remote_port ?? 0}
            onChange={(e) => set('remote_port', Number(e.target.value) || 0)}
            className="mt-1"
          />
        </div>
        <div>
          <Label className="text-xs">Scenario</Label>
          <select
            value={value.scenario_ref ?? ''}
            onChange={(e) => set('scenario_ref', e.target.value || undefined)}
            className="border-input bg-background mt-1 w-full rounded-md border px-2 py-1.5 text-sm"
          >
            <option value="">(none)</option>
            {scenarios.map((s) => (
              <option key={s.id} value={s.id}>
                {s.id} — {s.name}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        <div>
          <Label className="text-xs">Rate (cps)</Label>
          <Input
            type="number"
            value={value.rate ?? 0}
            onChange={(e) => set('rate', Number(e.target.value) || 0)}
            className="mt-1"
          />
        </div>
        <div>
          <Label className="text-xs">Max concurrent</Label>
          <Input
            type="number"
            value={value.max_concurrent ?? 0}
            onChange={(e) => set('max_concurrent', Number(e.target.value) || 0)}
            className="mt-1"
          />
        </div>
        <div>
          <Label className="text-xs">Duration (ms, 0 = ∞)</Label>
          <Input
            type="number"
            value={value.duration_ms ?? 0}
            onChange={(e) => set('duration_ms', Number(e.target.value) || 0)}
            className="mt-1"
          />
        </div>
      </div>
      <div>
        <Label className="text-xs">Local binds</Label>
        <div className="mt-1">
          <TransportListEditor value={value.transports ?? []} onChange={setTransports} defaultPort={0} />
        </div>
      </div>
      <div>
        <Label className="text-xs">Notes</Label>
        <Textarea
          value={value.notes ?? ''}
          onChange={(e) => set('notes', e.target.value)}
          className="mt-1 min-h-[80px]"
        />
      </div>
    </div>
  )
}
