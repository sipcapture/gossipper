import type { ProfileRuntime } from '@/api/v2'

const STATUS_STYLES: Record<string, { label: string; cls: string }> = {
  'built-in': {
    label: 'built-in',
    cls: 'bg-amber-100 text-amber-900 dark:bg-amber-900/40 dark:text-amber-100',
  },
  running: {
    label: 'running',
    cls: 'bg-emerald-100 text-emerald-900 dark:bg-emerald-900/40 dark:text-emerald-100',
  },
  pending: {
    label: 'pending',
    cls: 'bg-sky-100 text-sky-900 dark:bg-sky-900/40 dark:text-sky-100',
  },
  idle: {
    label: 'idle',
    cls: 'bg-muted text-foreground/70',
  },
  succeeded: {
    label: 'succeeded',
    cls: 'bg-emerald-50 text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-200',
  },
  failed: {
    label: 'failed',
    cls: 'bg-red-100 text-red-900 dark:bg-red-900/40 dark:text-red-100',
  },
  stopped: {
    label: 'stopped',
    cls: 'bg-slate-200 text-slate-800 dark:bg-slate-700/40 dark:text-slate-100',
  },
}

export function RuntimeBadge({ runtime }: { runtime?: ProfileRuntime }) {
  const status = runtime?.status ?? 'idle'
  const style = STATUS_STYLES[status] ?? STATUS_STYLES.idle
  const parts: string[] = []
  if (runtime?.job_id) parts.push(`job ${runtime.job_id.slice(0, 8)}`)
  if (runtime?.pid) parts.push(`pid ${runtime.pid}`)
  if (runtime?.exit_code !== undefined) parts.push(`exit ${runtime.exit_code}`)
  if (runtime?.started_at) parts.push(`started ${formatRelative(runtime.started_at)}`)
  const tooltip = parts.length > 0 ? parts.join(' · ') : undefined
  return (
    <span
      title={tooltip}
      className={`inline-block rounded px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wider ${style.cls}`}
    >
      {style.label}
    </span>
  )
}

function formatRelative(iso: string): string {
  const ts = Date.parse(iso)
  if (!Number.isFinite(ts)) return iso
  const dt = Date.now() - ts
  if (dt < 60_000) return `${Math.max(1, Math.round(dt / 1000))}s ago`
  if (dt < 3_600_000) return `${Math.round(dt / 60_000)}m ago`
  if (dt < 86_400_000) return `${Math.round(dt / 3_600_000)}h ago`
  return `${Math.round(dt / 86_400_000)}d ago`
}
