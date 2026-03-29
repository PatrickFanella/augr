import { CheckCircle2, Circle, Loader2 } from 'lucide-react'

import { cn } from '@/lib/utils'

export type PhaseStatus = 'pending' | 'active' | 'completed'

export interface PhaseInfo {
  label: string
  status: PhaseStatus
  latencyMs?: number
}

function formatLatency(ms: number) {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function PhaseIcon({ status }: { status: PhaseStatus }) {
  switch (status) {
    case 'completed':
      return <CheckCircle2 className="size-5 text-primary" />
    case 'active':
      return <Loader2 className="size-5 animate-spin text-primary" />
    default:
      return <Circle className="size-5 text-muted-foreground" />
  }
}

interface PhaseProgressProps {
  phases: PhaseInfo[]
}

export function PhaseProgress({ phases }: PhaseProgressProps) {
  return (
    <div className="flex items-center gap-2" data-testid="phase-progress">
      {phases.map((phase, index) => (
        <div key={phase.label} className="flex items-center gap-2">
          {index > 0 && (
            <div
              className={cn(
                'h-0.5 w-6 sm:w-10',
                phases[index - 1].status !== 'pending' ? 'bg-primary' : 'bg-muted',
              )}
            />
          )}
          <div className="flex flex-col items-center gap-1">
            <PhaseIcon status={phase.status} />
            <span
              className={cn(
                'text-xs font-medium',
                phase.status === 'pending' ? 'text-muted-foreground' : 'text-foreground',
              )}
            >
              {phase.label}
            </span>
            {phase.latencyMs !== undefined && (
              <span className="text-[10px] text-muted-foreground">
                {formatLatency(phase.latencyMs)}
              </span>
            )}
          </div>
        </div>
      ))}
    </div>
  )
}
