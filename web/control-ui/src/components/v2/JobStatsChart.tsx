import { useMemo } from 'react'

import type { StatsPoint } from '@/lib/jobsLive'

export type JobStatsChartProps = {
  points: StatsPoint[]
  height?: number
}

export function JobStatsChart({ points, height = 48 }: JobStatsChartProps) {
  const { path, max } = useMemo(() => {
    if (points.length === 0) return { path: '', max: 1 }
    const values = points.map((p) => p.value)
    const maxV = Math.max(1, ...values)
    const w = 200
    const h = height - 4
    const step = points.length <= 1 ? w : w / (points.length - 1)
    const coords = points.map((p, i) => {
      const x = i * step
      const y = h - (p.value / maxV) * h + 2
      return `${x},${y}`
    })
    return { path: coords.join(' '), max: maxV }
  }, [points, height])

  if (points.length === 0) {
    return <p className="text-muted-foreground text-[10px]">No stats samples yet.</p>
  }

  return (
    <div className="flex items-end gap-2">
      <svg viewBox={`0 0 200 ${height}`} className="bg-muted/30 h-12 w-full max-w-md rounded border">
        <polyline
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          className="text-primary"
          points={path}
        />
      </svg>
      <span className="text-muted-foreground text-[10px] whitespace-nowrap">max {max}</span>
    </div>
  )
}
