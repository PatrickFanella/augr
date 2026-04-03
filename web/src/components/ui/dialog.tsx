import { type HTMLAttributes, type MouseEvent, createContext, forwardRef, useCallback, useContext, useEffect, useId, useRef } from 'react'

import { cn } from '@/lib/utils'

interface DialogContextValue {
  titleId: string
  descriptionId: string
}

const DialogContext = createContext<DialogContextValue | null>(null)

interface DialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  children: React.ReactNode
}

export function Dialog({ open, onOpenChange, children }: DialogProps) {
  const dialogRef = useRef<HTMLDivElement>(null)
  const previousFocusRef = useRef<HTMLElement | null>(null)
  const titleId = useId()
  const descriptionId = useId()

  useEffect(() => {
    if (!open) return

    previousFocusRef.current = document.activeElement as HTMLElement | null

    // Move focus into the dialog
    requestAnimationFrame(() => {
      const firstFocusable = dialogRef.current?.querySelector<HTMLElement>(
        'input, button, textarea, select, [tabindex]:not([tabindex="-1"])',
      )
      firstFocusable?.focus()
    })

    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        onOpenChange(false)
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('keydown', handleKeyDown)
      // Restore focus on close
      previousFocusRef.current?.focus()
    }
  }, [open, onOpenChange])

  if (!open) return null

  return (
    <DialogContext.Provider value={{ titleId, descriptionId }}>
      <div className="fixed inset-0 z-50 flex items-center justify-center">
        <div
          className="fixed inset-0 bg-slate-950/80 backdrop-blur-sm"
          onClick={() => onOpenChange(false)}
          data-testid="dialog-overlay"
        />
        <div
          ref={dialogRef}
          role="dialog"
          aria-modal="true"
          aria-labelledby={titleId}
          aria-describedby={descriptionId}
          className="relative z-50 w-full max-w-lg"
        >
          {children}
        </div>
      </div>
    </DialogContext.Provider>
  )
}

const DialogContent = forwardRef<HTMLDivElement, HTMLAttributes<HTMLDivElement>>(
  ({ className, onClick, ...props }, ref) => {
    const handleClick = useCallback(
      (e: MouseEvent<HTMLDivElement>) => {
        e.stopPropagation()
        onClick?.(e)
      },
      [onClick],
    )

    return (
      <div
        ref={ref}
        className={cn(
          'mx-4 rounded-lg border border-white/10 bg-card/95 p-5 shadow-[0_0_0_1px_rgba(255,255,255,0.03),0_20px_48px_rgba(2,6,23,0.42)] backdrop-blur-lg sm:mx-0',
          className,
        )}
        onClick={handleClick}
        {...props}
      />
    )
  },
)

DialogContent.displayName = 'DialogContent'

function DialogHeader({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('mb-4 space-y-1.5', className)} {...props} />
}

function DialogTitle({ className, ...props }: HTMLAttributes<HTMLHeadingElement>) {
  const ctx = useContext(DialogContext)
  return <h2 id={ctx?.titleId} className={cn('text-lg font-semibold leading-none tracking-tight', className)} {...props} />
}

function DialogDescription({ className, ...props }: HTMLAttributes<HTMLParagraphElement>) {
  const ctx = useContext(DialogContext)
  return <p id={ctx?.descriptionId} className={cn('text-sm text-muted-foreground', className)} {...props} />
}

function DialogFooter({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('mt-6 flex justify-end gap-2', className)} {...props} />
}

export { DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter }
