import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertTriangle, CheckCircle2, Power, Shield, XCircle } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { apiClient } from '@/lib/api/client';
import type { EngineStatus, RiskStatus } from '@/lib/api/types';
import { cn } from '@/lib/utils';

function riskStatusConfig(status: RiskStatus) {
  switch (status) {
    case 'normal':
      return {
        icon: CheckCircle2,
        label: 'Normal',
        variant: 'success' as const,
      };
    case 'warning':
      return {
        icon: AlertTriangle,
        label: 'Warning',
        variant: 'warning' as const,
      };
    case 'breached':
      return {
        icon: XCircle,
        label: 'Breached',
        variant: 'destructive' as const,
      };
  }
}

function CircuitBreakerDisplay({ status }: { status: EngineStatus }) {
  const { circuit_breaker: cb } = status;

  const stateLabels: Record<string, string> = {
    open: 'Open',
    tripped: 'Tripped',
    cooldown: 'Cooldown',
  };

  const stateVariants: Record<string, 'success' | 'destructive' | 'warning'> = {
    open: 'success',
    tripped: 'destructive',
    cooldown: 'warning',
  };

  return (
    <div className="rounded-lg border border-white/8 bg-background/45 p-3">
      <div className="flex items-center justify-between">
        <p className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
          Circuit breaker
        </p>
        <Badge variant={stateVariants[cb.state] ?? 'secondary'}>
          {stateLabels[cb.state] ?? cb.state}
        </Badge>
      </div>
      {cb.reason ? <p className="mt-2 text-sm text-muted-foreground">{cb.reason}</p> : null}
    </div>
  );
}

function PositionLimitsDisplay({ status }: { status: EngineStatus }) {
  const { position_limits: limits } = status;

  const items = [
    { label: 'Per position', value: `${limits.max_per_position_pct}%` },
    { label: 'Total exposure', value: `${limits.max_total_pct}%` },
    { label: 'Max concurrent', value: String(limits.max_concurrent) },
    { label: 'Per market', value: `${limits.max_per_market_pct}%` },
  ];

  return (
    <div className="rounded-lg border border-white/8 bg-background/45 p-3">
      <p className="mb-3 font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
        Position limits
      </p>
      <div className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
        {items.map(({ label, value }) => (
          <div key={label} className="flex justify-between">
            <span className="text-muted-foreground">{label}</span>
            <span className="font-mono font-medium text-foreground">{value}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

export function RiskStatusBar() {
  const queryClient = useQueryClient();

  const { data, isLoading, isError } = useQuery({
    queryKey: ['risk', 'status'],
    queryFn: () => apiClient.getRiskStatus(),
    refetchInterval: 15_000,
  });

  const killSwitchMutation = useMutation({
    mutationFn: (active: boolean) =>
      apiClient.toggleKillSwitch({
        active,
        reason: active ? 'Activated from dashboard' : 'Deactivated from dashboard',
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['risk', 'status'] });
    },
  });

  if (isLoading) {
    return (
      <Card data-testid="risk-status-loading">
        <CardHeader className="flex flex-row items-center justify-between pb-2">
          <div className="h-4 w-32 animate-pulse rounded bg-muted" />
          <div className="size-4 animate-pulse rounded bg-muted" />
        </CardHeader>
        <CardContent>
          <div className="space-y-3">
            <div className="h-16 animate-pulse rounded bg-muted" />
            <div className="h-16 animate-pulse rounded bg-muted" />
          </div>
        </CardContent>
      </Card>
    );
  }

  if (isError || !data) {
    return (
      <Card data-testid="risk-status-error">
        <CardHeader>
          <CardTitle>Risk status</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">
            Unable to load risk status. Start the API server to see live data.
          </p>
        </CardContent>
      </Card>
    );
  }

  const config = riskStatusConfig(data.risk_status);
  const StatusIcon = config.icon;

  return (
    <Card data-testid="risk-status">
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="flex items-center gap-2">
              <Shield className="size-5" />
              Risk status
            </CardTitle>
            <CardDescription>Engine health and risk controls</CardDescription>
          </div>
          <Badge variant={config.variant} className="gap-1">
            <StatusIcon className="size-3" />
            {config.label}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <CircuitBreakerDisplay status={data} />
        <PositionLimitsDisplay status={data} />

        <div className="rounded-lg border border-white/8 bg-background/45 p-3">
          <div className="flex items-center justify-between">
            <div>
              <p className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
                Kill switch
              </p>
              <p className="mt-2 text-sm text-muted-foreground">
                {data.kill_switch.active
                  ? (data.kill_switch.reason && data.kill_switch.reason.trim()) ||
                    'All trading halted'
                  : 'Trading is enabled'}
              </p>
            </div>
            <Button
              variant={data.kill_switch.active ? 'outline' : 'default'}
              size="dense"
              disabled={killSwitchMutation.isPending}
              onClick={() => killSwitchMutation.mutate(!data.kill_switch.active)}
              data-testid="kill-switch-toggle"
            >
              <Power className={cn('size-4', data.kill_switch.active && 'text-destructive')} />
              {data.kill_switch.active ? 'Deactivate' : 'Activate'}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
