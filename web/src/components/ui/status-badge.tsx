import { cn } from '@/lib/utils'
import { statusConfig, type AppStatus } from '@/lib/status'

export type StatusBadgeProps = {
  status: AppStatus
  label?: string
  className?: string
}

export function StatusBadge({ status, label, className }: StatusBadgeProps) {
  const config = statusConfig[status]
  const normalizedLabel = label?.trim().toLowerCase()
  const displayLabel = status === 'unknown'
    && label
    && !normalizedLabel?.startsWith('unknown:')
    && !['unknown', 'unavailable', 'not configured', 'not_configured', 'cancelled'].includes(normalizedLabel ?? '')
    ? `Unknown: ${label}`
    : label ?? config.label
  return (
    <span
      className={cn(
        'status-pill',
        config.pillClass,
        className,
      )}
    >
      {displayLabel}
    </span>
  )
}
