import { useQuery } from '@tanstack/react-query';
import { Clock } from 'lucide-react';

import { RunSignalBadge, RunStatusBadge } from '@/components/runs/run-badges';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { apiClient } from '@/lib/api/client';
import type { PipelineRun } from '@/lib/api/types';
import { formatRunDate } from '@/lib/run-format';

const RECENT_RUN_LIMIT = 10;

function RunRow({ run }: { run: PipelineRun }) {
  return (
    <li className="flex items-center gap-3 rounded-lg border border-border bg-background p-3 transition-colors hover:border-primary/15 hover:bg-accent/45">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <p className="truncate text-sm font-medium">{run.ticker}</p>
          {run.signal ? <RunSignalBadge signal={run.signal} /> : null}
        </div>
        <p className="mt-1 flex items-center gap-1 font-mono text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
          <Clock className="size-3" />
          {formatRunDate(run.started_at)}
          {run.completed_at ? ` — ${formatRunDate(run.completed_at)}` : ''}
        </p>
      </div>
      <RunStatusBadge status={run.status} />
    </li>
  );
}

export function RecentRuns() {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['runs', 'dashboard-recent'],
    queryFn: () => apiClient.listRuns({ limit: RECENT_RUN_LIMIT }),
    refetchInterval: 15_000,
  });

  const runs = data?.data ?? [];

  return (
    <Card data-testid="recent-runs">
      <CardHeader>
        <CardTitle>Recent runs</CardTitle>
        <CardDescription>Latest pipeline executions across strategies</CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-3" data-testid="recent-runs-loading">
            {Array.from({ length: 3 }).map((_, i) => (
              <div
                key={i}
                className="flex items-center gap-3 rounded-lg border border-border bg-background p-3"
              >
                <div className="h-4 w-24 animate-pulse rounded bg-muted" />
                <div className="ml-auto h-5 w-16 animate-pulse rounded-full bg-muted" />
              </div>
            ))}
          </div>
        ) : isError ? (
          <p className="text-sm text-muted-foreground" data-testid="recent-runs-error">
            Unable to load recent runs.
          </p>
        ) : runs.length === 0 ? (
          <div
            className="flex flex-col items-center gap-2 rounded-lg border border-dashed border-border bg-background py-10 text-center"
            data-testid="recent-runs-empty"
          >
            <Clock className="size-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">No runs yet</p>
          </div>
        ) : (
          <ul className="space-y-2" data-testid="recent-runs-list">
            {runs.map((run) => (
              <RunRow key={run.id} run={run} />
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
