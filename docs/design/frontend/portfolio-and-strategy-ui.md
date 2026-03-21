---
title: "Portfolio & Strategy UI"
date: 2026-03-20
tags: [frontend, portfolio, strategies, positions, configuration]
---

# Portfolio & Strategy UI

## Portfolio Page

### Positions Table

| Column      | Description                         |
| ----------- | ----------------------------------- |
| Ticker      | Asset symbol                        |
| Side        | Long/Short                          |
| Quantity    | Number of shares/units              |
| Avg Entry   | Average entry price                 |
| Current     | Current market price                |
| P&L ($)     | Unrealized profit/loss              |
| P&L (%)     | Percentage return                   |
| Stop Loss   | Current stop-loss level             |
| Take Profit | Target exit price                   |
| Strategy    | Which strategy opened this position |

Features:

- Sortable by any column
- Color-coded P&L (green positive, red negative)
- Click row to expand with full position history
- Quick actions: close position, adjust stop/target

### Trade History

Filterable table of all executed trades:

- Date range filter
- Strategy filter
- Side filter (buy/sell)
- Export to CSV

### P&L Charts

- **Cumulative P&L:** Line chart showing running total P&L over time
- **Daily P&L:** Bar chart showing daily gains/losses
- **By Strategy:** Stacked area chart showing contribution per strategy
- **Drawdown:** Chart showing portfolio drawdown over time

## Strategy Page

### Strategy List

Cards or table showing all strategies:

```tsx
function StrategyCard({ strategy }: { strategy: Strategy }) {
  return (
    <Card className="p-4">
      <div className="flex justify-between items-start">
        <div>
          <h3 className="font-semibold">{strategy.name}</h3>
          <p className="text-muted-foreground">
            {strategy.ticker} · {strategy.marketType}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <SignalBadge signal={strategy.lastSignal} />
          <StatusDot active={strategy.isActive} />
        </div>
      </div>
      <div className="mt-3 grid grid-cols-3 gap-2 text-sm">
        <Stat label="Total P&L" value={formatCurrency(strategy.totalPnl)} />
        <Stat label="Win Rate" value={formatPct(strategy.winRate)} />
        <Stat label="Trades" value={strategy.tradeCount} />
      </div>
    </Card>
  );
}
```

### Strategy Configuration Form

Multi-step form using React Hook Form + Zod:

**Step 1: Basics**

- Strategy name
- Ticker symbol
- Market type (stock / crypto / polymarket)
- Paper trading toggle

**Step 2: Agents**

- Analyst selection (checkboxes: market, fundamentals, news, social)
- Debate rounds configuration
- Risk debate rounds configuration

**Step 3: LLM**

- Deep Think provider + model
- Quick Think provider + model
- Temperature settings

**Step 4: Risk**

- Position size method (ATR / Kelly / fixed)
- Max position size (% of account)
- Stop-loss ATR multiplier
- Take-profit ATR multiplier

**Step 5: Schedule**

- Cron schedule or manual trigger only
- Time zone selection

```tsx
const strategySchema = z.object({
  name: z.string().min(1, "Required"),
  ticker: z.string().min(1, "Required").toUpperCase(),
  marketType: z.enum(["stock", "crypto", "polymarket"]),
  isPaper: z.boolean().default(true),
  config: z.object({
    analysts: z
      .array(z.enum(["market", "fundamentals", "news", "social"]))
      .min(1),
    maxDebateRounds: z.number().min(1).max(10).default(3),
    maxRiskDebateRounds: z.number().min(1).max(10).default(3),
    deepThinkLlm: llmConfigSchema,
    quickThinkLlm: llmConfigSchema,
    positionSizePct: z.number().min(1).max(25).default(5),
    stopLossAtrMult: z.number().min(0.5).max(5).default(1.5),
    takeProfitAtrMult: z.number().min(1).max(10).default(3),
  }),
  scheduleCron: z.string().optional(),
});
```

### Strategy Detail Page

Combines:

- Strategy configuration summary
- Run history table (with signal, P&L per run)
- Performance chart (P&L over time for this strategy)
- "Run Now" button for manual trigger
- Edit / Pause / Delete actions

### Agent Memory Browser

Accessible at `/memories`:

- Search bar with full-text search
- Filter by agent role (bull, bear, trader, invest_judge, risk_manager)
- Each memory card shows situation, recommendation, and outcome
- Delete button for individual memories

---

**Related:** [[dashboard-design]] · [[frontend-overview]] · [[api-design]] · [[agent-visualization]]
