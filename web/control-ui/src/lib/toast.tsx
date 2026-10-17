import { useCallback, useMemo, useState, type ReactNode } from 'react'

import { ToastContext, type ToastKind } from '@/lib/toastContext'
import { cn } from '@/lib/utils'

export type ToastItem = {
  id: number
  kind: ToastKind
  message: string
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const toast = useCallback((message: string, kind: ToastKind = 'info') => {
    const id = Date.now() + Math.random()
    setItems((prev) => [...prev, { id, kind, message }])
    window.setTimeout(() => {
      setItems((prev) => prev.filter((t) => t.id !== id))
    }, 4500)
  }, [])
  const value = useMemo(() => ({ toast }), [toast])
  return (
    <ToastContext.Provider value={value}>
      {children}
      <div
        className="pointer-events-none fixed right-3 bottom-3 z-50 flex max-w-sm flex-col gap-2"
        aria-live="polite"
      >
        {items.map((t) => (
          <div
            key={t.id}
            className={cn(
              'border-border bg-card pointer-events-auto rounded-md border px-3 py-2 text-xs shadow-md',
              t.kind === 'success' && 'border-success/40 bg-success/10 text-success',
              t.kind === 'error' && 'border-destructive/40 bg-destructive/10 text-destructive',
              t.kind === 'info' && 'text-foreground',
            )}
          >
            {t.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}
