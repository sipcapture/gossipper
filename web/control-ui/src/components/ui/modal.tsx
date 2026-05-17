import { useEffect, type ReactNode } from 'react'

export type ModalProps = {
  open: boolean
  onClose: () => void
  title: string
  description?: string
  children: ReactNode
  footer?: ReactNode
  size?: 'sm' | 'md' | 'lg' | 'xl'
}

const SIZE: Record<NonNullable<ModalProps['size']>, string> = {
  sm: 'max-w-md',
  md: 'max-w-xl',
  lg: 'max-w-3xl',
  xl: 'max-w-5xl',
}

export function Modal({ open, onClose, title, description, children, footer, size = 'md' }: ModalProps) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/40" onClick={onClose} aria-hidden="true" />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className={`bg-card text-card-foreground border-border relative z-10 m-4 flex max-h-[90vh] w-full flex-col overflow-hidden rounded-lg border shadow-xl ${SIZE[size]}`}
      >
        <header className="border-border flex shrink-0 items-start justify-between gap-4 border-b px-4 py-3">
          <div className="min-w-0">
            <h2 className="text-sm font-semibold tracking-tight">{title}</h2>
            {description ? (
              <p className="text-muted-foreground mt-0.5 text-xs">{description}</p>
            ) : null}
          </div>
          <button
            type="button"
            onClick={onClose}
            className="text-muted-foreground hover:text-foreground -mr-1 -mt-1 rounded-md px-2 py-1 text-xs"
            aria-label="Close"
          >
            ✕
          </button>
        </header>
        <div className="min-h-0 flex-1 overflow-auto px-4 py-3">{children}</div>
        {footer ? (
          <footer className="border-border flex shrink-0 items-center justify-end gap-2 border-t bg-muted/30 px-4 py-3">
            {footer}
          </footer>
        ) : null}
      </div>
    </div>
  )
}
