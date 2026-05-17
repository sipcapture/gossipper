import { useCallback, useEffect, useMemo, useState } from 'react'

import {
  createServer,
  deleteServer,
  listScenarios,
  listServers,
  startServerProfile,
  stopServerProfile,
  updateServer,
  type ScenarioMeta,
  type ServerProfile,
  type TransportSpec,
} from '@/api/v2'
import { Button } from '@/components/ui/button'
import { DataTable, type Column } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Modal } from '@/components/ui/modal'
import { Textarea } from '@/components/ui/textarea'
import { RuntimeBadge } from '@/components/v2/RuntimeBadge'
import { TransportListEditor } from '@/components/v2/TransportListEditor'

const emptyDraft = (): ServerProfile => ({
  id: '',
  name: '',
  transports: [{ transport: 'u1', local_ip: '0.0.0.0', local_port: 5060, enabled: true }],
})

export type ServersV2Props = {
  bearer?: string
  busy: boolean
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  errorText?: string | null
}

export function ServersV2({ bearer, busy, run, errorText }: ServersV2Props) {
  const [rows, setRows] = useState<ServerProfile[]>([])
  const [scenarios, setScenarios] = useState<ScenarioMeta[]>([])
  const [draft, setDraft] = useState<ServerProfile | null>(null)
  const [createMode, setCreateMode] = useState(false)

  const refresh = useCallback(async () => {
    const r = await listServers({ bearer })
    setRows(r.servers ?? [])
    const s = await listScenarios({ bearer })
    setScenarios(s.scenarios ?? [])
  }, [bearer])

  // Refresh only the rows (cheap) without touching the run/busy spinner.
  // Used by the auto-poll loop so the status column stays live without
  // grey-ing out the table every 3 s.
  const refreshRowsOnly = useCallback(async () => {
    try {
      const r = await listServers({ bearer })
      setRows(r.servers ?? [])
    } catch (err) {
      console.warn('refresh servers:', err)
    }
  }, [bearer])

  useEffect(() => {
    void run(() => refresh())
  }, [run, refresh])

  useEffect(() => {
    const id = window.setInterval(() => {
      void refreshRowsOnly()
    }, 3000)
    return () => window.clearInterval(id)
  }, [refreshRowsOnly])

  const onCreate = () => {
    setDraft(emptyDraft())
    setCreateMode(true)
  }

  const onEdit = (row: ServerProfile) => {
    setDraft({ ...row, transports: row.transports ?? [] })
    setCreateMode(false)
  }

  const onClose = () => setDraft(null)

  const onSave = () => {
    if (!draft) return
    void run(async () => {
      if (createMode) {
        await createServer(draft, { bearer })
      } else {
        await updateServer(draft.id, draft, { bearer })
      }
      setDraft(null)
      await refresh()
    })
  }

  const onDelete = (row: ServerProfile) => {
    if (!window.confirm(`Delete server profile "${row.id}"?`)) return
    void run(async () => {
      await deleteServer(row.id, { bearer })
      await refresh()
    })
  }

  const onStart = (row: ServerProfile) => {
    void run(async () => {
      try {
        await startServerProfile(row.id, { bearer })
      } catch (err) {
        console.warn('start:', err)
      } finally {
        await refresh()
      }
    })
  }
  const onStop = (row: ServerProfile) => {
    void run(async () => {
      try {
        await stopServerProfile(row.id, { bearer })
      } catch (err) {
        // "no running job" / 409 built-in is fine — surface but don't break refresh.
        console.warn('stop:', err)
      } finally {
        await refresh()
      }
    })
  }

  const columns: Column<ServerProfile>[] = useMemo(
    () => [
      { key: 'id', header: 'ID', render: (r) => <code className="text-xs">{r.id}</code> },
      { key: 'name', header: 'Name', render: (r) => r.name },
      {
        key: 'status',
        header: 'Status',
        render: (r) => <RuntimeBadge runtime={r.runtime} />,
      },
      {
        key: 'transports',
        header: 'Transports',
        render: (r) =>
          (r.transports ?? []).map((t, i) => (
            <span
              key={i}
              className="bg-muted text-foreground/80 mr-1 inline-block rounded px-1.5 py-0.5 font-mono text-[10px]"
              title={`${t.local_ip}:${t.local_port} (${t.enabled ? 'on' : 'off'})`}
            >
              {t.transport}/{t.local_port ?? '-'}
              {t.enabled ? '' : '·off'}
            </span>
          )),
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
        render: (r) => {
          const builtin = r.source === 'built-in'
          const title = builtin
            ? 'Built-in profile (seeded from management config) — runs inside the master process. Restart the service to change bindings.'
            : undefined
          return (
            <div className="flex justify-end gap-1">
              <Button
                type="button"
                variant="outline"
                size="xs"
                onClick={() => onStart(r)}
                disabled={busy || builtin}
                title={title}
              >
                Start
              </Button>
              <Button
                type="button"
                variant="outline"
                size="xs"
                onClick={() => onStop(r)}
                disabled={busy || builtin}
                title={title}
              >
                Stop
              </Button>
              <Button type="button" variant="outline" size="xs" onClick={() => onEdit(r)}>
                Edit
              </Button>
              <Button
                type="button"
                variant="destructive"
                size="xs"
                onClick={() => onDelete(r)}
                disabled={builtin}
                title={title}
              >
                Delete
              </Button>
            </div>
          )
        },
      },
    ],
    [busy, onDelete, onEdit, onStart, onStop],
  )

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold">Server profiles (UAS)</h2>
          <p className="text-muted-foreground text-xs">
            Manage SIP server profiles stored in <code>profiles/servers/&lt;id&gt;.json</code>.
            Use the Jobs page to actually start an engine from a profile.
          </p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" size="sm" onClick={() => void run(() => refresh())}>
            Refresh
          </Button>
          <Button type="button" size="sm" onClick={onCreate}>
            + New server profile
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
        empty="No server profiles yet — create one above."
      />

      <Modal
        open={draft !== null}
        onClose={onClose}
        size="lg"
        title={createMode ? 'New server profile' : `Edit server profile · ${draft?.id ?? ''}`}
        description={createMode ? 'IDs must be ASCII [a-zA-Z0-9._-], up to 64 chars.' : undefined}
        footer={
          <>
            <Button type="button" variant="outline" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button type="button" size="sm" onClick={onSave} disabled={busy}>
              {createMode ? 'Create' : 'Save'}
            </Button>
          </>
        }
      >
        {draft ? (
          <ServerForm
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

function ServerForm({
  value,
  onChange,
  scenarios,
  disableId,
}: {
  value: ServerProfile
  onChange: (v: ServerProfile) => void
  scenarios: ScenarioMeta[]
  disableId: boolean
}) {
  const set = <K extends keyof ServerProfile>(k: K, v: ServerProfile[K]) => onChange({ ...value, [k]: v })
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
            placeholder="primary"
          />
        </div>
        <div>
          <Label className="text-xs">Name</Label>
          <Input
            value={value.name}
            onChange={(e) => set('name', e.target.value)}
            className="mt-1"
            placeholder="Primary UAS"
          />
        </div>
      </div>
      <div>
        <Label className="text-xs">Description</Label>
        <Input
          value={value.description ?? ''}
          onChange={(e) => set('description', e.target.value)}
          className="mt-1"
        />
      </div>
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
        <div>
          <Label className="text-xs">Default scenario</Label>
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
        <div>
          <Label className="text-xs">Max concurrent</Label>
          <Input
            type="number"
            value={value.max_concurrent ?? 0}
            onChange={(e) => set('max_concurrent', Number(e.target.value) || 0)}
            className="mt-1"
          />
        </div>
      </div>
      <div>
        <Label className="text-xs">Transports / listeners</Label>
        <div className="mt-1">
          <TransportListEditor value={value.transports ?? []} onChange={setTransports} />
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
