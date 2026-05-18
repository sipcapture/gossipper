import type { JobTimelineBucket } from '@/lib/jobsLive'

export type JobTimelineChartProps = {
  buckets: JobTimelineBucket[]
}

export function JobTimelineChart({ buckets }: JobTimelineChartProps) {
  const max = Math.max(1, ...buckets.map((b) => b.succeeded + b.failed))
  return (
    <div className="flex flex-col gap-1">
      <div className="text-muted-foreground text-[10px]">Job outcomes by hour (24h, UTC-local display)</div>
      <div className="flex h-16 items-end gap-px">
        {buckets.map((b, i) => {
          const total = b.succeeded + b.failed
          const hOk = (b.succeeded / max) * 100
          const hFail = (b.failed / max) * 100
          return (
            <div
              key={i}
              className="bg-muted/40 flex min-w-0 flex-1 flex-col justify-end gap-px"
              title={`ok ${b.succeeded} / fail ${b.failed}`}
            >
              {hFail > 0 ? (
                <div className="bg-destructive/70 w-full" style={{ height: `${hFail}%`, minHeight: total ? 2 : 0 }} />
              ) : null}
              {hOk > 0 ? (
                <div className="bg-success/70 w-full" style={{ height: `${hOk}%`, minHeight: total ? 2 : 0 }} />
              ) : null}
            </div>
          )
        })}
      </div>
      <div className="text-muted-foreground flex justify-between text-[9px]">
        <span>24h ago</span>
        <span>now</span>
      </div>
    </div>
  )
}
