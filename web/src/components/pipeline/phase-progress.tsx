import { CheckCircle2, Circle, Loader2 } from 'lucide-react';

import { cn } from '@/lib/utils';

export type PhaseStatus = 'pending' | 'active' | 'completed';

export interface PhaseInfo {
  label: string;
  status: PhaseStatus;
  latencyMs?: number;
}

function formatLatency(ms: number) {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function PhaseIcon({ status }: { status: PhaseStatus }) {
  switch (status) {
    case 'completed':
      return <CheckCircle2 className="size-5 text-primary" />;
    case 'active':
      return <Loader2 className="size-5 animate-spin text-primary" />;
    default:
      return <Circle className="size-5 text-muted-foreground" />;
  }
}

interface PhaseProgressProps {
  phases: PhaseInfo[];
}

export function PhaseProgress({ phases }: PhaseProgressProps) {
  return (
    <section
      className="rounded-lg border border-border bg-card px-4 py-3"
      data-testid="phase-progress"
    >
      <div className="flex flex-wrap items-center gap-2 sm:gap-3">
        {phases.map((phase, index) => (
          <div key={phase.label} className="flex items-center gap-2 sm:gap-3">
            {index > 0 && (
              <div
                className={cn(
                  'h-px w-4 sm:w-8',
                  phases[index - 1].status !== 'pending' ? 'bg-primary/60' : 'bg-white/10',
                )}
              />
            )}
            <div className="flex min-w-19 flex-col items-center gap-1.5 rounded-md border border-border bg-background px-2 py-2 text-center">
              <PhaseIcon status={phase.status} />
              <span
                className={cn(
                  'font-mono text-[11px] font-medium uppercase tracking-[0.16em]',
                  phase.status === 'pending' ? 'text-muted-foreground' : 'text-foreground',
                )}
              >
                {phase.label}
              </span>
              {phase.latencyMs !== undefined && (
                <span className="font-mono text-[10px] text-muted-foreground">
                  {formatLatency(phase.latencyMs)}
                </span>
              )}
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
