import { useCallback, useEffect, useMemo, useState } from 'react'

import { clearCalls, downloadCallDump, getCall, listCalls, type CallDetail, type CallSummary, type SIPTraceMessage } from '@/api/v2'
import { Button } from '@/components/ui/button'
import { DataTable, type Column } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SipMessageModal, formatTraceTime } from '@/components/v2/SipMessageModal'
import { cn } from '@/lib/utils'

const STATE_BADGE: Record<string, string> = {
  active: 'bg-success/15 text-success',
  ended: 'bg-muted text-foreground/80',
  failed: 'bg-destructive/15 text-destructive',
}

export type CallsV2Props = {
  bearer?: string
  busy: boolean
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  callId?: string | null
  onOpenCall: (id?: string) => void
}

export function CallsV2({ bearer, busy, run, callId, onOpenCall }: CallsV2Props) {
  const [rows, setRows] = useState<CallSummary[]>([])
  const [query, setQuery] = useState('')
  const [detail, setDetail] = useState<CallDetail | null>(null)
  const [missing, setMissing] = useState(false)

  const refreshList = useCallback(async () => {
    const r = await listCalls({ bearer })
    setRows(r.calls ?? [])
  }, [bearer])

  const refreshDetail = useCallback(
    async (id: string) => {
      try {
        const d = await getCall(id, { bearer })
        setDetail(d)
        setMissing(false)
      } catch (e) {
        const status = e && typeof e === 'object' && 'status' in e ? (e as { status?: number }).status : undefined
        if (status === 404) {
          setDetail(null)
          setMissing(true)
          return
        }
        throw e
      }
    },
    [bearer],
  )

  useEffect(() => {
    void run(() => refreshList())
  }, [run, refreshList])

  useEffect(() => {
    if (!callId) return
    void run(() => refreshDetail(callId))
  }, [callId, run, refreshDetail])

  useEffect(() => {
    const tick = window.setInterval(() => {
      void refreshList()
      if (callId) void refreshDetail(callId)
    }, 2000)
    return () => window.clearInterval(tick)
  }, [callId, refreshList, refreshDetail])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return rows
    return rows.filter((r) =>
      [r.call_id, r.from, r.to, r.scenario, r.peer, r.rtp_dest, r.result, r.state]
        .filter(Boolean)
        .some((v) => String(v).toLowerCase().includes(q)),
    )
  }, [rows, query])

  const onZip = useCallback(
    (id: string) => {
      void run(() => downloadCallDump(id, { bearer }))
    },
    [bearer, run],
  )

  const columns = useMemo<Column<CallSummary>[]>(
    () => [
      {
        key: 'started',
        header: 'Time',
        render: (r) => <span className="tabular-nums text-xs">{formatWhen(r.started_at)}</span>,
      },
      {
        key: 'state',
        header: 'State',
        render: (r) => (
          <span className={cn('rounded px-1.5 py-0.5 text-[11px] font-medium', STATE_BADGE[r.state] ?? 'bg-muted')}>
            {r.state}
          </span>
        ),
      },
      { key: 'from', header: 'From', render: (r) => r.from || '—' },
      { key: 'to', header: 'To', render: (r) => r.to || '—' },
      { key: 'scenario', header: 'Scenario', render: (r) => r.scenario || '—' },
      {
        key: 'sip',
        header: 'SIP',
        align: 'right',
        render: (r) => <span className="tabular-nums">{r.sip_count}</span>,
      },
      {
        key: 'rtp',
        header: 'RTP',
        render: (r) => (
          <span className="tabular-nums text-xs">
            {r.rtp_count}
            {r.rtp_dest ? <span className="text-muted-foreground"> → {r.rtp_dest}</span> : null}
          </span>
        ),
      },
      {
        key: 'dur',
        header: 'Dur',
        align: 'right',
        render: (r) => <span className="tabular-nums text-xs">{formatDur(r.duration_ms)}</span>,
      },
      {
        key: 'zip',
        header: '',
        align: 'right',
        render: (r) => (
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-7 px-2 text-xs"
            disabled={busy}
            onClick={(e) => {
              e.stopPropagation()
              onZip(r.call_id)
            }}
          >
            Zip
          </Button>
        ),
      },
    ],
    [busy, onZip],
  )

  const onClear = useCallback(() => {
    void run(async () => {
      await clearCalls({ bearer })
      setRows([])
      setDetail(null)
      onOpenCall()
    })
  }, [bearer, onOpenCall, run])

  if (callId) {
    return (
      <CallDetailView
        id={callId}
        detail={callId ? detail : null}
        missing={callId ? missing : false}
        busy={busy}
        onBack={() => onOpenCall()}
        onRefresh={() => void run(() => refreshDetail(callId))}
        onZip={() => onZip(callId)}
      />
    )
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="text-sm font-medium">Calls</p>
          <p className="text-muted-foreground text-xs">
            Last {rows.length} dialogs (INVITE). Always recorded — Live Trace toggles do not apply.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Filter Call-ID, from, to…"
            className="h-8 w-56"
          />
          <Button type="button" size="sm" variant="outline" onClick={() => void run(() => refreshList())} disabled={busy}>
            Refresh
          </Button>
          <Button type="button" size="sm" variant="outline" onClick={onClear} disabled={busy || rows.length === 0}>
            Clear
          </Button>
        </div>
      </div>
      <DataTable
        rows={filtered}
        columns={columns}
        rowKey={(r) => r.call_id}
        loading={busy && rows.length === 0}
        empty="No calls yet. Place or receive an INVITE."
        onRowClick={(r) => onOpenCall(r.call_id)}
      />
    </div>
  )
}

function CallDetailView({
  id,
  detail,
  missing,
  busy,
  onBack,
  onRefresh,
  onZip,
}: {
  id: string
  detail: CallDetail | null
  missing: boolean
  busy: boolean
  onBack: () => void
  onRefresh: () => void
  onZip: () => void
}) {
  const [tab, setTab] = useState('sip')

  if (missing) {
    return (
      <div className="flex flex-col gap-3">
        <Button type="button" size="sm" variant="ghost" className="w-fit" onClick={onBack}>
          ← Calls
        </Button>
        <p className="text-muted-foreground text-sm">Call not found: {id}</p>
      </div>
    )
  }
  if (!detail) {
    return (
      <div className="flex flex-col gap-3">
        <Button type="button" size="sm" variant="ghost" className="w-fit" onClick={onBack}>
          ← Calls
        </Button>
        <p className="text-muted-foreground text-sm">Loading…</p>
      </div>
    )
  }

  const stats = [
    { label: 'SIP', value: `${detail.sip_count} (${detail.sip_send} out / ${detail.sip_recv} in)` },
    { label: 'RTP sent', value: `${detail.rtp_send} pkts · ${formatBytes(detail.rtp_bytes_send)}` },
    { label: 'RTP dest', value: detail.rtp_dest || '—' },
    { label: 'RTP src', value: detail.rtp_src || '—' },
    { label: 'Codec', value: detail.codec ? `${detail.codec}${detail.payload_type != null ? ` PT ${detail.payload_type}` : ''}` : '—' },
    { label: 'Debug', value: String(detail.debug_count) },
    { label: 'Duration', value: formatDur(detail.duration_ms) },
    { label: 'Peer', value: detail.peer || '—' },
  ]

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <Button type="button" size="sm" variant="ghost" onClick={onBack}>
            ← Calls
          </Button>
          <span className={cn('rounded px-1.5 py-0.5 text-[11px] font-medium', STATE_BADGE[detail.state] ?? 'bg-muted')}>
            {detail.state}
          </span>
          {detail.result ? <span className="text-muted-foreground text-xs">{detail.result}</span> : null}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button type="button" size="sm" variant="outline" onClick={onZip} disabled={busy}>
            Download zip
          </Button>
          <Button type="button" size="sm" variant="outline" onClick={onRefresh}>
            Refresh
          </Button>
        </div>
      </div>

      <div>
        <p className="font-mono text-sm break-all">{detail.call_id}</p>
        <p className="text-muted-foreground mt-0.5 text-xs">
          {detail.from || '—'} → {detail.to || '—'}
          {detail.scenario ? ` · ${detail.scenario}` : ''}
          {detail.direction ? ` · ${detail.direction}` : ''}
        </p>
        {detail.error ? <p className="text-destructive mt-1 text-xs">{detail.error}</p> : null}
      </div>

      <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
        {stats.map((s) => (
          <div key={s.label} className="bg-muted/40 rounded-md border px-3 py-2">
            <div className="text-muted-foreground text-[10px] uppercase tracking-wide">{s.label}</div>
            <div className="mt-0.5 font-mono text-xs break-all">{s.value}</div>
          </div>
        ))}
      </div>

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList variant="line">
          <TabsTrigger value="sip">SIP ({detail.sip.length})</TabsTrigger>
          <TabsTrigger value="debug">Debug ({detail.debug.length})</TabsTrigger>
          <TabsTrigger value="rtp">RTP ({detail.rtp.length})</TabsTrigger>
        </TabsList>
        <TabsContent value="sip">
          <TraceList rows={detail.sip} empty="No SIP on this call." />
        </TabsContent>
        <TabsContent value="debug">
          <TraceList rows={detail.debug} empty="No debug events." />
        </TabsContent>
        <TabsContent value="rtp">
          {detail.rtp.length === 0 ? (
            <p className="text-muted-foreground p-3 text-xs">No RTP sent on this call.</p>
          ) : (
            <div className="overflow-auto rounded-md border font-mono text-[11px]">
              <table className="w-full border-collapse">
                <thead className="bg-muted/40 text-muted-foreground">
                  <tr>
                    {['Time', 'Dir', 'Src', 'Dst', 'PT', 'Seq', 'Size'].map((h) => (
                      <th key={h} className="border-b px-2 py-1 text-left font-medium">
                        {h}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {detail.rtp.map((p, i) => (
                    <tr key={`${p.ts}-${p.seq}-${i}`} className="border-b last:border-b-0">
                      <td className="px-2 py-1 tabular-nums">{formatTraceTime(p.ts)}</td>
                      <td className="px-2 py-1 uppercase">{p.dir}</td>
                      <td className="px-2 py-1">{p.src}</td>
                      <td className="px-2 py-1">{p.dst}</td>
                      <td className="px-2 py-1 tabular-nums">{p.pt}</td>
                      <td className="px-2 py-1 tabular-nums">{p.seq}</td>
                      <td className="px-2 py-1 tabular-nums">{p.size}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {detail.rtp_count > detail.rtp.length ? (
                <p className="text-muted-foreground px-2 py-1 text-[10px]">
                  Showing last {detail.rtp.length} of {detail.rtp_count} packets.
                </p>
              ) : null}
            </div>
          )}
        </TabsContent>
      </Tabs>
    </div>
  )
}

function TraceList({
  rows,
  empty,
}: {
  rows: SIPTraceMessage[]
  empty: string
}) {
  const [open, setOpen] = useState<SIPTraceMessage | null>(null)

  if (rows.length === 0) {
    return <p className="text-muted-foreground p-3 text-xs">{empty}</p>
  }
  return (
    <div className="bg-muted/40 max-h-[calc(100vh-22rem)] overflow-auto rounded-md border font-mono text-[11px]">
      {rows.map((m) => {
        const app = m.kind === 'app'
        const outbound = !app && m.dir === 'send'
        return (
          <div key={m.seq} className="border-b last:border-b-0">
            <button
              type="button"
              className="hover:bg-background/80 flex w-full items-start gap-2 px-2 py-1 text-left"
              onClick={() => setOpen(m)}
            >
              <span
                className={cn(
                  'mt-0.5 shrink-0 rounded px-1 font-semibold',
                  app
                    ? 'bg-sky-500/15 text-sky-700'
                    : outbound
                      ? 'bg-amber-500/15 text-amber-700'
                      : 'bg-emerald-500/15 text-emerald-700',
                )}
              >
                {app ? 'DBG' : outbound ? 'OUT' : 'IN'}
              </span>
              <span className="text-muted-foreground w-[5.75rem] shrink-0 tabular-nums">{formatTraceTime(m.ts)}</span>
              {app && m.level ? (
                <span className="text-muted-foreground mt-0.5 w-10 shrink-0 uppercase">{m.level}</span>
              ) : null}
              <span className="min-w-0 flex-1 truncate">{m.summary}</span>
            </button>
          </div>
        )
      })}
      <SipMessageModal msg={open} onClose={() => setOpen(null)} />
    </div>
  )
}

function formatWhen(ts: string) {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ts
  return d.toISOString().replace('T', ' ').slice(0, 19)
}

function formatDur(ms: number) {
  if (!ms || ms < 0) return '0s'
  if (ms < 1000) return `${ms}ms`
  const s = ms / 1000
  if (s < 60) return `${s.toFixed(1)}s`
  const m = Math.floor(s / 60)
  const rem = Math.round(s % 60)
  return `${m}m ${String(rem).padStart(2, '0')}s`
}

function formatBytes(n: number) {
  if (!n) return '0 B'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}
