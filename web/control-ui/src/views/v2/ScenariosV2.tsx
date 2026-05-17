import { useCallback, useEffect, useMemo, useState } from 'react'

import {
  createScenarioV2,
  deleteScenarioV2,
  getScenarioV2,
  listScenarios,
  updateScenarioV2,
  type ScenarioMeta,
} from '@/api/v2'
import { Button } from '@/components/ui/button'
import { DataTable, type Column } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Modal } from '@/components/ui/modal'
import { Textarea } from '@/components/ui/textarea'

const STARTER_XML = `<?xml version="1.0" encoding="UTF-8"?>
<scenario name="my_scenario">
  <recv request="INVITE" />
  <send>
    <![CDATA[
SIP/2.0 200 OK
[last_Via:]
[last_From:]
[last_To:];tag=[call_number]
[last_Call-ID:]
[last_CSeq:]
Content-Length: 0

    ]]>
  </send>
</scenario>
`

type Draft = { id: string; name: string; description?: string; role?: string; xml: string }

export type ScenariosV2Props = {
  bearer?: string
  busy: boolean
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  errorText?: string | null
}

export function ScenariosV2({ bearer, busy, run, errorText }: ScenariosV2Props) {
  const [rows, setRows] = useState<ScenarioMeta[]>([])
  const [draft, setDraft] = useState<Draft | null>(null)
  const [createMode, setCreateMode] = useState(false)

  const refresh = useCallback(async () => {
    const r = await listScenarios({ bearer })
    setRows(r.scenarios ?? [])
  }, [bearer])

  useEffect(() => {
    void run(() => refresh())
  }, [run, refresh])

  const onEdit = (row: ScenarioMeta) => {
    void run(async () => {
      const body = await getScenarioV2(row.id, { bearer })
      setDraft({
        id: body.meta.id,
        name: body.meta.name,
        description: body.meta.description,
        role: body.meta.role,
        xml: body.xml,
      })
      setCreateMode(false)
    })
  }

  const onCreate = () => {
    setDraft({ id: '', name: '', xml: STARTER_XML })
    setCreateMode(true)
  }

  const onSave = () => {
    if (!draft) return
    void run(async () => {
      const meta: ScenarioMeta = {
        id: draft.id,
        name: draft.name || draft.id,
        description: draft.description,
        role: draft.role,
      }
      if (createMode) {
        await createScenarioV2(meta, draft.xml, { bearer })
      } else {
        await updateScenarioV2(draft.id, meta, draft.xml, { bearer })
      }
      setDraft(null)
      await refresh()
    })
  }

  const onDelete = (row: ScenarioMeta) => {
    if (!window.confirm(`Delete scenario "${row.id}"?`)) return
    void run(async () => {
      await deleteScenarioV2(row.id, { bearer })
      await refresh()
    })
  }

  const columns: Column<ScenarioMeta>[] = useMemo(
    () => [
      { key: 'id', header: 'ID', render: (r) => <code className="text-xs">{r.id}</code> },
      { key: 'name', header: 'Name', render: (r) => r.name },
      { key: 'role', header: 'Role', render: (r) => r.role ?? '—' },
      {
        key: 'updated',
        header: 'Updated',
        render: (r) => (r.updated_at ? new Date(r.updated_at).toLocaleString() : '—'),
      },
      {
        key: 'actions',
        header: '',
        align: 'right',
        render: (r) => (
          <div className="flex justify-end gap-1">
            <Button type="button" variant="outline" size="xs" onClick={() => onEdit(r)}>
              Edit XML
            </Button>
            <Button type="button" variant="destructive" size="xs" onClick={() => onDelete(r)}>
              Delete
            </Button>
          </div>
        ),
      },
    ],
    [onDelete, onEdit],
  )

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold">Scenarios</h2>
          <p className="text-muted-foreground text-xs">
            SIP XML scenarios. Stored as <code>scenarios/&lt;id&gt;.xml</code> plus a JSON sidecar with metadata.
          </p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" size="sm" onClick={() => void run(() => refresh())}>
            Refresh
          </Button>
          <Button type="button" size="sm" onClick={onCreate}>
            + New scenario
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
        empty="No scenarios yet — create one above."
      />

      <Modal
        open={draft !== null}
        onClose={() => setDraft(null)}
        size="xl"
        title={createMode ? 'New scenario' : `Edit scenario · ${draft?.id ?? ''}`}
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
          <div className="flex h-[70vh] flex-col gap-3">
            <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
              <div>
                <Label className="text-xs">ID</Label>
                <Input
                  value={draft.id}
                  onChange={(e) => setDraft({ ...draft, id: e.target.value })}
                  disabled={!createMode}
                  className="mt-1"
                  placeholder="uas_basic"
                />
              </div>
              <div>
                <Label className="text-xs">Name</Label>
                <Input
                  value={draft.name}
                  onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                  className="mt-1"
                />
              </div>
              <div>
                <Label className="text-xs">Role hint</Label>
                <select
                  value={draft.role ?? ''}
                  onChange={(e) => setDraft({ ...draft, role: e.target.value || undefined })}
                  className="border-input bg-background mt-1 w-full rounded-md border px-2 py-1.5 text-sm"
                >
                  <option value="">(either)</option>
                  <option value="server">server (UAS)</option>
                  <option value="client">client (UAC)</option>
                </select>
              </div>
            </div>
            <div>
              <Label className="text-xs">Description</Label>
              <Input
                value={draft.description ?? ''}
                onChange={(e) => setDraft({ ...draft, description: e.target.value })}
                className="mt-1"
              />
            </div>
            <div className="flex min-h-0 flex-1 flex-col">
              <Label className="text-xs">XML</Label>
              <Textarea
                value={draft.xml}
                onChange={(e) => setDraft({ ...draft, xml: e.target.value })}
                className="mt-1 min-h-0 flex-1 font-mono text-xs"
                spellCheck={false}
              />
            </div>
          </div>
        ) : null}
      </Modal>
    </section>
  )
}
