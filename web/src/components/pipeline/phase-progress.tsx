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
      className="rounded-lg border border-white/10 bg-card/80 px-4 py-3 shadow-[0_0_0_1px_rgba(255,255,255,0.02),0_14px_34px_rgba(2,6,23,0.28)] backdrop-blur-sm"
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
            <div className="flex min-w-19 flex-col items-center gap-1.5 rounded-md border border-white/6 bg-background/40 px-2 py-2 text-center">
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
