import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft } from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';

import { PageHeader } from '@/components/layout/page-header';
import { AnalystCards } from '@/components/pipeline/analyst-cards';
import { DebateView } from '@/components/pipeline/debate-view';
import { DecisionInspector } from '@/components/pipeline/decision-inspector';
import { FinalSignal } from '@/components/pipeline/final-signal';
import { type PhaseInfo, PhaseProgress } from '@/components/pipeline/phase-progress';
import { TraderPlan } from '@/components/pipeline/trader-plan';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent } from '@/components/ui/card';
import { apiClient } from '@/lib/api/client';
import type {
  AgentDecision,
  AgentRole,
  WebSocketMessage,
  WebSocketServerMessage,
} from '@/lib/api/types';
import { useWebSocketClient } from '@/hooks/use-websocket-client';

const analysisRoles: AgentRole[] = [
  'market_analyst',
  'fundamentals_analyst',
  'news_analyst',
  'social_media_analyst',
];
const debateRoles: AgentRole[] = ['bull_researcher', 'bear_researcher'];
const riskDebateRoles: AgentRole[] = [
  'aggressive_analyst',
  'conservative_analyst',
  'neutral_analyst',
];

const legacyRiskRoleMap: Partial<Record<AgentRole, AgentRole>> = {
  aggressive_risk: 'aggressive_analyst',
  conservative_risk: 'conservative_analyst',
  neutral_risk: 'neutral_analyst',
};

function normalizeAgentRole(role: AgentRole): AgentRole {
  return legacyRiskRoleMap[role] ?? role;
}

function normalizeDecision(decision: AgentDecision): AgentDecision {
  const normalizedRole = normalizeAgentRole(decision.agent_role);
  if (normalizedRole === decision.agent_role) {
    return decision;
  }

  return {
    ...decision,
    agent_role: normalizedRole,
  };
}

function getLatestDecision(
  decisions: AgentDecision[],
  roles: AgentRole[],
): AgentDecision | undefined {
  for (let i = decisions.length - 1; i >= 0; i--) {
    if (roles.includes(decisions[i].agent_role)) {
      return decisions[i];
    }
  }
  return undefined;
}

function computePhases(
  decisions: AgentDecision[],
  isCompleted: boolean,
  hasSignal: boolean,
): PhaseInfo[] {
  const analysisDecisions = decisions.filter((d) => analysisRoles.includes(d.agent_role));
  const debateDecisions = decisions.filter((d) => debateRoles.includes(d.agent_role));
  const traderDecision = getLatestDecision(decisions, ['trader']);
  const riskDecisions = decisions.filter((d) => riskDebateRoles.includes(d.agent_role));
  const signalDecision = getLatestDecision(decisions, ['risk_manager']);

  function phaseLatency(phaseDecisions: AgentDecision[]): number | undefined {
    if (phaseDecisions.length === 0) return undefined;
    return Math.max(...phaseDecisions.map((d) => d.latency_ms ?? 0));
  }

  function status(done: boolean, hasAny: boolean): PhaseInfo['status'] {
    if (done || (isCompleted && hasAny)) return 'completed';
    if (hasAny) return 'active';
    return 'pending';
  }

  const analysisDone =
    new Set(analysisDecisions.map((d) => d.agent_role)).size >= analysisRoles.length;
  const debateDone = new Set(debateDecisions.map((d) => d.agent_role)).size >= debateRoles.length;
  const traderDone = !!traderDecision;
  const riskDone = new Set(riskDecisions.map((d) => d.agent_role)).size >= riskDebateRoles.length;
  const signalDone = !!signalDecision || hasSignal;

  return [
    {
      label: 'Analysis',
      status: status(analysisDone, analysisDecisions.length > 0),
      latencyMs: phaseLatency(analysisDecisions),
    },
    {
      label: 'Debate',
      status: status(debateDone, debateDecisions.length > 0),
      latencyMs: phaseLatency(debateDecisions),
    },
    {
      label: 'Trading',
      status: status(traderDone, traderDone),
      latencyMs: traderDecision?.latency_ms,
    },
    {
      label: 'Risk',
      status: status(riskDone, riskDecisions.length > 0),
      latencyMs: phaseLatency(riskDecisions),
    },
    {
      label: 'Signal',
      status: status(signalDone, signalDone),
      latencyMs: signalDecision?.latency_ms,
    },
  ];
}

function isWebSocketMessage(msg: WebSocketServerMessage): msg is WebSocketMessage {
  return 'type' in msg && !('status' in msg);
}

export function PipelineRunPage() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();
  const [selectedDecision, setSelectedDecision] = useState<AgentDecision | null>(null);
  const subscribedRef = useRef(false);

  const {
    data: run,
    isLoading: runLoading,
    isError: runError,
  } = useQuery({
    queryKey: ['run', id],
    queryFn: () => apiClient.getRun(id!),
    enabled: !!id,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'running' ? 3_000 : false;
    },
  });

  const { data: decisionsData } = useQuery({
    queryKey: ['run-decisions', id],
    queryFn: () => apiClient.getRunDecisions(id!, { limit: 10_000 }),
    enabled: !!id,
    refetchInterval: () => {
      return run?.status === 'running' ? 3_000 : false;
    },
  });

  const decisions = useMemo(
    () => (Array.isArray(decisionsData?.data) ? decisionsData.data.map(normalizeDecision) : []),
    [decisionsData],
  );

  const handleWebSocketMessage = useCallback(
    (msg: WebSocketServerMessage) => {
      if (!isWebSocketMessage(msg)) return;
      if (msg.run_id !== id) return;

      queryClient.invalidateQueries({ queryKey: ['run', id] });
      queryClient.invalidateQueries({ queryKey: ['run-decisions', id] });
    },
    [id, queryClient],
  );

  const { status: wsStatus, subscribe } = useWebSocketClient({
    enabled: run?.status === 'running',
    onMessage: handleWebSocketMessage,
  });

  const isWsConnected = wsStatus === 'open';

  useEffect(() => {
    if (isWsConnected && !subscribedRef.current && id) {
      subscribe({ run_ids: [id] });
      subscribedRef.current = true;
    }
    if (!isWsConnected) {
      subscribedRef.current = false;
    }
  }, [isWsConnected, subscribe, id]);

  const traderDecision = useMemo(() => getLatestDecision(decisions, ['trader']), [decisions]);

  const signalDecision = useMemo(() => getLatestDecision(decisions, ['risk_manager']), [decisions]);

  const phases = useMemo(
    () => computePhases(decisions, run?.status === 'completed', Boolean(run?.signal)),
    [decisions, run?.signal, run?.status],
  );

  if (runLoading) {
    return (
      <div className="space-y-6" data-testid="pipeline-run-loading">
        <div className="h-8 w-48 animate-pulse rounded bg-muted" />
        <div className="h-64 animate-pulse rounded-lg border bg-muted" />
      </div>
    );
  }

  if (runError || !run) {
    return (
      <div className="space-y-4" data-testid="pipeline-run-error">
        <Link
          to="/runs"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="size-4" />
          Back to runs
        </Link>
        <Card>
          <CardContent className="py-8">
            <p className="text-center text-sm text-muted-foreground">
              Unable to load pipeline run. It may not exist or the API server is unavailable.
            </p>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="space-y-6" data-testid="pipeline-run-page">
      <PageHeader
        eyebrow="Run detail"
        title={`${run.ticker} pipeline run`}
        description="Replay the agent pipeline, inspect decisions by phase, and verify the final trading signal."
        meta={(
          <>
            <Badge
              variant={
                run.status === 'completed'
                  ? 'success'
                  : run.status === 'running'
                    ? 'default'
                    : run.status === 'failed'
                      ? 'destructive'
                      : 'warning'
              }
            >
              {run.status}
            </Badge>
            <span className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
              {new Date(run.started_at).toLocaleString()}
            </span>
          </>
        )}
        actions={(
          <Link
            to="/runs"
            className="inline-flex items-center gap-1 rounded-md border border-border bg-background px-3 py-2 text-sm text-muted-foreground transition-colors hover:border-primary/25 hover:text-foreground"
          >
            <ArrowLeft className="size-4" />
            Back to runs
          </Link>
        )}
      />

      <PhaseProgress phases={phases} />

      <div className="space-y-6">
        <AnalystCards
          decisions={decisions}
          onSelectDecision={setSelectedDecision}
          isCompleted={run.status === 'completed'}
        />

        <DebateView
          title="Phase 2 — Bull vs Bear Debate"
          roles={debateRoles}
          decisions={decisions}
          onSelectDecision={setSelectedDecision}
          isCompleted={run.status === 'completed'}
        />

        <TraderPlan decision={traderDecision} onSelectDecision={setSelectedDecision} />

        <DebateView
          title="Phase 4 — Risk Assessment"
          roles={riskDebateRoles}
          decisions={decisions}
          onSelectDecision={setSelectedDecision}
          isCompleted={run.status === 'completed'}
        />

        <FinalSignal
          signal={run.signal}
          signalDecision={signalDecision}
          onSelectDecision={setSelectedDecision}
        />
      </div>

      {selectedDecision && (
        <DecisionInspector decision={selectedDecision} onClose={() => setSelectedDecision(null)} />
      )}
    </div>
  );
}
