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
import { validateScenarioXML } from '@/lib/xmlValidate'
import { validateMediaRefs } from '@/lib/mediaRefs'
import {
  openScenarioEditorWindow,
  type ScenarioRouteKind,
} from '@/lib/routing'
import {
  clearPendingNewScenario,
  peekPendingNewScenario,
  roleMatchesFilter,
  sourceLabel,
  stashPendingNewScenario,
} from '@/lib/scenarioDraft'
import { PrepToolsPanel } from '@/components/v2/PrepToolsPanel'
import { PcapImportPanel } from '@/components/v2/PcapImportPanel'
import { ScenarioGraphEditor } from '@/components/v2/ScenarioGraphEditor'
import { Button } from '@/components/ui/button'
import { DataTable, type Column } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Modal } from '@/components/ui/modal'
import { Textarea } from '@/components/ui/textarea'

function slugifyID(name: string): string {
  const stripped = name.replace(/\.[^./]+$/, '')
  return stripped
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, '_')
    .replace(/_+/g, '_')
    .replace(/^[._-]+|[._-]+$/g, '')
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
type SourceKind = 'store' | 'builtin' | 'lab'
type SourceFilter = 'mine' | 'builtin' | 'lab' | 'all'

type ListRow = {
  key: string
  id: string
  name: string
  role?: string
  description?: string
  source: SourceKind
  updated_at?: string
}

export type ScenariosV2Props = {
  bearer?: string
  busy: boolean
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  errorText?: string | null
  scenarioKind?: ScenarioRouteKind
  scenarioId?: string
  onScenarioRoute?: (kind: ScenarioRouteKind, id?: string) => void
}

export function ScenariosV2({
  bearer,
  busy,
  run,
  errorText,
  scenarioKind = 'list',
  scenarioId,
  onScenarioRoute,
}: ScenariosV2Props) {
  const [rows, setRows] = useState<ScenarioMeta[]>([])
  const [draft, setDraft] = useState<Draft | null>(null)
  const [createMode, setCreateMode] = useState(false)
  const [sourceFilter, setSourceFilter] = useState<SourceFilter>('mine')
  const [roleFilter, setRoleFilter] = useState<'all' | 'server' | 'client' | 'either'>('all')
  const [query, setQuery] = useState('')
  const [dragOver, setDragOver] = useState(false)
  const [helpOpen, setHelpOpen] = useState(false)
  const [history, setHistory] = useState<ScenarioHistoryEntry[] | null>(null)
  const [historyView, setHistoryView] = useState<{ ts: string; xml: string } | null>(null)
  const [historyMode, setHistoryMode] = useState<'diff' | 'side' | 'xml'>('diff')
  const [historyDiffBase, setHistoryDiffBase] = useState<'current' | string>('current')
  const [historyBaseXML, setHistoryBaseXML] = useState('')
  const [forkOpen, setForkOpen] = useState(false)
  const [forkDraft, setForkDraft] = useState({ id: '', name: '' })
  const [builtins, setBuiltins] = useState<BuiltinScenarioMeta[]>([])
  const [wavNames, setWavNames] = useState<Set<string>>(new Set())
  const [pcapNames, setPcapNames] = useState<Set<string>>(new Set())
  const [editorTab, setEditorTab] = useState<'graph' | 'xml'>('graph')
  const xmlRef = useRef<HTMLTextAreaElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const editor = scenarioKind !== 'list'

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

  const runRef = useRef(run)
  useEffect(() => {
    runRef.current = run
  }, [run])

  useEffect(() => {
    if (!editor) return
    let cancelled = false
    const load = async () => {
      if (scenarioKind === 'new') {
        const pending = peekPendingNewScenario()
        if (cancelled) return
        setDraft(pending ?? { id: '', name: '', xml: STARTER_XML })
        setCreateMode(true)
        setEditorTab('graph')
        return
      }
      if (scenarioKind === 'edit' && scenarioId) {
        const body = await getScenarioV2(scenarioId, { bearer })
        if (cancelled) return
        setDraft({
          id: body.meta.id,
          name: body.meta.name,
          description: body.meta.description,
          role: body.meta.role,
          xml: body.xml,
        })
        setCreateMode(false)
        setEditorTab('graph')
        return
      }
      if (scenarioKind === 'builtin' && scenarioId) {
        const body = await getBuiltinScenario(scenarioId, { bearer })
        if (cancelled) return
        setDraft({
          id: `${scenarioId}_copy`,
          name: body.meta?.name || scenarioId,
          role: body.meta?.role,
          xml: body.xml,
        })
        setCreateMode(true)
        setEditorTab('graph')
      }
    }
    void runRef.current(load)
    return () => {
      cancelled = true
    }
  }, [editor, scenarioKind, scenarioId, bearer])

  useEffect(() => {
    if (editor) return
    const onFocus = () => {
      void runRef.current(() => refresh())
    }
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [editor, refresh])

  const leaveEditor = useCallback(() => {
    if (createMode) clearPendingNewScenario()
    onScenarioRoute?.('list')
    if (window.opener && !window.opener.closed) {
      try {
        window.close()
      } catch {
        /* ignore */
      }
    }
  }, [createMode, onScenarioRoute])

  const openNewEditor = useCallback((next?: Draft) => {
    if (next) stashPendingNewScenario(next)
    else clearPendingNewScenario()
    const popped = openScenarioEditorWindow({ kind: 'new' })
    if (!popped) onScenarioRoute?.('new')
  }, [onScenarioRoute])

  const openRow = useCallback(
    (row: ListRow) => {
      if (row.source === 'store') {
        const popped = openScenarioEditorWindow({ kind: 'edit', id: row.id })
        if (!popped) onScenarioRoute?.('edit', row.id)
        return
      }
      const popped = openScenarioEditorWindow({ kind: 'builtin', id: row.id })
      if (!popped) onScenarioRoute?.('builtin', row.id)
    },
    [onScenarioRoute],
  )

  const onUploadFile = (file: File) => {
    void run(async () => {
      const text = await file.text()
      const id = slugifyID(file.name) || 'scenario'
      openNewEditor({
        id,
        name: file.name.replace(/\.[^./]+$/, ''),
        xml: text,
      })
      setHelpOpen(false)
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
        clearPendingNewScenario()
      } else {
        await updateScenarioV2(draft.id, meta, draft.xml, { bearer })
      }
      await refresh()
      leaveEditor()
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

  const onDelete = useCallback(
    (row: ListRow) => {
      if (row.source !== 'store') return
      if (!window.confirm(`Delete scenario "${row.id}"?`)) return
      void run(async () => {
        await deleteScenarioV2(row.id, { bearer })
        await refresh()
      })
    },
    [bearer, refresh, run],
  )

  const allRows: ListRow[] = useMemo(() => {
    const store: ListRow[] = rows.map((s) => ({
      key: `store:${s.id}`,
      id: s.id,
      name: s.name,
      role: s.role,
      description: s.description,
      source: 'store',
      updated_at: s.updated_at,
    }))
    const catalog: ListRow[] = builtins.map((b) => ({
      key: `${b.source === 'lab' ? 'lab' : 'builtin'}:${b.id}`,
      id: b.id,
      name: b.name || b.id,
      role: b.role,
      description: b.description,
      source: b.source === 'lab' ? 'lab' : 'builtin',
    }))
    return [...store, ...catalog]
  }, [rows, builtins])

  const sourceCounts = useMemo(() => {
    const out = { all: allRows.length, mine: 0, builtin: 0, lab: 0 }
    for (const r of allRows) {
      if (r.source === 'store') out.mine++
      else if (r.source === 'lab') out.lab++
      else out.builtin++
    }
    return out
  }, [allRows])

  const visibleRows = useMemo(() => {
    const q = query.trim().toLowerCase()
    return allRows.filter((s) => {
      if (sourceFilter === 'mine' && s.source !== 'store') return false
      if (sourceFilter === 'builtin' && s.source !== 'builtin') return false
      if (sourceFilter === 'lab' && s.source !== 'lab') return false
      if (!roleMatchesFilter(s.role, roleFilter)) return false
      if (!q) return true
      return (
        s.id.toLowerCase().includes(q) ||
        (s.name ?? '').toLowerCase().includes(q) ||
        (s.description ?? '').toLowerCase().includes(q)
      )
    })
  }, [allRows, sourceFilter, roleFilter, query])

  const roleCounts = useMemo(() => {
    const scoped =
      sourceFilter === 'all'
        ? allRows
        : allRows.filter((s) =>
            sourceFilter === 'mine' ? s.source === 'store' : s.source === sourceFilter,
          )
    const out: Record<string, number> = { all: scoped.length, server: 0, client: 0, either: 0 }
    for (const s of scoped) {
      if (roleMatchesFilter(s.role, 'server')) out.server++
      else if (roleMatchesFilter(s.role, 'client')) out.client++
      else out.either++
    }
    return out
  }, [allRows, sourceFilter])

  const columns: Column<ListRow>[] = useMemo(
    () => [
      { key: 'id', header: 'ID', render: (r) => <code className="text-xs">{r.id}</code> },
      { key: 'name', header: 'Name', render: (r) => r.name || '—' },
      {
        key: 'source',
        header: 'Source',
        render: (r) => (
          <span className="text-muted-foreground text-[11px]">{sourceLabel(r.source)}</span>
        ),
      },
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
          <div className="flex justify-end gap-1" onClick={(e) => e.stopPropagation()}>
            <Button type="button" variant="outline" size="xs" onClick={() => openRow(r)}>
              {r.source === 'store' ? 'Edit' : 'Clone'}
            </Button>
            {r.source === 'store' ? (
              <Button type="button" variant="destructive" size="xs" onClick={() => onDelete(r)}>
                Delete
              </Button>
            ) : null}
          </div>
        ),
      },
    ],
    [onDelete, openRow],
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

  useEffect(() => {
    if (history === null || !draft) return
    if (historyDiffBase === 'current') {
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

  const diffOldXML = historyDiffBase === 'current' ? (draft?.xml ?? '') : historyBaseXML
  const diffNewXML = historyView?.xml ?? ''
  const diffBaseLabel =
    historyDiffBase === 'current'
      ? 'current editor'
      : new Date(history?.find((h) => h.ts === historyDiffBase)?.timestamp ?? '').toLocaleString() ||
        historyDiffBase

  const onViewHistoryEntry = (ts: string) => {
    if (!draft) return
    void run(async () => {
      const body = await getScenarioHistory(draft.id, ts, { bearer })
      setHistoryView({ ts, xml: body.xml })
    })
  }

  const onRestoreHistoryEntry = () => {
    if (!draft || !historyView) return
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

  const historyModals = (
    <>
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
            <Button type="button" variant="outline" size="sm" onClick={onOpenFork} disabled={!historyView || busy}>
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
            <Button type="button" size="sm" onClick={onRestoreHistoryEntry} disabled={!historyView || busy}>
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
                      <HistoryDiffSummary oldXML={diffOldXML} newXML={diffNewXML} baseLabel={diffBaseLabel} />
                      <div className="border-border bg-background flex overflow-hidden rounded-md border text-[10px]">
                        {(['diff', 'side', 'xml'] as const).map((m) => (
                          <button
                            key={m}
                            type="button"
                            onClick={() => setHistoryMode(m)}
                            className={`px-2 py-0.5 ${
                              historyMode === m ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'
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
            <Button type="button" size="sm" onClick={onForkSubmit} disabled={!forkDraft.id.trim() || busy}>
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
    </>
  )

  if (editor) {
    return (
      <section className="flex h-full min-h-0 flex-col gap-3">
        {errorText ? (
          <div className="border-destructive/40 bg-destructive/10 text-destructive rounded-md border px-3 py-2 text-xs">
            {errorText}
          </div>
        ) : null}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <Button type="button" variant="outline" size="sm" onClick={leaveEditor}>
              ← List
            </Button>
            <p className="text-muted-foreground text-xs">
              {createMode ? 'New scenario' : `Editing ${draft?.id ?? scenarioId ?? ''}`}
            </p>
          </div>
          <div className="flex gap-2">
            {!createMode ? (
              <Button type="button" variant="outline" size="sm" onClick={onOpenHistory} disabled={busy}>
                View history
              </Button>
            ) : null}
            <Button
              type="button"
              size="sm"
              onClick={onSave}
              disabled={busy || !!xmlError || !draft?.id.trim()}
              title={xmlError ?? undefined}
            >
              {createMode ? 'Create' : 'Save'}
            </Button>
          </div>
        </div>
        {draft ? (
          <div className="flex min-h-0 flex-1 flex-col gap-3">
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
                <div className="border-border flex overflow-hidden rounded-md border text-[11px]">
                  {(['graph', 'xml'] as const).map((m) => (
                    <button
                      key={m}
                      type="button"
                      onClick={() => setEditorTab(m)}
                      className={`px-3 py-1 ${
                        editorTab === m ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'
                      }`}
                    >
                      {m === 'graph' ? 'Graph' : 'XML'}
                    </button>
                  ))}
                </div>
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
              {editorTab === 'graph' ? (
                <div className="mt-1 flex min-h-0 flex-1 flex-col">
                  <ScenarioGraphEditor xml={draft.xml} onChange={(xml) => setDraft({ ...draft, xml })} />
                </div>
              ) : (
                <Textarea
                  ref={xmlRef}
                  value={draft.xml}
                  onChange={(e) => setDraft({ ...draft, xml: e.target.value })}
                  className={`mt-1 min-h-0 flex-1 font-mono text-xs ${xmlError ? 'border-destructive/60' : ''}`}
                  spellCheck={false}
                />
              )}
            </div>
          </div>
        ) : (
          <p className="text-muted-foreground text-sm">Loading scenario…</p>
        )}
        {historyModals}
      </section>
    )
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
            Click a row to edit it in a new window. Import from PCAP, XML, or prep tools lives under Help.
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
          <Button type="button" variant="outline" size="sm" onClick={() => setHelpOpen(true)}>
            Help
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={() => void run(() => refresh())}>
            Refresh
          </Button>
          <Button type="button" size="sm" onClick={() => openNewEditor()}>
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
          {([
            ['mine', 'My scenarios'],
            ['builtin', 'Built-in'],
            ['lab', 'Lab'],
            ['all', 'All'],
          ] as const).map(([id, label]) => (
            <button
              key={id}
              type="button"
              onClick={() => setSourceFilter(id)}
              className={`rounded px-2 py-0.5 text-[11px] ${
                sourceFilter === id
                  ? 'bg-primary text-primary-foreground'
                  : 'border-border bg-background hover:bg-muted border'
              }`}
            >
              {label} <span className="text-[10px] opacity-75">({sourceCounts[id] ?? 0})</span>
            </button>
          ))}
        </div>
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
          {visibleRows.length}/{allRows.length}
        </span>
      </div>

      <DataTable
        rows={visibleRows}
        columns={columns}
        rowKey={(r) => r.key}
        onRowClick={openRow}
        loading={busy && allRows.length === 0}
        empty={
          sourceFilter === 'mine' && rows.length === 0
            ? 'No saved scenarios yet — New scenario, or Help to import XML / PCAP.'
            : 'No scenarios match the current filter.'
        }
      />

      <Modal
        open={helpOpen}
        onClose={() => setHelpOpen(false)}
        size="xl"
        title="Help · Import scenarios"
        description="All ways to bring a scenario into gossipper: XML upload, pcap2scenario job import, and prep tools."
        footer={
          <Button type="button" variant="outline" size="sm" onClick={() => setHelpOpen(false)}>
            Close
          </Button>
        }
      >
        <div className="flex flex-col gap-4">
          <section className="border-border flex flex-col gap-2 rounded-lg border p-3">
            <h3 className="text-sm font-medium">Upload XML</h3>
            <p className="text-muted-foreground text-xs">
              Import a SIPp/gossipper <code>.xml</code> file. It opens in a new editor window — set the ID and Save.
              You can also drop an XML file onto the Scenarios list.
            </p>
            <div className="flex flex-wrap gap-2">
              <Button type="button" size="sm" onClick={() => fileRef.current?.click()}>
                Choose XML file
              </Button>
              <Button type="button" size="sm" variant="outline" onClick={() => openNewEditor()}>
                Blank scenario
              </Button>
            </div>
          </section>
          <PcapImportPanel
            bearer={bearer}
            run={run}
            onImported={() => {
              void run(() => refresh())
              setSourceFilter('mine')
            }}
          />
          <PrepToolsPanel bearer={bearer} run={run} />
        </div>
      </Modal>
      {historyModals}
    </section>
  )
}

function HistoryDiffSummary({
  oldXML,
  newXML,
  baseLabel,
}: {
  oldXML: string
  newXML: string
  baseLabel: string
}) {
  const { added, removed } = useMemo(() => summariseDiff(lineDiff(oldXML, newXML)), [oldXML, newXML])
  if (added === 0 && removed === 0) {
    return <span className="text-muted-foreground">no changes vs {baseLabel}</span>
  }
  return (
    <span className="font-mono">
      <span className="text-success">+{added}</span> <span className="text-destructive">-{removed}</span>{' '}
      <span className="text-muted-foreground">vs {baseLabel}</span>
    </span>
  )
}

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
  const rowCls = line.op === 'add' ? 'bg-success/10' : line.op === 'del' ? 'bg-destructive/10' : ''
  const signCls =
    line.op === 'add' ? 'text-success' : line.op === 'del' ? 'text-destructive' : 'text-muted-foreground'
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
