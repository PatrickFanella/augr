---
title: "Frontend Overview"
date: 2026-03-20
tags: [frontend, react, typescript, architecture]
---

# Frontend Overview

React SPA providing a real-time dashboard for monitoring agents, managing strategies, and tracking portfolio performance.

## Project Structure

```
web/
├── src/
│   ├── main.tsx                    # Entry point
│   ├── App.tsx                     # Router + layout
│   ├── api/
│   │   ├── client.ts              # Axios/fetch wrapper
│   │   ├── queries.ts             # TanStack Query hooks
│   │   └── websocket.ts           # WebSocket connection manager
│   ├── components/
│   │   ├── ui/                    # shadcn/ui components
│   │   ├── layout/
│   │   │   ├── Sidebar.tsx
│   │   │   ├── Header.tsx
│   │   │   └── Layout.tsx
│   │   ├── dashboard/
│   │   │   ├── PortfolioSummary.tsx
│   │   │   ├── StrategyList.tsx
│   │   │   ├── ActivityFeed.tsx
│   │   │   └── RiskStatusBar.tsx
│   │   ├── agents/
│   │   │   ├── PipelineView.tsx    # Agent pipeline visualization
│   │   │   ├── DebateView.tsx      # Bull/bear debate viewer
│   │   │   ├── AgentNode.tsx       # Individual agent node
│   │   │   └── SignalBadge.tsx
│   │   ├── portfolio/
│   │   │   ├── PositionTable.tsx
│   │   │   ├── TradeHistory.tsx
│   │   │   ├── PnLChart.tsx
│   │   │   └── PortfolioChart.tsx
│   │   ├── strategies/
│   │   │   ├── StrategyForm.tsx
│   │   │   ├── StrategyDetail.tsx
│   │   │   └── RunHistory.tsx
│   │   └── risk/
│   │       ├── RiskDashboard.tsx
│   │       └── KillSwitchToggle.tsx
│   ├── hooks/
│   │   ├── useWebSocket.ts        # WebSocket hook with reconnection
│   │   └── usePipelineEvents.ts   # Pipeline event stream
│   ├── pages/
│   │   ├── DashboardPage.tsx
│   │   ├── StrategiesPage.tsx
│   │   ├── PortfolioPage.tsx
│   │   ├── PipelineRunPage.tsx
│   │   ├── MemoriesPage.tsx
│   │   └── SettingsPage.tsx
│   ├── types/
│   │   └── api.ts                 # TypeScript types matching Go API
│   └── lib/
│       └── utils.ts
├── package.json
├── tsconfig.json
├── vite.config.ts
└── tailwind.config.ts
```

## Key Libraries

| Library            | Version | Purpose                           |
| ------------------ | ------- | --------------------------------- |
| React              | 19      | UI framework                      |
| Vite               | 6       | Build tool                        |
| React Router       | 7       | Client-side routing               |
| TanStack Query     | 5       | Server state management + caching |
| TanStack Table     | 8       | Data tables                       |
| shadcn/ui          | Latest  | Component primitives              |
| Tailwind CSS       | 4       | Utility-first CSS                 |
| Recharts           | 2       | Charts (portfolio, P&L)           |
| Lightweight Charts | 4       | Price/candlestick charts          |
| React Hook Form    | 7       | Form handling                     |
| Zod                | 3       | Schema validation                 |

## Routing

```
/                     → Dashboard (portfolio summary, strategies, activity)
/strategies           → Strategy list + create
/strategies/:id       → Strategy detail + runs
/strategies/:id/run   → Live pipeline run view
/portfolio            → Positions, trades, P&L charts
/memories             → Agent memory browser + search
/settings             → Configuration, LLM providers, risk limits
```

## State Management

**Server state** (majority of data) is managed via TanStack Query:

```tsx
// api/queries.ts
export function useStrategies() {
  return useQuery({
    queryKey: ["strategies"],
    queryFn: () => apiClient.get<Strategy[]>("/strategies"),
  });
}

export function usePipelineRun(runId: string) {
  return useQuery({
    queryKey: ["runs", runId],
    queryFn: () => apiClient.get<PipelineRun>(`/runs/${runId}`),
    refetchInterval: 2000, // poll while running
  });
}
```

**Real-time state** comes via WebSocket for low-latency updates (see [[websocket-server]]).

**Local UI state** (active tab, form state, modal open/close) uses React `useState` — no global state library needed.

## WebSocket Integration

```tsx
// hooks/useWebSocket.ts
export function useWebSocket() {
  const [events, setEvents] = useState<WSEvent[]>([]);
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    const ws = new WebSocket(`ws://${window.location.host}/ws?token=${token}`);

    ws.onmessage = (event) => {
      const data = JSON.parse(event.data) as WSEvent;
      setEvents((prev) => [data, ...prev].slice(0, 100));

      // Invalidate relevant queries for fresh data
      if (data.type === "order_filled") {
        queryClient.invalidateQueries({ queryKey: ["portfolio"] });
      }
    };

    wsRef.current = ws;
    return () => ws.close();
  }, [token]);

  return { events, subscribe };
}
```

## Design System

- **Colors:** Dark theme default (trading dashboards are always dark)
- **Font:** Inter (UI), JetBrains Mono (numbers, code)
- **Components:** shadcn/ui for consistency + accessibility
- **Responsive:** Desktop-first, tablet-compatible (not mobile — trading is a desktop activity)

---

**Related:** [[dashboard-design]] · [[agent-visualization]] · [[portfolio-and-strategy-ui]] · [[api-design]]
