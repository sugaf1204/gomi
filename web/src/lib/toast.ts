type ToastTone = 'error' | 'success' | 'info'

type ToastDetail = {
  tone: ToastTone
  message: string
}

export function notifyToast(detail: ToastDetail) {
  window.dispatchEvent(new CustomEvent('gomi:toast', { detail }))
}

export function notifyError(message: string) {
  notifyToast({ tone: 'error', message })
}
