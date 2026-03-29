package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	internalapi "github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	appStyle = lipgloss.NewStyle().
			Padding(1, 2)
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230"))
	tabStyle = lipgloss.NewStyle().
			Padding(0, 2).
			Foreground(lipgloss.Color("245"))
	activeTabStyle = tabStyle.Copy().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("62"))
	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)
	mutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	dangerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
)

type PortfolioSummary struct {
	OpenPositions int
	UnrealizedPnL float64
	RealizedPnL   float64
}

type ActivityItem struct {
	OccurredAt time.Time
	Title      string
	Details    string
}

type Snapshot struct {
	Portfolio  PortfolioSummary
	Positions  []domain.Position
	Strategies []domain.Strategy
	Risk       risk.EngineStatus
	LatestRun  *domain.PipelineRun
	Settings   internalapi.SettingsResponse
	Activity   []ActivityItem
}

type EventSource interface {
	Messages() <-chan internalapi.WSMessage
	Close() error
}

type ConnectFunc func(context.Context) (EventSource, error)

type Options struct {
	Snapshot Snapshot
	Connect  ConnectFunc
	Output   io.Writer
	Width    int
	Height   int
}

type Model struct {
	tabs        []string
	activeTab   int
	width       int
	height      int
	snapshot    Snapshot
	latestRun   *domain.PipelineRun
	runProgress int
	events      <-chan internalapi.WSMessage
}

type wsEventMsg struct {
	event internalapi.WSMessage
}

func Run(ctx context.Context, opts Options) error {
	output := opts.Output
	if output == nil {
		output = os.Stdout
	}

	model := NewModel(opts.Snapshot, opts.Width, opts.Height)
	var source EventSource
	if opts.Connect != nil {
		var err error
		source, err = opts.Connect(ctx)
		if err != nil {
			model.appendActivity(ActivityItem{
				OccurredAt: time.Now().UTC(),
				Title:      "WebSocket unavailable",
				Details:    err.Error(),
			})
		} else {
			defer source.Close()
			model.events = source.Messages()
		}
	}

	program := tea.NewProgram(
		model,
		tea.WithContext(ctx),
		tea.WithAltScreen(),
		tea.WithOutput(output),
	)
	_, err := program.Run()
	return err
}

func Render(snapshot Snapshot, width, height int) string {
	model := NewModel(snapshot, width, height)
	return model.View()
}

func NewModel(snapshot Snapshot, width, height int) Model {
	model := Model{
		tabs:      []string{"Dashboard", "Strategies", "Portfolio", "Config"},
		width:     width,
		height:    height,
		snapshot:  snapshot,
		activeTab: 0,
	}
	if model.width <= 0 {
		model.width = 120
	}
	if model.height <= 0 {
		model.height = 34
	}
	if snapshot.LatestRun != nil {
		run := *snapshot.LatestRun
		model.latestRun = &run
		model.runProgress = progressForRun(run)
	}
	return model
}

func (m Model) Init() tea.Cmd {
	if m.events == nil {
		return nil
	}
	return waitForEvent(m.events)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab", "right", "l":
			m.activeTab = (m.activeTab + 1) % len(m.tabs)
		case "shift+tab", "left", "h":
			m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
		}
	case wsEventMsg:
		m.applyEvent(msg.event)
		if m.events != nil {
			return m, waitForEvent(m.events)
		}
	}

	return m, nil
}

func (m Model) View() string {
	contentWidth := max(60, m.width-6)
	tabParts := make([]string, 0, len(m.tabs))
	for i, tab := range m.tabs {
		style := tabStyle
		if i == m.activeTab {
			style = activeTabStyle
		}
		tabParts = append(tabParts, style.Render(tab))
	}

	help := mutedStyle.Render("tab/←/→ switch • q quit")
	body := ""
	switch m.tabs[m.activeTab] {
	case "Dashboard":
		body = m.renderDashboard(contentWidth)
	case "Strategies":
		body = m.renderStrategies(contentWidth)
	case "Portfolio":
		body = m.renderPortfolio(contentWidth)
	case "Config":
		body = m.renderConfig(contentWidth)
	}

	return appStyle.Width(contentWidth + 4).Render(
		titleStyle.Render("Trading Agent Dashboard") + "\n" +
			lipgloss.JoinHorizontal(lipgloss.Top, tabParts...) + "\n\n" +
			body + "\n\n" + help,
	)
}

func waitForEvent(ch <-chan internalapi.WSMessage) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return nil
		}
		return wsEventMsg{event: event}
	}
}

func (m *Model) applyEvent(event internalapi.WSMessage) {
	m.appendActivity(ActivityItem{
		OccurredAt: event.Timestamp,
		Title:      formatEventType(event.Type),
		Details:    describeEvent(event),
	})

	if event.RunID == [16]byte{} {
		return
	}

	if m.latestRun == nil || m.latestRun.ID != event.RunID {
		m.latestRun = &domain.PipelineRun{
			ID:         event.RunID,
			StrategyID: event.StrategyID,
			Status:     domain.PipelineStatusRunning,
			StartedAt:  event.Timestamp,
		}
	}

	if ticker := extractTicker(event.Data); ticker != "" {
		m.latestRun.Ticker = ticker
	}

	switch event.Type {
	case internalapi.EventPipelineStart:
		m.latestRun.Status = domain.PipelineStatusRunning
		m.runProgress = max(m.runProgress, 10)
	case internalapi.EventAgentDecision:
		m.latestRun.Status = domain.PipelineStatusRunning
		m.runProgress = max(m.runProgress, 35)
	case internalapi.EventDebateRound:
		m.latestRun.Status = domain.PipelineStatusRunning
		m.runProgress = max(m.runProgress, 55)
	case internalapi.EventSignal:
		m.latestRun.Status = domain.PipelineStatusRunning
		m.runProgress = max(m.runProgress, 80)
	case internalapi.EventOrderSubmitted, internalapi.EventPositionUpdate:
		m.latestRun.Status = domain.PipelineStatusRunning
		m.runProgress = max(m.runProgress, 90)
	case internalapi.EventOrderFilled:
		m.latestRun.Status = domain.PipelineStatusCompleted
		m.runProgress = 100
	case internalapi.EventError:
		m.latestRun.Status = domain.PipelineStatusFailed
		m.latestRun.ErrorMessage = describeEvent(event)
		m.runProgress = 100
	}
}

func (m *Model) appendActivity(item ActivityItem) {
	if item.OccurredAt.IsZero() {
		item.OccurredAt = time.Now().UTC()
	}
	m.snapshot.Activity = append([]ActivityItem{item}, m.snapshot.Activity...)
	if len(m.snapshot.Activity) > 10 {
		m.snapshot.Activity = m.snapshot.Activity[:10]
	}
}

func (m Model) renderDashboard(width int) string {
	leftWidth := max(34, width/2-1)
	rightWidth := width - leftWidth - 2

	left := lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderPortfolioCard(leftWidth),
		m.renderRiskCard(leftWidth),
		m.renderRunCard(leftWidth),
	)
	right := m.renderActivityCard(rightWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (m Model) renderStrategies(width int) string {
	lines := []string{titleStyle.Render("Active strategies")}
	if len(m.snapshot.Strategies) == 0 {
		lines = append(lines, mutedStyle.Render("No active strategies"))
		return cardStyle.Width(width).Render(strings.Join(lines, "\n"))
	}

	for _, strategy := range m.snapshot.Strategies {
		status := successStyle.Render("ACTIVE")
		if !strategy.IsPaper {
			status += " " + warningStyle.Render("LIVE")
		} else {
			status += " " + mutedStyle.Render("PAPER")
		}
		lines = append(lines, fmt.Sprintf("• %s (%s)  %s", strategy.Name, strategy.Ticker, status))
	}
	return cardStyle.Width(width).Render(strings.Join(lines, "\n"))
}

func (m Model) renderPortfolio(width int) string {
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderPortfolioCard(width),
		m.renderPositionsCard(width),
	)
}

func (m Model) renderConfig(width int) string {
	brokers := []string{"none configured"}
	if len(m.snapshot.Settings.System.ConnectedBrokers) > 0 {
		brokers = brokers[:0]
		for _, broker := range m.snapshot.Settings.System.ConnectedBrokers {
			state := "not configured"
			if broker.Configured {
				state = "configured"
			}
			mode := "live"
			if broker.PaperMode {
				mode = "paper"
			}
			brokers = append(brokers, fmt.Sprintf("• %s (%s, %s)", broker.Name, mode, state))
		}
	}

	body := []string{
		titleStyle.Render("Config"),
		fmt.Sprintf("Environment: %s", emptyDash(m.snapshot.Settings.System.Environment)),
		fmt.Sprintf("Version: %s", emptyDash(m.snapshot.Settings.System.Version)),
		fmt.Sprintf("Default provider: %s", emptyDash(m.snapshot.Settings.LLM.DefaultProvider)),
		fmt.Sprintf("Quick model: %s", emptyDash(m.snapshot.Settings.LLM.QuickThinkModel)),
		fmt.Sprintf("Deep model: %s", emptyDash(m.snapshot.Settings.LLM.DeepThinkModel)),
		"",
		titleStyle.Render("Connected brokers"),
	}
	body = append(body, brokers...)
	return cardStyle.Width(width).Render(strings.Join(body, "\n"))
}

func (m Model) renderPortfolioCard(width int) string {
	body := []string{
		titleStyle.Render("Portfolio summary"),
		fmt.Sprintf("Open positions: %d", m.snapshot.Portfolio.OpenPositions),
		fmt.Sprintf("Unrealized P&L: %.2f", m.snapshot.Portfolio.UnrealizedPnL),
		fmt.Sprintf("Realized P&L: %.2f", m.snapshot.Portfolio.RealizedPnL),
	}
	return cardStyle.Width(width).Render(strings.Join(body, "\n"))
}

func (m Model) renderRiskCard(width int) string {
	status := m.snapshot.Risk.RiskStatus
	riskLabel := successStyle.Render(strings.ToUpper(status.String()))
	if status == domain.RiskStatusWarning {
		riskLabel = warningStyle.Render(strings.ToUpper(status.String()))
	}
	if status == domain.RiskStatusBreached {
		riskLabel = dangerStyle.Render(strings.ToUpper(status.String()))
	}

	ratio := clampPercent(m.snapshot.Risk.PositionLimits.MaxTotalPct)
	if m.snapshot.Portfolio.OpenPositions > 0 && m.snapshot.Risk.PositionLimits.MaxConcurrent > 0 {
		ratio = float64(m.snapshot.Portfolio.OpenPositions) / float64(m.snapshot.Risk.PositionLimits.MaxConcurrent)
	}

	body := []string{
		titleStyle.Render("Risk status"),
		fmt.Sprintf("Status: %s", riskLabel),
		fmt.Sprintf("Circuit breaker: %s", emptyDash(m.snapshot.Risk.CircuitBreaker.State.String())),
		fmt.Sprintf("Kill switch: %t", m.snapshot.Risk.KillSwitch.Active),
		m.renderStatusBar(ratio, width-6),
	}
	return cardStyle.Width(width).Render(strings.Join(body, "\n"))
}

func (m Model) renderRunCard(width int) string {
	body := []string{titleStyle.Render("Pipeline run")}
	if m.latestRun == nil {
		body = append(body, mutedStyle.Render("No pipeline runs available"))
		return cardStyle.Width(width).Render(strings.Join(body, "\n"))
	}

	body = append(body,
		fmt.Sprintf("Ticker: %s", emptyDash(m.latestRun.Ticker)),
		fmt.Sprintf("Run ID: %s", m.latestRun.ID.String()),
		fmt.Sprintf("Status: %s", emptyDash(m.latestRun.Status.String())),
		fmt.Sprintf("Progress: %d%%", m.runProgress),
		m.renderProgressBar(m.runProgress, width-6),
	)
	if strings.TrimSpace(m.latestRun.ErrorMessage) != "" {
		body = append(body, dangerStyle.Render("Error: "+m.latestRun.ErrorMessage))
	}
	return cardStyle.Width(width).Render(strings.Join(body, "\n"))
}

func (m Model) renderActivityCard(width int) string {
	body := []string{titleStyle.Render("Live activity feed")}
	if len(m.snapshot.Activity) == 0 {
		body = append(body, mutedStyle.Render("Waiting for WebSocket activity"))
		return cardStyle.Width(width).Render(strings.Join(body, "\n"))
	}
	for _, item := range m.snapshot.Activity {
		body = append(body, fmt.Sprintf("• %s  %s", item.OccurredAt.UTC().Format("15:04:05"), item.Title))
		if strings.TrimSpace(item.Details) != "" {
			body = append(body, "  "+item.Details)
		}
	}
	return cardStyle.Width(width).Render(strings.Join(body, "\n"))
}

func (m Model) renderPositionsCard(width int) string {
	body := []string{titleStyle.Render("Open positions")}
	if len(m.snapshot.Positions) == 0 {
		body = append(body, mutedStyle.Render("No open positions"))
		return cardStyle.Width(width).Render(strings.Join(body, "\n"))
	}
	for _, position := range m.snapshot.Positions {
		body = append(body, fmt.Sprintf(
			"• %s %s  qty %.2f  avg %.2f  current %s",
			position.Ticker,
			position.Side.String(),
			position.Quantity,
			position.AvgEntry,
			formatOptionalFloat(position.CurrentPrice),
		))
	}
	return cardStyle.Width(width).Render(strings.Join(body, "\n"))
}

func (m Model) renderStatusBar(ratio float64, width int) string {
	return renderFilledBar(clampPercent(ratio), width, lipgloss.Color("214"))
}

func (m Model) renderProgressBar(progress int, width int) string {
	return renderFilledBar(float64(progress)/100, width, lipgloss.Color("42"))
}

func renderFilledBar(ratio float64, width int, color lipgloss.Color) string {
	barWidth := max(10, width)
	filled := int(clampPercent(ratio) * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	return lipgloss.NewStyle().
		Foreground(color).
		Render(strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled))
}

func progressForRun(run domain.PipelineRun) int {
	switch run.Status {
	case domain.PipelineStatusCompleted, domain.PipelineStatusFailed, domain.PipelineStatusCancelled:
		return 100
	case domain.PipelineStatusRunning:
		return 40
	default:
		return 0
	}
}

func formatEventType(eventType internalapi.EventType) string {
	switch eventType {
	case internalapi.EventPipelineStart:
		return "Pipeline started"
	case internalapi.EventAgentDecision:
		return "Agent decision"
	case internalapi.EventDebateRound:
		return "Debate round"
	case internalapi.EventSignal:
		return "Signal generated"
	case internalapi.EventOrderSubmitted:
		return "Order submitted"
	case internalapi.EventOrderFilled:
		return "Order filled"
	case internalapi.EventPositionUpdate:
		return "Position updated"
	case internalapi.EventCircuitBreaker:
		return "Circuit breaker"
	case internalapi.EventError:
		return "Pipeline error"
	default:
		return string(eventType)
	}
}

func describeEvent(event internalapi.WSMessage) string {
	switch data := event.Data.(type) {
	case map[string]any:
		parts := make([]string, 0, len(data))
		for _, key := range []string{"ticker", "signal", "status", "reason"} {
			if value, ok := data[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
				parts = append(parts, fmt.Sprintf("%s=%v", key, value))
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " • ")
		}
	}
	return strings.TrimSpace(fmt.Sprint(event.Data))
}

func extractTicker(data any) string {
	payload, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	value, ok := payload["ticker"]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func formatOptionalFloat(value *float64) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *value)
}

func clampPercent(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
