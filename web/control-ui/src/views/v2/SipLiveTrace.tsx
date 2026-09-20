import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { ChangeEvent } from 'react'

import { clearSIPTrace, downloadSIPTrace, putSIPTraceCapture, sipTraceWSURL } from '@/api/v2'
import type { ApiErrorV2, SIPTraceCapture, SIPTraceMessage, SIPTraceResponse } from '@/api/v2'
import { Button } from '@/components/ui/button'
import { LOG_LEVELS, appendSIPTrace, applySIPTraceFrame, rowMatchesCapture } from '@/lib/sipLive'
import { cn } from '@/lib/utils'

const MAX_ROWS = 400

interface SipLiveTraceProps {
  bearer?: string
  compact?: boolean
  fill?: boolean
}

export function SipLiveTrace({ bearer, compact, fill }: SipLiveTraceProps) {
  const [rows, setRows] = useState<SIPTraceMessage[]>([])
  const [paused, setPaused] = useState(false)
  const [connected, setConnected] = useState(false)
  const [filter, setFilter] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [openSeq, setOpenSeq] = useState<number | null>(null)
  const [exporting, setExporting] = useState<'text' | 'pcap' | null>(null)
  const [capture, setCapture] = useState<SIPTraceCapture>({ sip: true, app: false, level: 'debug' })
  const sinceRef = useRef(0)
  const pausedRef = useRef(false)
  const heldRef = useRef<SIPTraceMessage[]>([])
  const stickRef = useRef(true)
  const scrollerRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    pausedRef.current = paused
  }, [paused])

  useEffect(() => {
    let alive = true
    let retry: ReturnType<typeof setTimeout> | null = null
    let ws: WebSocket | null = null
    const connect = () => {
      if (!alive) return
      try {
        const sock = new WebSocket(sipTraceWSURL(bearer, sinceRef.current))
        ws = sock
        sock.onopen = () => {
          if (!alive) return
          setConnected(true)
          setErr(null)
        }
        sock.onmessage = (ev) => {
          let data: SIPTraceResponse
          try {
            data = JSON.parse(ev.data as string) as SIPTraceResponse
          } catch {
            return
          }
          sinceRef.current = data.next
          if (data.capture) setCapture(data.capture)
          if (data.cleared) {
            heldRef.current = []
            setRows([])
            setOpenSeq(null)
            return
          }
          if (!data.messages?.length) return
          if (pausedRef.current) {
            heldRef.current = appendSIPTrace(heldRef.current, data.messages, MAX_ROWS)
            return
          }
          setRows((prev) => applySIPTraceFrame(prev, data, MAX_ROWS))
        }
        sock.onclose = () => {
          setConnected(false)
          if (!alive) return
          retry = setTimeout(connect, 1500)
        }
        sock.onerror = () => sock.close()
      } catch (e) {
        setConnected(false)
        setErr(e instanceof Error ? e.message : 'trace unavailable')
        retry = setTimeout(connect, 3000)
      }
    }
    connect()
    return () => {
      alive = false
      if (retry) clearTimeout(retry)
      ws?.close()
    }
  }, [bearer])

  useEffect(() => {
    if (paused) return
    if (heldRef.current.length === 0) return
    const held = heldRef.current
    heldRef.current = []
    setRows((prev) => appendSIPTrace(prev, held, MAX_ROWS))
  }, [paused])

  useEffect(() => {
    const el = scrollerRef.current
    if (!el || !stickRef.current) return
    el.scrollTop = el.scrollHeight
  }, [rows])

  const onScroll = useCallback(() => {
    const el = scrollerRef.current
    if (!el) return
    stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
  }, [])

  const onClear = useCallback(() => {
    void (async () => {
      const data = await clearSIPTrace({ bearer })
      sinceRef.current = data.next
      setRows([])
      setOpenSeq(null)
      setFilter('')
      setErr(null)
    })()
  }, [bearer])

  const applyCapture = useCallback(
    (next: SIPTraceCapture) => {
      setCapture(next)
      void (async () => {
        try {
          const got = await putSIPTraceCapture(next, { bearer })
          setCapture(got)
          setErr(null)
        } catch (e) {
          const api = e as ApiErrorV2
          setErr(api.message || (e instanceof Error ? e.message : 'capture update failed'))
          setCapture(capture)
        }
      })()
    },
    [bearer, capture],
  )

  const onToggleSip = useCallback(() => {
    applyCapture({ ...capture, sip: !capture.sip })
  }, [applyCapture, capture])

  const onToggleApp = useCallback(() => {
    applyCapture({ ...capture, app: !capture.app })
  }, [applyCapture, capture])

  const onLevelChange = useCallback(
    (ev: ChangeEvent<HTMLSelectElement>) => {
      applyCapture({ ...capture, level: ev.target.value })
    },
    [applyCapture, capture],
  )

  const onTogglePause = useCallback(() => {
    setPaused((v) => !v)
  }, [])

  const onExport = useCallback(
    (format: 'text' | 'pcap') => {
      void (async () => {
        setExporting(format)
        setErr(null)
        try {
          await downloadSIPTrace(format, { bearer, scenario: filter || undefined })
        } catch (e) {
          const api = e as ApiErrorV2
          setErr(api.message || (e instanceof Error ? e.message : 'export failed'))
        } finally {
          setExporting(null)
        }
      })()
    },
    [bearer, filter],
  )

  const onExportText = useCallback(() => {
    onExport('text')
  }, [onExport])

  const onExportPcap = useCallback(() => {
    onExport('pcap')
  }, [onExport])

  const onToggleRow = useCallback((seq: number) => {
    setOpenSeq((cur) => (cur === seq ? null : seq))
  }, [])

  const onFilterChange = useCallback((ev: ChangeEvent<HTMLSelectElement>) => {
    setFilter(ev.target.value)
  }, [])

  const scenarios = useMemo(() => {
    const names = new Set<string>()
    for (const row of rows) {
      if (row.scenario) names.add(row.scenario)
    }
    return [...names].sort()
  }, [rows])

  const visible = useMemo(
    () =>
      rows.filter((row) => rowMatchesCapture(row, capture) && (!filter || row.scenario === filter)),
    [capture, filter, rows],
  )

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="text-xs font-medium">Live Trace</p>
          <p className="text-muted-foreground text-[11px]">
            SIP plus scenario debug on one timeline. SIP and Debug are independent capture toggles;
            Debug level (debug/info/warn/error) changes on the fly. Export downloads the full ring
            (text or pcap; pcap is SIP only), not only the last {MAX_ROWS} rows.
          </p>
        </div>
        <div className="flex items-center gap-1">
          <span className={cn('text-[11px]', connected ? 'text-emerald-700' : 'text-muted-foreground')}>
            {connected ? 'live' : 'offline'}
          </span>
          {scenarios.length > 0 ? (
            <select
              className="border-input bg-background h-7 rounded-md border px-1.5 text-[11px]"
              value={filter}
              onChange={onFilterChange}
              aria-label="Filter by scenario"
            >
              <option value="">All scenarios</option>
              {scenarios.map((name) => (
                <option key={name} value={name}>
                  {name}
                </option>
              ))}
            </select>
          ) : null}
          <span className="text-muted-foreground text-[11px]">{visible.length} msgs</span>
          <Button
            type="button"
            variant={capture.sip ? 'default' : 'outline'}
            size="xs"
            aria-pressed={capture.sip}
            onClick={onToggleSip}
          >
            SIP
          </Button>
          <Button
            type="button"
            variant={capture.app ? 'default' : 'outline'}
            size="xs"
            aria-pressed={capture.app}
            onClick={onToggleApp}
          >
            Debug
          </Button>
          <select
            className="border-input bg-background h-7 rounded-md border px-1.5 text-[11px]"
            value={capture.level ?? 'debug'}
            onChange={onLevelChange}
            aria-label="Debug log level"
          >
            {LOG_LEVELS.map((level) => (
              <option key={level} value={level}>
                {level}
              </option>
            ))}
          </select>
          <Button type="button" variant="outline" size="xs" onClick={onTogglePause}>
            {paused ? 'Resume' : 'Pause'}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="xs"
            disabled={exporting !== null}
            onClick={onExportText}
          >
            {exporting === 'text' ? 'Saving…' : 'Text'}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="xs"
            disabled={exporting !== null}
            onClick={onExportPcap}
          >
            {exporting === 'pcap' ? 'Saving…' : 'PCAP'}
          </Button>
          <Button type="button" variant="outline" size="xs" onClick={onClear}>
            Clear
          </Button>
        </div>
      </div>
      {err ? <p className="text-destructive text-[11px]">{err}</p> : null}
      <div
        ref={scrollerRef}
        onScroll={onScroll}
        className={cn(
          'bg-muted/40 overflow-auto rounded-md border font-mono text-[11px]',
          compact ? 'max-h-48' : fill ? 'min-h-[28rem] h-[calc(100vh-11rem)]' : 'h-80',
        )}
      >
        {visible.length === 0 ? (
          <p className="text-muted-foreground p-3">
            {!capture.sip && !capture.app
              ? 'Enable SIP or Debug to capture.'
              : 'Waiting for events…'}
          </p>
        ) : (
          visible.map((m) => (
            <SipTraceRow
              key={m.seq}
              msg={m}
              open={openSeq === m.seq}
              onToggle={onToggleRow}
            />
          ))
        )}
      </div>
    </div>
  )
}

interface SipTraceRowProps {
  msg: SIPTraceMessage
  open: boolean
  onToggle: (seq: number) => void
}

function SipTraceRow({ msg, open, onToggle }: SipTraceRowProps) {
  const app = msg.kind === 'app'
  const outbound = !app && msg.dir === 'send'
  const onClick = useCallback(() => {
    onToggle(msg.seq)
  }, [msg.seq, onToggle])
  const badge = app ? 'DBG' : outbound ? 'OUT' : 'IN'
  return (
    <div className={cn('border-b last:border-b-0', open ? 'bg-background/60' : undefined)}>
      <button
        type="button"
        className="hover:bg-background/80 flex w-full items-start gap-2 px-2 py-1 text-left"
        onClick={onClick}
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
          {badge}
        </span>
        <span className="text-muted-foreground w-[72px] shrink-0 tabular-nums">
          {formatSipTime(msg.ts)}
        </span>
        {msg.scenario ? (
          <span className="bg-background text-muted-foreground mt-0.5 max-w-[120px] shrink-0 truncate rounded border px-1">
            {msg.scenario}
          </span>
        ) : null}
        {app && msg.level ? (
          <span className="text-muted-foreground mt-0.5 w-10 shrink-0 uppercase">{msg.level}</span>
        ) : null}
        <span className="min-w-0 flex-1 truncate">{msg.summary}</span>
        <span className="text-muted-foreground hidden max-w-[160px] truncate sm:inline">
          {msg.peer}
        </span>
      </button>
      {open ? (
        <pre className="text-muted-foreground overflow-auto px-2 pb-2 whitespace-pre-wrap">
          {msg.raw}
        </pre>
      ) : null}
    </div>
  )
}

function formatSipTime(ts: string): string {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ts
  return d.toLocaleTimeString(undefined, { hour12: false })
}
