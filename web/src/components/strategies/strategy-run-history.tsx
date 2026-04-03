import { useQuery } from '@tanstack/react-query';
import { Clock } from 'lucide-react';

import { RunSignalBadge, RunStatusBadge } from '@/components/runs/run-badges';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { apiClient } from '@/lib/api/client';
import type { PipelineRun, UUID } from '@/lib/api/types';
import { formatRunDate } from '@/lib/run-format';

function RunRow({ run }: { run: PipelineRun }) {
  return (
    <li className="flex items-center gap-3 rounded-lg border border-white/8 bg-background/45 p-3 transition-colors hover:border-primary/15 hover:bg-accent/45">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <p className="truncate text-sm font-medium">{run.ticker}</p>
          {run.signal ? <RunSignalBadge signal={run.signal} /> : null}
        </div>
        <p className="flex items-center gap-1 font-mono text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
          <Clock className="size-3" />
          {formatRunDate(run.started_at)}
          {run.completed_at ? ` — ${formatRunDate(run.completed_at)}` : ''}
        </p>
      </div>
      <RunStatusBadge status={run.status} />
    </li>
  );
}

interface StrategyRunHistoryProps {
  strategyId: UUID;
}

export function StrategyRunHistory({ strategyId }: StrategyRunHistoryProps) {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['runs', { strategy_id: strategyId }],
    queryFn: () => apiClient.listRuns({ strategy_id: strategyId, limit: 20 }),
    refetchInterval: 15_000,
  });
  const runs = data?.data ?? [];

  return (
    <Card data-testid="strategy-run-history">
      <CardHeader>
        <CardTitle>Run history</CardTitle>
        <CardDescription>Recent pipeline executions</CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-3" data-testid="run-history-loading">
            {Array.from({ length: 3 }).map((_, i) => (
              <div
                key={i}
                className="flex items-center gap-3 rounded-lg border border-white/8 bg-background/45 p-3"
              >
                <div className="h-4 w-32 animate-pulse rounded bg-muted" />
                <div className="ml-auto h-5 w-16 animate-pulse rounded-full bg-muted" />
              </div>
            ))}
          </div>
        ) : isError ? (
          <p className="text-sm text-muted-foreground" data-testid="run-history-error">
            Unable to load run history.
          </p>
        ) : runs.length === 0 ? (
          <div
            className="flex flex-col items-center gap-2 rounded-lg border border-dashed border-white/10 bg-background/35 py-10 text-center"
            data-testid="run-history-empty"
          >
            <Clock className="size-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">No runs yet</p>
          </div>
        ) : (
          <ul className="space-y-2" data-testid="run-history-list">
            {runs.map((run) => (
              <RunRow key={run.id} run={run} />
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
