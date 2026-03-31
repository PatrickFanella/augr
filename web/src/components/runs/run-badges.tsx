import { AlertCircle, CheckCircle2, Loader2, XCircle } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import type { PipelineSignal, PipelineStatus } from '@/lib/api/types'

type BadgeVariant = 'default' | 'secondary' | 'success' | 'destructive' | 'warning'

interface StatusInfo {
  icon: typeof CheckCircle2
  label: string
  variant: BadgeVariant
}

const statusConfig: Record<PipelineStatus, StatusInfo> = {
  completed: { icon: CheckCircle2, label: 'Completed', variant: 'success' },
  running: { icon: Loader2, label: 'Running', variant: 'default' },
  failed: { icon: XCircle, label: 'Failed', variant: 'destructive' },
  cancelled: { icon: AlertCircle, label: 'Cancelled', variant: 'warning' },
}

const signalVariant: Record<PipelineSignal, Exclude<BadgeVariant, 'default' | 'warning'>> = {
  buy: 'success',
  sell: 'destructive',
  hold: 'secondary',
}

export function RunStatusBadge({ status }: { status: PipelineStatus }) {
  const config = statusConfig[status]
  const Icon = config.icon

  return (
    <Badge variant={config.variant} className="gap-1">
      <Icon className={`size-3 ${status === 'running' ? 'animate-spin' : ''}`} />
      {config.label}
    </Badge>
  )
}

export function RunSignalBadge({ signal }: { signal: PipelineSignal }) {
  return <Badge variant={signalVariant[signal]}>{signal}</Badge>
}
