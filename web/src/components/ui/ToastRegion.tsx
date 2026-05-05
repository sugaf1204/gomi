import { useEffect } from 'react'
import clsx from 'clsx'

export type ToastTone = 'error' | 'info'

export type ToastItem = {
  id: string
  message: string
  tone?: ToastTone
}

type Props = {
  toasts: ToastItem[]
  onDismiss: (id: string) => void
}

type ToastCardProps = {
  toast: ToastItem
  onDismiss: (id: string) => void
}

function ToastCard({ toast, onDismiss }: ToastCardProps) {
  useEffect(() => {
    const timerId = window.setTimeout(() => onDismiss(toast.id), 4200)
    return () => window.clearTimeout(timerId)
  }, [toast.id, onDismiss])

  return (
    <article className={clsx('toast-item', toast.tone === 'info' && 'toast-item-info')} role="status">
      <p className="m-0 text-[0.84rem] leading-[1.35]">{toast.message}</p>
      <button
        type="button"
        className="toast-close"
        onClick={() => onDismiss(toast.id)}
        aria-label="Close"
      >
        x
      </button>
    </article>
  )
}

export function ToastRegion({ toasts, onDismiss }: Props) {
  if (toasts.length === 0) return null

  return (
    <section className="toast-region" aria-live="polite" aria-atomic="true">
      {toasts.map((toast) => (
        <ToastCard key={toast.id} toast={toast} onDismiss={onDismiss} />
      ))}
    </section>
  )
}
