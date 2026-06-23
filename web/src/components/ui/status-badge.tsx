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
        status === 'running' && 'motion-safe:animate-pulse',
        className,
      )}
    >
      <span aria-hidden="true">{config.icon}</span>
      <span>{label ?? config.label}</span>
    </span>
  )
}
