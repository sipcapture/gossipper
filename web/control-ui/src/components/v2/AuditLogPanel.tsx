import { useCallback, useEffect, useMemo, useState } from 'react'

import { listAudit, type AuditEntry } from '@/api/v2'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'

export type AuditLogPanelProps = {
  bearer?: string
  autoRefresh?: boolean
  limit?: number
  onRefresh?: () => void
}

export function AuditLogPanel({
  bearer,
  autoRefresh = true,
  limit = 200,
  onRefresh,
}: AuditLogPanelProps) {
  const [audit, setAudit] = useState<AuditEntry[]>([])
  const [filter, setFilter] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loadAudit = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const r = await listAudit({ bearer }, limit)
      setAudit(r.audit ?? [])
      onRefresh?.()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [bearer, limit, onRefresh])

  useEffect(() => {
    void loadAudit()
  }, [loadAudit])

  useEffect(() => {
    if (!autoRefresh) return
    const tick = setInterval(() => {
      void loadAudit()
    }, 4000)
    return () => clearInterval(tick)
  }, [autoRefresh, loadAudit])

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (!q) return audit
    return audit.filter(
      (e) =>
        e.action.toLowerCase().includes(q) ||
        (e.username ?? '').toLowerCase().includes(q) ||
        (e.target ?? '').toLowerCase().includes(q),
    )
  }, [audit, filter])

  return (
    <Card>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 space-y-0">
        <CardTitle className="text-sm">Audit log</CardTitle>
        <div className="flex items-center gap-2">
          <Button type="button" variant="outline" size="xs" onClick={() => void loadAudit()} disabled={loading}>
            Refresh
          </Button>
          <span className="text-muted-foreground text-[11px]">
            {filtered.length}/{audit.length}
          </span>
        </div>
      </CardHeader>
      <CardContent>
        <p className="text-muted-foreground mb-2 text-xs">
          Mutating actions performed via <code>/api/v2</code> (limit {limit}).
        </p>
        <Input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="filter by action / user / target…"
          className="mb-2 h-7 max-w-md text-xs"
        />
        {error ? <p className="text-destructive mb-2 text-xs">{error}</p> : null}
        {filtered.length === 0 ? (
          <p className="text-muted-foreground text-xs">
            {audit.length === 0 ? 'No entries yet.' : 'No entries match the filter.'}
          </p>
        ) : (
          <div className="max-h-[32rem] overflow-auto rounded-md border">
            <table className="w-full text-xs">
              <thead className="text-muted-foreground bg-muted/30 sticky top-0">
                <tr>
                  <th className="border-border border-b px-2 py-1 text-left">Time</th>
                  <th className="border-border border-b px-2 py-1 text-left">User</th>
                  <th className="border-border border-b px-2 py-1 text-left">Action</th>
                  <th className="border-border border-b px-2 py-1 text-left">Target</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((e) => (
                  <tr key={e.id} className="border-border border-b">
                    <td className="px-2 py-1 font-mono whitespace-nowrap">
                      {new Date(e.ts).toLocaleString()}
                    </td>
                    <td className="px-2 py-1">{e.username || '—'}</td>
                    <td className="px-2 py-1 font-mono">{e.action}</td>
                    <td className="px-2 py-1 font-mono break-all">{e.target || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
