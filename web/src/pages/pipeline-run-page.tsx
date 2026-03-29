import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { AnalystCards } from '@/components/pipeline/analyst-cards'
import { DebateView } from '@/components/pipeline/debate-view'
import { DecisionInspector } from '@/components/pipeline/decision-inspector'
import { FinalSignal } from '@/components/pipeline/final-signal'
import { type PhaseInfo, PhaseProgress } from '@/components/pipeline/phase-progress'
import { TraderPlan } from '@/components/pipeline/trader-plan'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { apiClient } from '@/lib/api/client'
import type { AgentDecision, AgentRole, WebSocketMessage, WebSocketServerMessage } from '@/lib/api/types'
import { useWebSocketClient } from '@/hooks/use-websocket-client'

const analysisRoles: AgentRole[] = [
  'market_analyst',
  'fundamentals_analyst',
  'news_analyst',
  'social_media_analyst',
]
const debateRoles: AgentRole[] = ['bull_researcher', 'bear_researcher']
const riskDebateRoles: AgentRole[] = ['aggressive_risk', 'conservative_risk', 'neutral_risk']

function computePhases(decisions: AgentDecision[], isCompleted: boolean): PhaseInfo[] {
  const analysisDecisions = decisions.filter((d) => analysisRoles.includes(d.agent_role))
  const debateDecisions = decisions.filter((d) => debateRoles.includes(d.agent_role))
  const traderDecision = decisions.find((d) => d.agent_role === 'trader')
  const riskDecisions = decisions.filter((d) => riskDebateRoles.includes(d.agent_role))
  const judgeDecision = decisions.find((d) => d.agent_role === 'invest_judge')

  function phaseLatency(phaseDecisions: AgentDecision[]): number | undefined {
    if (phaseDecisions.length === 0) return undefined
    return Math.max(...phaseDecisions.map((d) => d.latency_ms ?? 0))
  }

  function status(done: boolean, hasAny: boolean): PhaseInfo['status'] {
    if (done) return 'completed'
    if (hasAny) return 'active'
    return 'pending'
  }

  const analysisDone =
    new Set(analysisDecisions.map((d) => d.agent_role)).size >= analysisRoles.length
  const debateDone =
    new Set(debateDecisions.map((d) => d.agent_role)).size >= debateRoles.length
  const traderDone = !!traderDecision
  const riskDone =
    new Set(riskDecisions.map((d) => d.agent_role)).size >= riskDebateRoles.length
  const judgeDone = !!judgeDecision

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
      status: status(traderDone, false),
      latencyMs: traderDecision?.latency_ms,
    },
    {
      label: 'Risk',
      status: status(riskDone, riskDecisions.length > 0),
      latencyMs: phaseLatency(riskDecisions),
    },
    {
      label: 'Signal',
      status: status(judgeDone || isCompleted, false),
      latencyMs: judgeDecision?.latency_ms,
    },
  ]
}

function isWebSocketMessage(msg: WebSocketServerMessage): msg is WebSocketMessage {
  return 'type' in msg && !('status' in msg)
}

export function PipelineRunPage() {
  const { id } = useParams<{ id: string }>()
  const queryClient = useQueryClient()
  const [selectedDecision, setSelectedDecision] = useState<AgentDecision | null>(null)
  const subscribedRef = useRef(false)

  const {
    data: run,
    isLoading: runLoading,
    isError: runError,
  } = useQuery({
    queryKey: ['run', id],
    queryFn: () => apiClient.getRun(id!),
    enabled: !!id,
    refetchInterval: (query) => {
      const status = query.state.data?.status
      return status === 'running' ? 3_000 : false
    },
  })

  const { data: decisionsData } = useQuery({
    queryKey: ['run-decisions', id],
    queryFn: () => apiClient.getRunDecisions(id!, { limit: 10_000 }),
    enabled: !!id,
    refetchInterval: () => {
      return run?.status === 'running' ? 3_000 : false
    },
  })

  const decisions = useMemo(() => decisionsData?.data ?? [], [decisionsData])

  const handleWebSocketMessage = useCallback(
    (msg: WebSocketServerMessage) => {
      if (!isWebSocketMessage(msg)) return
      if (msg.run_id !== id) return

      queryClient.invalidateQueries({ queryKey: ['run', id] })
      queryClient.invalidateQueries({ queryKey: ['run-decisions', id] })
    },
    [id, queryClient],
  )

  const { status: wsStatus, subscribe } = useWebSocketClient({
    enabled: run?.status === 'running',
    onMessage: handleWebSocketMessage,
  })

  const isWsConnected = wsStatus === 'open'

  useEffect(() => {
    if (isWsConnected && !subscribedRef.current && id) {
      subscribe({ run_ids: [id] })
      subscribedRef.current = true
    }
    if (!isWsConnected) {
      subscribedRef.current = false
    }
  }, [isWsConnected, subscribe, id])

  const traderDecision = useMemo(() => {
    for (let i = decisions.length - 1; i >= 0; i--) {
      if (decisions[i].agent_role === 'trader') return decisions[i]
    }
    return undefined
  }, [decisions])

  const judgeDecision = useMemo(() => {
    for (let i = decisions.length - 1; i >= 0; i--) {
      if (decisions[i].agent_role === 'invest_judge') return decisions[i]
    }
    return undefined
  }, [decisions])

  const phases = useMemo(
    () => computePhases(decisions, run?.status === 'completed'),
    [decisions, run?.status],
  )

  if (runLoading) {
    return (
      <div className="space-y-6" data-testid="pipeline-run-loading">
        <div className="h-8 w-48 animate-pulse rounded bg-muted" />
        <div className="h-64 animate-pulse rounded-lg border bg-muted" />
      </div>
    )
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
    )
  }

  return (
    <div className="space-y-6" data-testid="pipeline-run-page">
      <div className="flex items-center justify-between">
        <div className="space-y-1">
          <Link
            to="/runs"
            className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
          >
            <ArrowLeft className="size-4" />
            Back to runs
          </Link>
          <h2 className="text-2xl font-semibold tracking-tight">
            {run.ticker} — Pipeline Run
          </h2>
          <div className="flex items-center gap-2">
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
            <span className="text-xs text-muted-foreground">
              {new Date(run.started_at).toLocaleString()}
            </span>
          </div>
        </div>
      </div>

      <PhaseProgress phases={phases} />

      <div className="space-y-6">
        <AnalystCards decisions={decisions} onSelectDecision={setSelectedDecision} />

        <DebateView
          title="Phase 2 — Bull vs Bear Debate"
          roles={debateRoles}
          decisions={decisions}
          onSelectDecision={setSelectedDecision}
        />

        <TraderPlan decision={traderDecision} onSelectDecision={setSelectedDecision} />

        <DebateView
          title="Phase 4 — Risk Assessment"
          roles={riskDebateRoles}
          decisions={decisions}
          onSelectDecision={setSelectedDecision}
        />

        <FinalSignal
          signal={run.signal}
          judgeDecision={judgeDecision}
          onSelectDecision={setSelectedDecision}
        />
      </div>

      {selectedDecision && (
        <DecisionInspector
          decision={selectedDecision}
          onClose={() => setSelectedDecision(null)}
        />
      )}
    </div>
  )
}
