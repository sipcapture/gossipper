import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import {
  deleteMedia,
  downloadMediaURL,
  listMedia,
  uploadMedia,
  type MediaAsset,
  type MediaKind,
} from '@/api/v2'
import { Button } from '@/components/ui/button'
import { DataTable, type Column } from '@/components/ui/data-table'

function fmtSize(n: number): string {
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KiB'
  return (n / (1024 * 1024)).toFixed(2) + ' MiB'
}

export type MediaV2Props = {
  bearer?: string
  busy: boolean
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  errorText?: string | null
}

export function MediaV2({ bearer, busy, run, errorText }: MediaV2Props) {
  const [kind, setKind] = useState<MediaKind>('wav')
  const [rows, setRows] = useState<MediaAsset[]>([])
  const fileRef = useRef<HTMLInputElement>(null)

  const refresh = useCallback(async () => {
    const r = await listMedia(kind, { bearer })
    setRows(r.media ?? [])
  }, [bearer, kind])

  useEffect(() => {
    void run(() => refresh())
  }, [run, refresh])

  const onUpload = (file: File) => {
    void run(async () => {
      await uploadMedia(kind, file, { bearer })
      await refresh()
    })
  }

  const onDelete = (row: MediaAsset) => {
    if (!window.confirm(`Delete ${row.name}?`)) return
    void run(async () => {
      await deleteMedia(kind, row.name, { bearer })
      await refresh()
    })
  }

  const columns: Column<MediaAsset>[] = useMemo(
    () => [
      { key: 'name', header: 'Name', render: (r) => r.name },
      {
        key: 'size',
        header: 'Size',
        align: 'right',
        render: (r) => <span className="font-mono text-xs">{fmtSize(r.size_bytes)}</span>,
      },
      {
        key: 'mtime',
        header: 'Modified',
        render: (r) => new Date(r.mod_time).toLocaleString(),
      },
      {
        key: 'preview',
        header: 'Preview',
        render: (r) =>
          r.kind === 'wav' ? (
            <audio controls className="h-7" src={downloadMediaURL('wav', r.name, bearer)} />
          ) : (
            <span className="text-muted-foreground text-xs">—</span>
          ),
      },
      {
        key: 'actions',
        header: '',
        align: 'right',
        render: (r) => (
          <div className="flex justify-end gap-1">
            <a
              href={downloadMediaURL(r.kind, r.name, bearer)}
              download={r.name}
              className="bg-background border-border hover:bg-muted rounded-md border px-2.5 py-1 text-xs"
            >
              Download
            </a>
            <Button type="button" variant="destructive" size="xs" onClick={() => onDelete(r)}>
              Delete
            </Button>
          </div>
        ),
      },
    ],
    [bearer, onDelete],
  )

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold">Media library</h2>
          <p className="text-muted-foreground text-xs">
            WAV files for media playback and PCAP files for replay tests. Stored under <code>media/&lt;kind&gt;/</code>.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <div className="border-border bg-background flex overflow-hidden rounded-md border text-xs">
            {(['wav', 'pcap'] as MediaKind[]).map((k) => (
              <button
                key={k}
                type="button"
                onClick={() => setKind(k)}
                className={`px-3 py-1 ${kind === k ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'}`}
              >
                {k.toUpperCase()}
              </button>
            ))}
          </div>
          <input
            ref={fileRef}
            type="file"
            className="hidden"
            accept={kind === 'wav' ? 'audio/wav,audio/wave,.wav' : '.pcap,application/vnd.tcpdump.pcap'}
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f) onUpload(f)
              e.target.value = ''
            }}
          />
          <Button type="button" size="sm" onClick={() => fileRef.current?.click()}>
            Upload {kind.toUpperCase()}
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={() => void run(() => refresh())}>
            Refresh
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
        rowKey={(r) => r.name}
        loading={busy && rows.length === 0}
        empty={`No ${kind.toUpperCase()} files yet.`}
      />
    </section>
  )
}
