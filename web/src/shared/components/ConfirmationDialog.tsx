import { useEffect, useId, useRef, type ReactNode } from 'react'

type ConfirmationDialogProps = {
  open: boolean
  title: string
  confirmLabel: string
  cancelLabel?: string
  tone?: 'warning' | 'danger'
  busy?: boolean
  disableDismiss?: boolean
  error?: ReactNode
  children: ReactNode
  onConfirm: () => void
  onCancel: () => void
}

export function ConfirmationDialog({
  open,
  title,
  confirmLabel,
  cancelLabel = 'Cancel',
  tone = 'warning',
  busy = false,
  disableDismiss = false,
  error,
  children,
  onConfirm,
  onCancel,
}: ConfirmationDialogProps) {
  const titleId = useId()
  const descriptionId = useId()
  const dialogRef = useRef<HTMLElement | null>(null)
  const cancelRef = useRef<HTMLButtonElement | null>(null)
  const confirmRef = useRef<HTMLButtonElement | null>(null)

  useEffect(() => {
    if (!open) return
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    queueMicrotask(() => cancelRef.current?.focus())
    return () => previous?.focus()
  }, [open])

  useEffect(() => {
    if (!open) return
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape' && !disableDismiss && !busy) onCancel()
      if (event.key !== 'Tab') return
      const focusables = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>('a[href], button, input, textarea, select, [tabindex]:not([tabindex="-1"])') ?? [])
        .filter((item) => !item.hasAttribute('disabled') && item.getAttribute('aria-hidden') !== 'true')
      if (focusables.length === 0) return
      const first = focusables[0]
      const last = focusables.at(-1)!
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [busy, disableDismiss, onCancel, open])

  if (!open) return null

  return (
    <div className="dialog-backdrop" onMouseDown={() => { if (!disableDismiss && !busy) onCancel() }}>
      <section
        ref={dialogRef}
        className={`confirmation-dialog ${tone}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={descriptionId}
        onMouseDown={(event) => event.stopPropagation()}
      >
        <h2 id={titleId}>{title}</h2>
        <div id={descriptionId}>{children}</div>
        {error ? <div role="alert" className="error-box">{error}</div> : null}
        <div className="dialog-actions">
          <button ref={cancelRef} type="button" onClick={onCancel} disabled={busy && disableDismiss}>{cancelLabel}</button>
          <button ref={confirmRef} type="button" className="danger-button" onClick={onConfirm} disabled={busy}>
            {busy ? 'Working…' : confirmLabel}
          </button>
        </div>
      </section>
    </div>
  )
}
