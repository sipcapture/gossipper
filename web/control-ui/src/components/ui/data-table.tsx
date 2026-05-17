import { type ReactNode } from 'react'

import { cn } from '@/lib/utils'

export type Column<T> = {
  key: string
  header: ReactNode
  /** Render the column for a row. Receives the original row. */
  render: (row: T) => ReactNode
  /** Optional className applied to the <td>. */
  className?: string
  /** Right align (common for numeric/action columns). */
  align?: 'left' | 'right' | 'center'
}

export type DataTableProps<T> = {
  rows: T[]
  columns: Column<T>[]
  rowKey: (row: T) => string
  empty?: ReactNode
  loading?: boolean
  onRowClick?: (row: T) => void
}

export function DataTable<T>({ rows, columns, rowKey, empty, loading, onRowClick }: DataTableProps<T>) {
  return (
    <div className="border-border overflow-hidden rounded-md border">
      <table className="w-full border-collapse text-sm">
        <thead className="bg-muted/40 text-muted-foreground">
          <tr>
            {columns.map((c) => (
              <th
                key={c.key}
                className={cn(
                  'border-border border-b px-3 py-2 text-left text-xs font-medium uppercase tracking-wide',
                  c.align === 'right' && 'text-right',
                  c.align === 'center' && 'text-center',
                )}
              >
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {loading ? (
            <tr>
              <td colSpan={columns.length} className="text-muted-foreground px-3 py-6 text-center text-xs">
                Loading…
              </td>
            </tr>
          ) : rows.length === 0 ? (
            <tr>
              <td colSpan={columns.length} className="text-muted-foreground px-3 py-6 text-center text-xs">
                {empty ?? 'No rows'}
              </td>
            </tr>
          ) : (
            rows.map((row) => (
              <tr
                key={rowKey(row)}
                onClick={onRowClick ? () => onRowClick(row) : undefined}
                className={cn(
                  'border-border border-b last:border-b-0',
                  onRowClick && 'hover:bg-muted/40 cursor-pointer',
                )}
              >
                {columns.map((c) => (
                  <td
                    key={c.key}
                    className={cn(
                      'px-3 py-2 align-middle',
                      c.align === 'right' && 'text-right',
                      c.align === 'center' && 'text-center',
                      c.className,
                    )}
                  >
                    {c.render(row)}
                  </td>
                ))}
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  )
}
