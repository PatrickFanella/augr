import { cn } from '@/lib/utils'
import { statusConfig, type AppStatus } from '@/lib/status'

export type StatusBadgeProps = {
  status: AppStatus
  label?: string
  className?: string
}

export function StatusBadge({ status, label, className }: StatusBadgeProps) {
  const config = statusConfig[status]
  return (
    <span
      className={cn(
        'status-pill',
        config.pillClass,
        className,
      )}
    >
      {label ?? config.label}
    </span>
  )
}
