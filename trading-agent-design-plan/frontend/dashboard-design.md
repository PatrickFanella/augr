---
title: "Dashboard Design"
date: 2026-03-20
tags: [frontend, dashboard, ui, monitoring]
---

# Dashboard Design

The main dashboard provides at-a-glance visibility into portfolio health, agent activity, and risk status.

## Layout

```
┌──────────────────────────────────────────────────────────────────┐
│  Header: Trading Agent Dashboard              [Settings] [User]  │
├──────┬───────────────────────────────────────────────────────────┤
│      │                                                           │
│  S   │  ┌─ Portfolio Summary ──────────────────────────────────┐ │
│  i   │  │  Total: $102,450  |  Day: +$320 (+0.31%)            │ │
│  d   │  │  Positions: 4     |  Cash: $45,200                  │ │
│  e   │  │  Drawdown: -2.1%  |  Sharpe (30d): 1.42             │ │
│  b   │  └──────────────────────────────────────────────────────┘ │
│  a   │                                                           │
│  r   │  ┌─ Portfolio Value Chart ──────────────────────────────┐ │
│      │  │  [Line chart: 30d portfolio value]                   │ │
│  D   │  │                                                      │ │
│  a   │  └──────────────────────────────────────────────────────┘ │
│  s   │                                                           │
│  h   │  ┌─ Active Strategies ──┐ ┌─ Live Agent Activity ──────┐ │
│  b   │  │  AAPL Multi ● BUY   │ │  09:30 AAPL Risk: BUY      │ │
│  o   │  │  BTC Mom    ● HOLD  │ │  09:29 AAPL Debate R3      │ │
│  a   │  │  ETH Swing  ○ SELL  │ │  09:28 AAPL Trader: plan   │ │
│  r   │  │  Election   ● HOLD  │ │  09:27 AAPL Research: bull │ │
│  d   │  └──────────────────────┘ │  09:26 AAPL Analysts ✓    │ │
│      │                            └────────────────────────────┘ │
│  P   │                                                           │
│  o   │  ┌─ Risk Status Bar ───────────────────────────────────┐ │
│  r   │  │  Kill: OFF  |  Daily: -0.1%/-3%  |  DD: -2.1%/-10% │ │
│  t   │  └──────────────────────────────────────────────────────┘ │
│      │                                                           │
└──────┴───────────────────────────────────────────────────────────┘
```

## Components

### Portfolio Summary Card

```tsx
function PortfolioSummary() {
  const { data } = usePortfolioSummary();
  return (
    <Card>
      <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
        <Metric label="Total Value" value={formatCurrency(data.totalValue)} />
        <Metric
          label="Day P&L"
          value={formatCurrency(data.dayPnl)}
          change={data.dayPnlPct}
        />
        <Metric label="Open Positions" value={data.openPositions} />
        <Metric label="Cash" value={formatCurrency(data.cash)} />
        <Metric
          label="Max Drawdown"
          value={formatPct(data.maxDrawdown)}
          negative
        />
        <Metric label="Sharpe (30d)" value={data.sharpe30d.toFixed(2)} />
      </div>
    </Card>
  );
}
```

### Portfolio Value Chart

Line chart showing portfolio value over time (7d, 30d, 90d, YTD):

- **Library:** Recharts `AreaChart` with gradient fill
- **Data:** `GET /api/v1/portfolio/history?period=30d`
- **Update:** Refresh on `position_update` WebSocket events

### Active Strategies

Compact table showing each strategy's status:

| Column   | Content                                      |
| -------- | -------------------------------------------- |
| Name     | Strategy name (linked to detail)             |
| Ticker   | Asset being traded                           |
| Signal   | Last signal badge (BUY/SELL/HOLD with color) |
| Last Run | Timestamp of last pipeline completion        |
| Status   | Active/Paused indicator                      |
| P&L      | Cumulative P&L for this strategy             |

### Live Agent Activity Feed

Real-time feed of agent events via WebSocket:

```tsx
function ActivityFeed() {
  const { events } = useWebSocket();
  return (
    <div className="space-y-1 font-mono text-sm">
      {events.slice(0, 20).map((event) => (
        <ActivityItem key={event.id} event={event} />
      ))}
    </div>
  );
}

function ActivityItem({ event }: { event: WSEvent }) {
  const icon = {
    agent_decision: "🤖",
    debate_round: "💬",
    signal: "🎯",
    order_filled: "✅",
    circuit_breaker: "🚨",
  }[event.type];

  return (
    <div className="flex gap-2 text-muted-foreground">
      <span>{formatTime(event.timestamp)}</span>
      <span>{event.data.ticker}</span>
      <span>{icon}</span>
      <span>{event.data.summary}</span>
    </div>
  );
}
```

### Risk Status Bar

Persistent bar at the bottom showing current risk state:

- Kill switch status (OFF = green, ON = red, clickable toggle)
- Daily loss progress bar (current vs. limit)
- Drawdown progress bar (current vs. limit)
- Circuit breaker status (all clear / tripped)

Turns red and flashes when any circuit breaker trips.

## Real-Time Updates

The dashboard combines two data strategies:

1. **TanStack Query** — polls API every 30 seconds for full data refresh
2. **WebSocket** — pushes incremental updates in real-time

When a WebSocket event arrives, TanStack Query caches are selectively invalidated to trigger re-fetches of affected data.

---

**Related:** [[frontend-overview]] · [[agent-visualization]] · [[portfolio-and-strategy-ui]] · [[websocket-server]]
