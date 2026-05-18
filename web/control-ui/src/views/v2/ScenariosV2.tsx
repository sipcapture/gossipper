import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import {
  createScenarioV2,
  deleteScenarioHistory,
  deleteScenarioV2,
  forkScenarioHistory,
  getBuiltinScenario,
  getScenarioHistory,
  getScenarioV2,
  listBuiltinScenarios,
  listMedia,
  listScenarioHistory,
  listScenarios,
  updateScenarioV2,
  type BuiltinScenarioMeta,
  type ScenarioHistoryEntry,
  type ScenarioMeta,
} from '@/api/v2'
import { lineDiff, sideBySideDiff, summariseDiff, type DiffLine, type SideBySideRow } from '@/lib/lineDiff'
import { validateMediaRefs } from '@/lib/mediaRefs'
import { Button } from '@/components/ui/button'
import { DataTable, type Column } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Modal } from '@/components/ui/modal'
import { Textarea } from '@/components/ui/textarea'

// slugifyID turns a filename like "My Cool UAC v2.xml" into "my_cool_uac_v2".
// Server uistore allows [a-z0-9_-./] (no slashes here), so we lower-case and
// drop everything else; collapse runs and trim leading/trailing separators.
function slugifyID(name: string): string {
  const stripped = name.replace(/\.[^./]+$/, '')
  return stripped
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, '_')
    .replace(/_+/g, '_')
    .replace(/^[._-]+|[._-]+$/g, '')
}

// validateScenarioXML runs the browser's XML parser and returns null when the
// document is well-formed, or a short error string for inline display. This is
// a quick "did you forget to close a tag" check — the authoritative validation
// still happens server-side in uistore.PutScenario.
function validateScenarioXML(xml: string): string | null {
  const trimmed = xml.trim()
  if (trimmed === '') return 'XML is empty'
  try {
    const dp = new DOMParser()
    const doc = dp.parseFromString(trimmed, 'application/xml')
    const errNode = doc.getElementsByTagName('parsererror')[0]
    if (errNode) {
      const msg = (errNode.textContent ?? 'invalid XML').trim().replace(/\s+/g, ' ')
      return msg.length > 200 ? msg.slice(0, 200) + '…' : msg
    }
    if (!doc.documentElement || doc.documentElement.nodeName === 'parsererror') {
      return 'invalid XML'
    }
    return null
  } catch (e) {
    return String((e as Error).message ?? e)
  }
}

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
  const [roleFilter, setRoleFilter] = useState<'all' | 'server' | 'client' | 'either'>('all')
  const [query, setQuery] = useState('')
  const [dragOver, setDragOver] = useState(false)
  const [history, setHistory] = useState<ScenarioHistoryEntry[] | null>(null)
  const [historyView, setHistoryView] = useState<{ ts: string; xml: string } | null>(null)
  const [historyMode, setHistoryMode] = useState<'diff' | 'side' | 'xml'>('diff')
  const [historyDiffBase, setHistoryDiffBase] = useState<'current' | string>('current')
  const [historyBaseXML, setHistoryBaseXML] = useState('')
  const [forkOpen, setForkOpen] = useState(false)
  const [forkDraft, setForkDraft] = useState({ id: '', name: '' })
  const [builtins, setBuiltins] = useState<BuiltinScenarioMeta[]>([])
  const [builtinPreview, setBuiltinPreview] = useState<{ id: string; xml: string } | null>(null)
  const [wavNames, setWavNames] = useState<Set<string>>(new Set())
  const [pcapNames, setPcapNames] = useState<Set<string>>(new Set())
  const xmlRef = useRef<HTMLTextAreaElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  const xmlError = useMemo(() => (draft ? validateScenarioXML(draft.xml) : null), [draft])
  const mediaWarnings = useMemo(() => {
    if (!draft) return null
    return validateMediaRefs(draft.xml, { wav: wavNames, pcap: pcapNames })
  }, [draft, wavNames, pcapNames])

  const refresh = useCallback(async () => {
    const [r, bi, w, p] = await Promise.all([
      listScenarios({ bearer }),
      listBuiltinScenarios({ bearer }).catch(() => ({ scenarios: [] as BuiltinScenarioMeta[] })),
      listMedia('wav', { bearer }).catch(() => ({ media: [] })),
      listMedia('pcap', { bearer }).catch(() => ({ media: [] })),
    ])
    setRows(r.scenarios ?? [])
    setBuiltins(bi.scenarios ?? [])
    setWavNames(new Set((w.media ?? []).map((m) => m.name)))
    setPcapNames(new Set((p.media ?? []).map((m) => m.name)))
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

  const onUploadFile = (file: File) => {
    void run(async () => {
      const text = await file.text()
      const id = slugifyID(file.name) || 'scenario'
      setDraft({
        id,
        name: file.name.replace(/\.[^./]+$/, ''),
        xml: text,
      })
      setCreateMode(true)
    })
  }

  const onSave = () => {
    if (!draft) return
    if (xmlError) return
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

  const onViewBuiltin = (id: string) => {
    void run(async () => {
      const body = await getBuiltinScenario(id, { bearer })
      setBuiltinPreview({ id, xml: body.xml })
    })
  }

  const insertMediaAlias = (kind: 'wav' | 'pcap', name: string) => {
    if (!draft) return
    const token = `[[media:${kind}/${name}]]`
    const ta = xmlRef.current
    if (ta) {
      const start = ta.selectionStart ?? draft.xml.length
      const end = ta.selectionEnd ?? start
      const next = draft.xml.slice(0, start) + token + draft.xml.slice(end)
      setDraft({ ...draft, xml: next })
      window.requestAnimationFrame(() => {
        ta.focus()
        const pos = start + token.length
        ta.setSelectionRange(pos, pos)
      })
      return
    }
    setDraft({ ...draft, xml: draft.xml + token })
  }

  const onRestoreOverwrite = () => {
    if (!draft || !historyView) return
    if (
      !window.confirm(
        'Restore this snapshot into the editor and overwrite the current XML? You must Save to persist.',
      )
    )
      return
    setDraft({ ...draft, xml: historyView.xml })
    setHistory(null)
    setHistoryView(null)
  }

  const onDelete = (row: ScenarioMeta) => {
    if (!window.confirm(`Delete scenario "${row.id}"?`)) return
    void run(async () => {
      await deleteScenarioV2(row.id, { bearer })
      await refresh()
    })
  }

  const visibleRows = useMemo(() => {
    const q = query.trim().toLowerCase()
    return rows.filter((s) => {
      if (roleFilter !== 'all') {
        const r = (s.role ?? '').toLowerCase()
        const isEither = r === '' || r === 'either' || r === 'any'
        if (roleFilter === 'either' ? !isEither : r !== roleFilter) return false
      }
      if (!q) return true
      return (
        s.id.toLowerCase().includes(q) ||
        (s.name ?? '').toLowerCase().includes(q) ||
        (s.description ?? '').toLowerCase().includes(q)
      )
    })
  }, [rows, roleFilter, query])

  const roleCounts = useMemo(() => {
    const out: Record<string, number> = { all: rows.length, server: 0, client: 0, either: 0 }
    for (const s of rows) {
      const r = (s.role ?? '').toLowerCase()
      if (r === 'server') out.server++
      else if (r === 'client') out.client++
      else out.either++
    }
    return out
  }, [rows])

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

  const onOpenHistory = () => {
    if (!draft || createMode) return
    void run(async () => {
      const r = await listScenarioHistory(draft.id, { bearer })
      setHistory(r.history ?? [])
      setHistoryView(null)
      setHistoryDiffBase('current')
      setHistoryBaseXML(draft.xml)
    })
  }

  // Load the XML for a snapshot chosen as the diff "base" (anything other than
  // the live editor buffer).
  useEffect(() => {
    if (history === null || !draft) return
    if (historyDiffBase === 'current') {
      setHistoryBaseXML(draft.xml)
      return
    }
    let cancelled = false
    void getScenarioHistory(draft.id, historyDiffBase, { bearer })
      .then((body) => {
        if (!cancelled) setHistoryBaseXML(body.xml)
      })
      .catch(() => {
        if (!cancelled) setHistoryBaseXML('')
      })
    return () => {
      cancelled = true
    }
  }, [history, historyDiffBase, draft, bearer])

  const diffOldXML =
    historyDiffBase === 'current' ? (draft?.xml ?? '') : historyBaseXML
  const diffNewXML = historyView?.xml ?? ''
  const diffBaseLabel =
    historyDiffBase === 'current'
      ? 'current editor'
      : new Date(
          history?.find((h) => h.ts === historyDiffBase)?.timestamp ?? '',
        ).toLocaleString() || historyDiffBase

  const onViewHistoryEntry = (ts: string) => {
    if (!draft) return
    void run(async () => {
      const body = await getScenarioHistory(draft.id, ts, { bearer })
      setHistoryView({ ts, xml: body.xml })
    })
  }

  const onRestoreHistoryEntry = () => {
    if (!draft || !historyView) return
    // Loads the archived XML into the draft editor without persisting; the
    // user must hit Save to actually re-snapshot the current version and
    // promote the restored XML to head.
    setDraft({ ...draft, xml: historyView.xml })
    setHistory(null)
    setHistoryView(null)
  }

  const onDeleteHistoryEntry = () => {
    if (!draft || !historyView) return
    if (!window.confirm(`Delete snapshot ${historyView.ts}? This cannot be undone.`)) return
    void run(async () => {
      await deleteScenarioHistory(draft.id, historyView.ts, { bearer })
      const r = await listScenarioHistory(draft.id, { bearer })
      setHistory(r.history ?? [])
      if (historyDiffBase === historyView.ts) {
        setHistoryDiffBase('current')
      }
      setHistoryView(null)
    })
  }

  const onOpenFork = () => {
    if (!draft || !historyView) return
    setForkDraft({
      id: `${draft.id}_fork`,
      name: `${draft.name || draft.id} (fork)`,
    })
    setForkOpen(true)
  }

  const onForkSubmit = () => {
    if (!draft || !historyView || !forkDraft.id.trim()) return
    void run(async () => {
      await forkScenarioHistory(
        draft.id,
        historyView.ts,
        { id: forkDraft.id.trim(), name: forkDraft.name.trim() || forkDraft.id.trim() },
        { bearer },
      )
      setForkOpen(false)
      setHistory(null)
      setHistoryView(null)
      await refresh()
    })
  }

  const onDragOver = (e: React.DragEvent) => {
    if (!Array.from(e.dataTransfer.types).includes('Files')) return
    e.preventDefault()
    e.dataTransfer.dropEffect = 'copy'
    setDragOver(true)
  }
  const onDragLeave = (e: React.DragEvent) => {
    // Only un-set when leaving the wrapping element itself (drag enters into
    // child nodes would otherwise constantly flip the overlay off).
    if (e.currentTarget === e.target) setDragOver(false)
  }
  const onDrop = (e: React.DragEvent) => {
    e.preventDefault()
    setDragOver(false)
    const f = Array.from(e.dataTransfer.files).find(
      (f) => /\.xml$/i.test(f.name) || f.type === 'application/xml' || f.type === 'text/xml',
    )
    if (f) onUploadFile(f)
  }

  return (
    <section
      className="relative flex flex-col gap-3"
      onDragOver={onDragOver}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
    >
      {dragOver ? (
        <div
          className="border-primary/60 bg-primary/5 text-primary pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-md border-2 border-dashed text-sm font-medium"
          aria-hidden
        >
          Drop a .xml scenario here…
        </div>
      ) : null}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold">Scenarios</h2>
          <p className="text-muted-foreground text-xs">
            SIP XML scenarios. Stored as <code>scenarios/&lt;id&gt;.xml</code> plus a JSON sidecar with metadata. Tip: drag &amp; drop an .xml file anywhere on this panel.
          </p>
        </div>
        <div className="flex gap-2">
          <input
            ref={fileRef}
            type="file"
            className="hidden"
            accept=".xml,application/xml,text/xml"
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f) onUploadFile(f)
              e.target.value = ''
            }}
          />
          <Button type="button" variant="outline" size="sm" onClick={() => fileRef.current?.click()}>
            Upload XML
          </Button>
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

      <div className="flex flex-wrap items-center gap-2">
        <div className="flex flex-wrap gap-1">
          {(['all', 'server', 'client', 'either'] as const).map((r) => (
            <button
              key={r}
              type="button"
              onClick={() => setRoleFilter(r)}
              className={`rounded px-2 py-0.5 text-[11px] ${
                roleFilter === r
                  ? 'bg-primary text-primary-foreground'
                  : 'border-border bg-background hover:bg-muted border'
              }`}
            >
              {r} <span className="text-[10px] opacity-75">({roleCounts[r] ?? 0})</span>
            </button>
          ))}
        </div>
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="filter by id / name / description…"
          className="h-7 max-w-xs text-xs"
        />
        <span className="text-muted-foreground text-[11px]">
          {visibleRows.length}/{rows.length}
        </span>
      </div>

      <DataTable
        rows={visibleRows}
        columns={columns}
        rowKey={(r) => r.id}
        loading={busy && rows.length === 0}
        empty={
          rows.length === 0
            ? 'No scenarios yet — create one above or upload an XML.'
            : 'No scenarios match the current filter.'
        }
      />

      {builtins.length > 0 ? (
        <div className="border-border bg-card rounded-md border p-3">
          <h3 className="mb-2 text-xs font-medium">Built-in scenarios (read-only)</h3>
          <ul className="flex flex-wrap gap-2">
            {builtins.map((b) => (
              <li key={b.id}>
                <Button type="button" variant="outline" size="xs" onClick={() => onViewBuiltin(b.id)}>
                  {b.id}
                  {b.role ? ` · ${b.role}` : ''}
                </Button>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      <Modal
        open={draft !== null}
        onClose={() => setDraft(null)}
        size="xl"
        title={createMode ? 'New scenario' : `Edit scenario · ${draft?.id ?? ''}`}
        footer={
          <>
            {!createMode ? (
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={onOpenHistory}
                disabled={busy}
                className="mr-auto"
              >
                View history
              </Button>
            ) : null}
            <Button type="button" variant="outline" size="sm" onClick={() => setDraft(null)}>
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={onSave}
              disabled={busy || !!xmlError || !draft?.id.trim()}
              title={xmlError ?? undefined}
            >
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
              <div className="flex flex-wrap items-center justify-between gap-2">
                <Label className="text-xs">XML</Label>
                <div className="flex flex-wrap items-center gap-2">
                  {mediaWarnings && mediaWarnings.missing.length > 0 ? (
                    <span className="text-warning text-[10px]">
                      missing media: {mediaWarnings.missing.map((m) => `${m.kind}/${m.name}`).join(', ')}
                    </span>
                  ) : null}
                  {[...wavNames].slice(0, 8).map((name) => (
                    <Button
                      key={`wav-${name}`}
                      type="button"
                      variant="outline"
                      size="xs"
                      className="h-6 text-[10px]"
                      onClick={() => insertMediaAlias('wav', name)}
                    >
                      + wav/{name}
                    </Button>
                  ))}
                </div>
              </div>
              {xmlError ? (
                <span className="text-destructive text-[11px]" title={xmlError}>
                  XML error: {xmlError.length > 80 ? xmlError.slice(0, 80) + '…' : xmlError}
                </span>
              ) : (
                <span className="text-success text-[11px]">XML well-formed</span>
              )}
              <Textarea
                ref={xmlRef}
                value={draft.xml}
                onChange={(e) => setDraft({ ...draft, xml: e.target.value })}
                className={`mt-1 min-h-0 flex-1 font-mono text-xs ${
                  xmlError ? 'border-destructive/60' : ''
                }`}
                spellCheck={false}
              />
            </div>
          </div>
        ) : null}
      </Modal>

      <Modal
        open={history !== null}
        onClose={() => {
          setHistory(null)
          setHistoryView(null)
        }}
        size="xl"
        title={`History · ${draft?.id ?? ''}`}
        description={
          history && history.length === 0
            ? 'No prior versions yet. A snapshot is written every time you Save with changed XML.'
            : 'Click a version to preview. "Restore into editor" loads the archived XML into the current draft — the change is only persisted after Save.'
        }
        footer={
          <>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              onClick={onDeleteHistoryEntry}
              disabled={!historyView || busy}
            >
              Delete snapshot
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={onOpenFork}
              disabled={!historyView || busy}
            >
              Fork as new…
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => {
                setHistory(null)
                setHistoryView(null)
              }}
              className="ml-auto"
            >
              Close
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={onRestoreHistoryEntry}
              disabled={!historyView || busy}
            >
              Restore into editor
            </Button>
            <Button
              type="button"
              size="sm"
              variant="secondary"
              onClick={onRestoreOverwrite}
              disabled={!historyView || busy}
            >
              Restore overwrite
            </Button>
          </>
        }
      >
        {history ? (
          <div className="grid h-[70vh] grid-cols-12 gap-3">
            <div className="col-span-4 flex min-h-0 flex-col overflow-y-auto">
              <ul className="space-y-1">
                {history.map((h) => {
                  const selected = historyView?.ts === h.ts
                  return (
                    <li key={h.ts}>
                      <button
                        type="button"
                        onClick={() => onViewHistoryEntry(h.ts)}
                        className={`w-full rounded-md border px-2 py-1.5 text-left font-mono text-[11px] ${
                          selected
                            ? 'border-primary/60 bg-primary/10'
                            : 'border-border bg-background hover:bg-muted'
                        }`}
                      >
                        <div>{new Date(h.timestamp).toLocaleString()}</div>
                        <div className="text-muted-foreground text-[10px]">
                          {h.size_bytes} B
                          {h.meta?.name && h.meta.name !== draft?.id ? ` · ${h.meta.name}` : ''}
                        </div>
                      </button>
                    </li>
                  )
                })}
                {history.length === 0 ? (
                  <li className="text-muted-foreground text-xs">No snapshots yet.</li>
                ) : null}
              </ul>
            </div>
            <div className="col-span-8 flex min-h-0 flex-col">
              {historyView ? (
                <>
                  <div className="mb-1 flex flex-wrap items-center justify-between gap-2 text-[11px]">
                    <div className="text-muted-foreground">
                      Viewing <code>{historyView.ts}</code> · {historyView.xml.length} B
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      {historyMode !== 'xml' ? (
                        <label className="text-muted-foreground flex items-center gap-1">
                          Base
                          <select
                            value={historyDiffBase}
                            onChange={(e) => setHistoryDiffBase(e.target.value)}
                            className="border-input bg-background rounded-md border px-1.5 py-0.5 text-[10px]"
                          >
                            <option value="current">Current editor</option>
                            {history
                              ?.filter((h) => h.ts !== historyView.ts)
                              .map((h) => (
                                <option key={h.ts} value={h.ts}>
                                  {new Date(h.timestamp).toLocaleString()}
                                </option>
                              ))}
                          </select>
                        </label>
                      ) : null}
                      <HistoryDiffSummary
                        oldXML={diffOldXML}
                        newXML={diffNewXML}
                        baseLabel={diffBaseLabel}
                      />
                      <div className="border-border bg-background flex overflow-hidden rounded-md border text-[10px]">
                        {(['diff', 'side', 'xml'] as const).map((m) => (
                          <button
                            key={m}
                            type="button"
                            onClick={() => setHistoryMode(m)}
                            className={`px-2 py-0.5 ${
                              historyMode === m
                                ? 'bg-primary text-primary-foreground'
                                : 'hover:bg-muted'
                            }`}
                          >
                            {m === 'diff' ? 'Unified' : m === 'side' ? 'Side-by-side' : 'Snapshot XML'}
                          </button>
                        ))}
                      </div>
                    </div>
                  </div>
                  {historyMode === 'xml' ? (
                    <Textarea
                      value={historyView.xml}
                      readOnly
                      className="min-h-0 flex-1 font-mono text-[11px]"
                      spellCheck={false}
                    />
                  ) : historyMode === 'side' ? (
                    <SideBySideDiffView oldXML={diffOldXML} newXML={diffNewXML} />
                  ) : (
                    <DiffView oldXML={diffOldXML} newXML={diffNewXML} />
                  )}
                </>
              ) : (
                <div className="text-muted-foreground flex flex-1 items-center justify-center text-xs">
                  Select a version on the left to preview.
                </div>
              )}
            </div>
          </div>
        ) : null}
      </Modal>

      <Modal
        open={forkOpen}
        onClose={() => setForkOpen(false)}
        size="sm"
        title="Fork snapshot as new scenario"
        description="Creates a new scenario from the selected snapshot. The original is unchanged."
        footer={
          <>
            <Button type="button" variant="outline" size="sm" onClick={() => setForkOpen(false)}>
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={onForkSubmit}
              disabled={!forkDraft.id.trim() || busy}
            >
              Create fork
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-3">
          <div>
            <Label className="text-xs">New scenario ID</Label>
            <Input
              value={forkDraft.id}
              onChange={(e) => setForkDraft({ ...forkDraft, id: e.target.value })}
              className="mt-1 font-mono text-sm"
              placeholder="my_scenario_fork"
            />
          </div>
          <div>
            <Label className="text-xs">Name</Label>
            <Input
              value={forkDraft.name}
              onChange={(e) => setForkDraft({ ...forkDraft, name: e.target.value })}
              className="mt-1"
            />
          </div>
        </div>
      </Modal>

      <Modal
        open={builtinPreview !== null}
        onClose={() => setBuiltinPreview(null)}
        size="lg"
        title={builtinPreview ? `Built-in · ${builtinPreview.id}` : 'Built-in'}
      >
        {builtinPreview ? (
          <Textarea
            value={builtinPreview.xml}
            readOnly
            className="min-h-[50vh] font-mono text-[11px]"
            spellCheck={false}
          />
        ) : null}
      </Modal>
    </section>
  )
}

// HistoryDiffSummary renders a compact "+N -M" badge for the diff between
// base and the snapshot being viewed.
function HistoryDiffSummary({
  oldXML,
  newXML,
  baseLabel,
}: {
  oldXML: string
  newXML: string
  baseLabel: string
}) {
  const { added, removed } = useMemo(
    () => summariseDiff(lineDiff(oldXML, newXML)),
    [oldXML, newXML],
  )
  if (added === 0 && removed === 0) {
    return <span className="text-muted-foreground">no changes vs {baseLabel}</span>
  }
  return (
    <span className="font-mono">
      <span className="text-success">+{added}</span>{' '}
      <span className="text-destructive">-{removed}</span>{' '}
      <span className="text-muted-foreground">vs {baseLabel}</span>
    </span>
  )
}

// DiffView renders a unified, single-column line diff with colour-coded
// gutters. It treats `oldXML` (the snapshot) as "old" and `newXML` (current
// draft) as "new" — so "+" lines are present in the editor and "-" lines
// only in the snapshot. Inputs are memoised; per-line virtualisation is not
// needed at scenario-XML sizes.
function SideBySideDiffView({ oldXML, newXML }: { oldXML: string; newXML: string }) {
  const rows = useMemo(() => sideBySideDiff(oldXML, newXML), [oldXML, newXML])
  return (
    <div className="grid min-h-0 flex-1 grid-cols-2 gap-px overflow-auto rounded-md border font-mono text-[11px] leading-snug">
      <div className="bg-muted/20 border-border border-r">
        <div className="text-muted-foreground bg-muted/40 px-2 py-1 text-[10px]">Base</div>
        {rows.map((r, idx) => (
          <SideRow key={`l-${idx}`} side="left" row={r} />
        ))}
      </div>
      <div className="bg-muted/20">
        <div className="text-muted-foreground bg-muted/40 px-2 py-1 text-[10px]">Snapshot</div>
        {rows.map((r, idx) => (
          <SideRow key={`r-${idx}`} side="right" row={r} />
        ))}
      </div>
    </div>
  )
}

function SideRow({ side, row }: { side: 'left' | 'right'; row: SideBySideRow }) {
  const op = side === 'left' ? row.leftOp : row.rightOp
  const text = side === 'left' ? row.leftText : row.rightText
  const no = side === 'left' ? row.leftNo : row.rightNo
  const rowCls =
    op === 'add' ? 'bg-success/10' : op === 'del' ? 'bg-destructive/10' : op === 'blank' ? 'opacity-40' : ''
  return (
    <div className={`flex whitespace-pre ${rowCls}`}>
      <span className="text-muted-foreground bg-muted/40 select-none px-1.5">
        {no === -1 ? '    ' : no.toString().padStart(4, ' ')}
      </span>
      <span className="flex-1 px-1">{text === '' ? '\u00A0' : text}</span>
    </div>
  )
}

function DiffView({ oldXML, newXML }: { oldXML: string; newXML: string }) {
  const lines = useMemo(() => lineDiff(oldXML, newXML), [oldXML, newXML])
  return (
    <div className="bg-background min-h-0 flex-1 overflow-auto rounded-md border font-mono text-[11px] leading-snug">
      {lines.length === 0 ? (
        <div className="text-muted-foreground p-2">Both versions are empty.</div>
      ) : (
        lines.map((d, idx) => <DiffRow key={idx} line={d} />)
      )}
    </div>
  )
}

function DiffRow({ line }: { line: DiffLine }) {
  const sign = line.op === 'add' ? '+' : line.op === 'del' ? '-' : ' '
  const rowCls =
    line.op === 'add'
      ? 'bg-success/10'
      : line.op === 'del'
        ? 'bg-destructive/10'
        : ''
  const signCls =
    line.op === 'add'
      ? 'text-success'
      : line.op === 'del'
        ? 'text-destructive'
        : 'text-muted-foreground'
  const fmt = (n: number) => (n === -1 ? '    ' : n.toString().padStart(4, ' '))
  return (
    <div className={`flex whitespace-pre ${rowCls}`}>
      <span className="text-muted-foreground bg-muted/40 select-none px-1.5">{fmt(line.oldNo)}</span>
      <span className="text-muted-foreground bg-muted/40 select-none px-1.5">{fmt(line.newNo)}</span>
      <span className={`select-none px-1.5 ${signCls}`}>{sign}</span>
      <span className="flex-1 px-1">{line.text === '' ? '\u00A0' : line.text}</span>
    </div>
  )
}
