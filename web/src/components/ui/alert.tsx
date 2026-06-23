import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'
import { statusConfig, type AppStatus } from '@/lib/status'

export type AlertVariant = 'info' | 'success' | 'warning' | 'danger'

const variantToStatus: Record<AlertVariant, AppStatus> = {
  info: 'processing',
  success: 'success',
  warning: 'warning',
  danger: 'danger',
}

export type AlertProps = {
  variant?: AlertVariant
  title?: string
  children?: ReactNode
  className?: string
}

export function Alert({ variant = 'info', title, children, className }: AlertProps) {
  const config = statusConfig[variantToStatus[variant]]
  return (
    <div
      className={cn(
        'flex min-w-0 gap-3 rounded-md border-2 p-4',
        `border-[var(--color-${config.token})] bg-[var(--color-${config.token}-muted)] text-[var(--color-${config.token}-text)]`,
        className,
      )}
      role={variant === 'danger' ? 'alert' : 'status'}
    >
      <div className="font-mono text-lg font-bold" aria-hidden="true">
        {config.icon}
      </div>
      <div className="min-w-0">
        {title && <div className="font-mono text-sm font-bold">{title}</div>}
        {children && (
          <div className={cn('text-sm leading-6 text-[var(--color-text-secondary)]', title && 'mt-1')}>
            {children}
          </div>
        )}
      </div>
    </div>
  )
}
