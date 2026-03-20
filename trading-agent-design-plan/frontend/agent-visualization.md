---
title: "Agent Visualization"
date: 2026-03-20
tags: [frontend, agents, visualization, real-time, pipeline]
---

# Agent Visualization

Real-time visualization of the agent pipeline as it executes. Shows which agents are active, debate progress, and the final signal.

## Pipeline Progress View

Accessed from the dashboard when clicking a running strategy, or via `/strategies/:id/run`.

```
┌─ Pipeline: AAPL · 2026-03-20 09:30:00 ──────────────────────────┐
│                                                                    │
│  ┌─────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐          │
│  │ Market  │  │ Fundmtls │  │   News   │  │  Social  │          │
│  │  ✓ 3s   │  │  ✓ 2.8s  │  │  ✓ 2.1s  │  │  ✓ 3.5s  │          │
│  └────┬────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘          │
│       └─────────┬──┴────────────┘──────────────┘                  │
│                 ▼                                                  │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │  Research Debate · Round 3/3                                  │ │
│  │  ┌──────────────────┐  ┌──────────────────┐                  │ │
│  │  │ 🐂 Bull          │  │ 🐻 Bear          │                  │ │
│  │  │ Services growth  │  │ P/E overvalued   │                  │ │
│  │  │ offsets cyclical │  │ EU DMA risk...   │                  │ │
│  │  │ headwinds...     │  │                  │                  │ │
│  │  └──────────────────┘  └──────────────────┘                  │ │
│  │  Judge: "Moderately bullish — 7/10 confidence"               │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                 ▼                                                  │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │  Trader Agent · ✓                                             │ │
│  │  Action: BUY  Entry: Limit $182.50  Stop: $178.30            │ │
│  │  Size: $10,250 (10% of account)  Target: $190.80             │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                 ▼                                                  │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │  Risk Debate · ⏳ Round 2/3                                   │ │
│  │  Aggressive: "Size looks appropriate..."                      │ │
│  │  Conservative: "Tighten stop to $179.50..."                   │ │
│  │  Neutral: ⣾ generating...                                    │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                 ▼                                                  │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │  Signal · ○ Waiting                                           │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                                                    │
│  Elapsed: 18.2s  LLM Calls: 12  Tokens: 8,430  Cost: ~$0.25     │
│                                                                    │
│  [View Full Report]  [Cancel Pipeline]                             │
└───────────────────────────────────────────────────────────────────┘
```

## Component Architecture

### PipelineView (Container)

```tsx
function PipelineView({ runId }: { runId: string }) {
  const { data: run } = usePipelineRun(runId);
  const { events } = usePipelineEvents(runId);

  return (
    <div className="space-y-4">
      <PipelineHeader run={run} />
      <AnalystPhase
        decisions={run.decisions.filter((d) => d.phase === "analysis")}
      />
      <ResearchDebatePhase
        decisions={run.decisions.filter((d) => d.phase === "research_debate")}
      />
      <TraderPhase
        decision={run.decisions.find((d) => d.agent_role === "trader")}
      />
      <RiskDebatePhase
        decisions={run.decisions.filter((d) => d.phase === "risk_debate")}
      />
      <SignalPhase signal={run.signal} />
      <PipelineStats run={run} />
    </div>
  );
}
```

### AgentNode

Individual agent box with status indicator:

```tsx
type AgentStatus = "waiting" | "running" | "complete" | "error";

function AgentNode({ name, status, latencyMs, output }: AgentNodeProps) {
  const statusColors = {
    waiting: "border-muted",
    running: "border-blue-500 animate-pulse",
    complete: "border-green-500",
    error: "border-red-500",
  };

  return (
    <Card className={`${statusColors[status]} border-2`}>
      <div className="flex items-center gap-2">
        <StatusDot status={status} />
        <span className="font-medium">{name}</span>
        {latencyMs && (
          <span className="text-muted-foreground text-xs">{latencyMs}ms</span>
        )}
      </div>
      {output && (
        <p className="text-sm text-muted-foreground mt-2 line-clamp-3">
          {output}
        </p>
      )}
    </Card>
  );
}
```

### DebateView

Side-by-side debate visualization with rounds:

```tsx
function DebateView({ messages, maxRounds }: DebateViewProps) {
  const rounds = groupByRound(messages);

  return (
    <div className="space-y-4">
      {rounds.map((round, i) => (
        <div key={i} className="flex gap-4">
          <DebateBubble
            role="bull"
            content={round.bull}
            className="bg-green-950/30 border-green-800"
          />
          <DebateBubble
            role="bear"
            content={round.bear}
            className="bg-red-950/30 border-red-800"
          />
        </div>
      ))}
      <RoundProgress current={messages.length / 2} max={maxRounds} />
    </div>
  );
}
```

## Real-Time Updates

Pipeline visualization updates are driven by WebSocket events:

1. `agent_decision` → Update agent node status from "running" to "complete"
2. `debate_round` → Add new debate bubble, advance round counter
3. `signal` → Flash signal badge with animation
4. `error` → Turn affected node red, show error message

Events arrive via the `usePipelineEvents` hook which filters the global WebSocket stream by `run_id`.

## Decision Inspector

Clicking any agent node opens a full-screen modal with:

- Complete LLM prompt (system + user messages)
- Full response text
- Token usage and cost
- Latency breakdown
- Relevant memories that were injected

This enables debugging of agent behavior and prompt engineering.

---

**Related:** [[dashboard-design]] · [[frontend-overview]] · [[websocket-server]] · [[agent-system-overview]]
