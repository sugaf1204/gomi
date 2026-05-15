import { useRef, type ReactNode } from 'react'

type ModalOverlayProps = {
  children: ReactNode
  onBackdropClick: () => void
  className?: string
}

export function ModalOverlay({ children, onBackdropClick, className = 'fixed inset-0 bg-[rgba(30,28,24,0.38)] grid place-items-center z-20 p-4' }: ModalOverlayProps) {
  const pointerStartedOnBackdrop = useRef(false)

  return (
    <div
      className={className}
      role="dialog"
      aria-modal="true"
      onPointerDown={(event) => {
        pointerStartedOnBackdrop.current = event.target === event.currentTarget
      }}
      onPointerUp={(event) => {
        if (pointerStartedOnBackdrop.current && event.target === event.currentTarget) {
          onBackdropClick()
        }
        pointerStartedOnBackdrop.current = false
      }}
      onPointerCancel={() => {
        pointerStartedOnBackdrop.current = false
      }}
    >
      {children}
    </div>
  )
}
