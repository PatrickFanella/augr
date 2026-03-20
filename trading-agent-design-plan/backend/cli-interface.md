---
title: "CLI Interface — Bubble Tea + Lipgloss"
date: 2026-03-20
tags: [backend, cli, bubble-tea, lipgloss, tui]
---

# CLI Interface

A full terminal user interface built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) (TUI framework) and [Lipgloss](https://github.com/charmbracelet/lipgloss) (styling). Provides complete control over the trading agent system without needing the web frontend.

## Libraries

| Library                              | Purpose                                                         |
| ------------------------------------ | --------------------------------------------------------------- |
| `github.com/charmbracelet/bubbletea` | Elm-architecture TUI framework                                  |
| `github.com/charmbracelet/lipgloss`  | Terminal CSS-like styling                                       |
| `github.com/charmbracelet/bubbles`   | Pre-built components (tables, text inputs, spinners, viewports) |
| `github.com/charmbracelet/huh`       | Interactive forms and prompts                                   |
| `github.com/charmbracelet/log`       | Styled logging                                                  |
| `github.com/spf13/cobra`             | CLI command structure and flags                                 |

## Command Structure

```
tradingagent                          # Launch TUI dashboard
tradingagent run <ticker>             # Run pipeline for a ticker (interactive)
tradingagent run AAPL --date 2026-03-20 --paper  # Non-interactive run
tradingagent strategies               # List strategies
tradingagent strategies create        # Interactive strategy wizard
tradingagent portfolio                # Show positions and P&L
tradingagent risk status              # Show risk controls status
tradingagent risk kill                # Activate kill switch
tradingagent risk unkill              # Deactivate kill switch
tradingagent memories search "AAPL momentum"  # Search agent memories
tradingagent config                   # View/edit configuration
tradingagent serve                    # Start the HTTP/WS API server
```

## TUI Dashboard (Main View)

The default `tradingagent` command launches a full-screen TUI dashboard:

```
┌─ Trading Agent ──────────────────────────────────────────────────┐
│                                                                   │
│  ┌─ Portfolio Summary ────────────────────────────────────────┐  │
│  │  Total Value: $102,450   Day P&L: +$320 (+0.31%)          │  │
│  │  Open Positions: 4       Cash: $45,200                    │  │
│  │  Max Drawdown: -2.1%     Sharpe (30d): 1.42               │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                   │
│  ┌─ Active Strategies ────────────────────────────────────────┐  │
│  │  NAME            TICKER  SIGNAL   LAST RUN     STATUS      │  │
│  │  ▸ AAPL Multi    AAPL    BUY      09:30:28     ● Active    │  │
│  │    BTC Momentum  BTCUSD  HOLD     08:00:15     ● Active    │  │
│  │    ETH Swing     ETHUSD  SELL     08:00:22     ○ Paused    │  │
│  │    US Election   POLYMARKET HOLD  12:00:00     ● Active    │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                   │
│  ┌─ Recent Activity ──────────────────────────────────────────┐  │
│  │  09:30:28  AAPL  Risk Manager: BUY (confidence: 7/10)      │  │
│  │  09:30:25  AAPL  Risk Debate: Round 3 complete             │  │
│  │  09:30:18  AAPL  Trader: Plan generated (limit $182.50)    │  │
│  │  09:30:12  AAPL  Research Manager: Moderately bullish      │  │
│  │  09:30:05  AAPL  Analysts complete (4/4)                   │  │
│  │  08:00:22  ETH   Risk Manager: HOLD (low conviction)       │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                   │
│  ┌─ Risk Status ──────────────────────────────────────────────┐  │
│  │  Kill Switch: OFF   Circuit Breakers: ALL CLEAR            │  │
│  │  Daily Loss: -0.1% / -3.0% limit                          │  │
│  │  Drawdown:   -2.1% / -10.0% limit                         │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                   │
│  [r]un  [s]trategies  [p]ortfolio  [c]onfig  [k]ill  [q]uit     │
└──────────────────────────────────────────────────────────────────┘
```

## Pipeline Run View

When watching a pipeline run live (`tradingagent run AAPL`):

```
┌─ Pipeline: AAPL · 2026-03-20 ────────────────────────────────────┐
│                                                                    │
│  Phase 1: Analysis                                                 │
│  ┌──────────┐ ┌──────────────┐ ┌──────────┐ ┌──────────┐         │
│  │ Market   │ │ Fundamentals │ │   News   │ │  Social  │         │
│  │ ✓ 3.2s  │ │   ✓ 2.8s    │ │ ✓ 2.1s  │ │ ⣾ ...   │         │
│  └──────────┘ └──────────────┘ └──────────┘ └──────────┘         │
│                                                                    │
│  Phase 2: Research Debate                                          │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  Round 1/3                                                   │  │
│  │  🐂 Bull: "Strong breakout above 200-day SMA with RSI..."  │  │
│  │  🐻 Bear: "Revenue growth decelerating; P/E at 32x..."     │  │
│  │  Round 2/3                                                   │  │
│  │  🐂 Bull: "Services revenue +18% offsets hardware cycle..." │  │
│  │  🐻 Bear: "EU DMA compliance costs unknown; margin..."     │  │
│  │  Round 3/3                                                   │  │
│  │  🐂 Bull: ⣾ generating...                                  │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                    │
│  Phase 3: Trading          ○ Waiting                               │
│  Phase 4: Risk Debate      ○ Waiting                               │
│  Phase 5: Signal           ○ Waiting                               │
│                                                                    │
│  Elapsed: 12.4s   LLM Calls: 8   Tokens: 4,230   Cost: ~$0.12    │
│                                                                    │
│  [esc] back   [d]etail   [c]ancel                                  │
└───────────────────────────────────────────────────────────────────┘
```

## Architecture

### Elm Architecture (Bubble Tea)

```go
// internal/cli/dashboard/model.go
type DashboardModel struct {
    // Sub-models
    portfolioTable table.Model
    strategyList   list.Model
    activityLog    viewport.Model
    riskStatus     RiskStatusModel

    // State
    activeTab   Tab
    strategies  []domain.Strategy
    positions   []domain.Position
    recentRuns  []domain.PipelineRun
    riskState   domain.RiskStatus

    // Connection to backend
    apiClient   *api.Client
    wsConn      *websocket.Conn
    width       int
    height      int
}

type Tab int
const (
    TabDashboard Tab = iota
    TabStrategies
    TabPortfolio
    TabConfig
    TabPipelineRun
)

func (m DashboardModel) Init() tea.Cmd {
    return tea.Batch(
        m.fetchPortfolio(),
        m.fetchStrategies(),
        m.connectWebSocket(),
        m.tickEverySecond(),
    )
}

func (m DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "q", "ctrl+c":
            return m, tea.Quit
        case "r":
            return m, m.switchTab(TabPipelineRun)
        case "s":
            return m, m.switchTab(TabStrategies)
        case "p":
            return m, m.switchTab(TabPortfolio)
        case "k":
            return m, m.toggleKillSwitch()
        }

    case WebSocketEvent:
        return m.handleWSEvent(msg)

    case PortfolioLoaded:
        m.positions = msg.Positions
        m.portfolioTable = buildPortfolioTable(msg.Positions)

    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
    }
    return m, nil
}

func (m DashboardModel) View() string {
    // Lipgloss styling
    header := lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("39")).
        Render("Trading Agent")

    // Layout with tabs
    switch m.activeTab {
    case TabDashboard:
        return m.renderDashboard()
    case TabPipelineRun:
        return m.renderPipelineRun()
    // ...
    }
}
```

### Lipgloss Theme

```go
// internal/cli/theme/theme.go
var (
    Primary   = lipgloss.Color("39")   // blue
    Success   = lipgloss.Color("82")   // green
    Warning   = lipgloss.Color("214")  // orange
    Danger    = lipgloss.Color("196")  // red
    Muted     = lipgloss.Color("241")  // gray

    BuyBadge = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("0")).
        Background(Success).
        Padding(0, 1).
        Render("BUY")

    SellBadge = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("0")).
        Background(Danger).
        Padding(0, 1).
        Render("SELL")

    HoldBadge = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("0")).
        Background(Warning).
        Padding(0, 1).
        Render("HOLD")

    BoxStyle = lipgloss.NewStyle().
        BorderStyle(lipgloss.RoundedBorder()).
        BorderForeground(Primary).
        Padding(1, 2)
)
```

### WebSocket Integration

The TUI connects to the same WebSocket server as the React frontend:

```go
func (m DashboardModel) connectWebSocket() tea.Cmd {
    return func() tea.Msg {
        conn, _, err := websocket.Dial(context.Background(),
            "ws://localhost:8080/ws?token="+m.apiToken, nil)
        if err != nil {
            return WSError{err}
        }
        // Subscribe to all events
        conn.Write(context.Background(), websocket.MessageText,
            []byte(`{"action":"subscribe","all":true}`))

        // Start reading events
        go func() {
            for {
                _, data, err := conn.Read(context.Background())
                if err != nil {
                    return
                }
                m.program.Send(parseWSEvent(data))
            }
        }()
        return WSConnected{conn}
    }
}
```

### Interactive Strategy Wizard (Huh)

```go
func runStrategyWizard() (*domain.Strategy, error) {
    var strategy domain.Strategy

    form := huh.NewForm(
        huh.NewGroup(
            huh.NewInput().Title("Strategy Name").Value(&strategy.Name),
            huh.NewInput().Title("Ticker Symbol").Value(&strategy.Ticker),
            huh.NewSelect[string]().
                Title("Market Type").
                Options(
                    huh.NewOption("US Stock", "stock"),
                    huh.NewOption("Crypto", "crypto"),
                    huh.NewOption("Polymarket", "polymarket"),
                ).
                Value(&strategy.MarketType),
        ),
        huh.NewGroup(
            huh.NewMultiSelect[string]().
                Title("Analysts").
                Options(
                    huh.NewOption("Market (Technical)", "market"),
                    huh.NewOption("Fundamentals", "fundamentals"),
                    huh.NewOption("News", "news"),
                    huh.NewOption("Social Media", "social"),
                ).
                Value(&strategy.Config.Analysts),
        ),
        huh.NewGroup(
            huh.NewSelect[string]().
                Title("LLM Provider").
                Options(
                    huh.NewOption("Anthropic (Claude)", "anthropic"),
                    huh.NewOption("OpenAI (GPT)", "openai"),
                    huh.NewOption("Ollama (Local)", "ollama"),
                ).
                Value(&strategy.Config.LLMProvider),
            huh.NewConfirm().
                Title("Paper Trading Mode?").
                Value(&strategy.IsPaper),
        ),
    )

    err := form.Run()
    return &strategy, err
}
```

## Project Structure Addition

```
internal/cli/
├── cmd/
│   ├── root.go           # cobra root command → launches TUI
│   ├── run.go            # `tradingagent run <ticker>`
│   ├── strategies.go     # `tradingagent strategies`
│   ├── portfolio.go      # `tradingagent portfolio`
│   ├── risk.go           # `tradingagent risk`
│   ├── config.go         # `tradingagent config`
│   ├── memories.go       # `tradingagent memories`
│   └── serve.go          # `tradingagent serve`
├── dashboard/
│   ├── model.go          # main dashboard model
│   ├── portfolio.go      # portfolio sub-view
│   ├── strategies.go     # strategy list sub-view
│   ├── pipeline.go       # live pipeline run view
│   ├── risk.go           # risk status sub-view
│   └── activity.go       # activity log sub-view
├── theme/
│   └── theme.go          # lipgloss colors and styles
└── api/
    └── client.go         # HTTP/WS client for backend
```

The CLI binary and API server share the same binary:

- `tradingagent` — launches TUI (connects to running server)
- `tradingagent serve` — starts the HTTP/WS server
- Both can run simultaneously (TUI connects to server via HTTP/WS)

---

**Related:** [[go-project-structure]] · [[api-design]] · [[websocket-server]] · [[dashboard-design]]
