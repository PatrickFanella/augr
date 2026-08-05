package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

var polymarketResolutionsSpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "0 * * * *"}

func (o *JobOrchestrator) registerPolymarketResolutionsJob() {
	if o.deps.PolymarketAccountRepo == nil || o.deps.PolymarketResolvedRepo == nil {
		return
	}
	o.Register("polymarket_resolutions", "Process resolved Polymarket markets and update win rates", polymarketResolutionsSpec, o.polymarketResolutions)
}

func (o *JobOrchestrator) polymarketResolutions(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://gamma-api.polymarket.com/markets?closed=true&active=false&order=closedTime&ascending=false&limit=500&offset=0", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	var markets []struct {
		Slug          string          `json:"slug"`
		Outcomes      json.RawMessage `json:"outcomes"`
		OutcomePrices json.RawMessage `json:"outcomePrices"`
		EndDate       string          `json:"endDate"`
		Closed        bool            `json:"closed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&markets); err != nil {
		return err
	}
	var processedMarkets, updatedAccounts, skippedUnresolved, skippedUnsupported, skippedNoTrades int
	for _, m := range markets {
		if !m.Closed {
			continue
		}
		outcomes, err := decodeStringArrayMaybeJSON(m.Outcomes)
		if err != nil {
			skippedUnsupported++
			continue
		}
		prices, err := decodeScalarArrayMaybeJSON(m.OutcomePrices)
		if err != nil {
			skippedUnsupported++
			continue
		}
		winningSide, ok := winnerFromGamma(outcomes, prices)
		if !ok {
			skippedUnresolved++
			continue
		}
		if normalized, ok := normalizePolymarketSide(winningSide); ok {
			winningSide = normalized
		} else {
			skippedUnsupported++
			continue
		}
		processed, err := o.deps.PolymarketResolvedRepo.IsProcessed(ctx, m.Slug)
		if err != nil || processed {
			continue
		}
		resolvedAt := time.Now().UTC()
		if parsed, parseErr := time.Parse(time.RFC3339, m.EndDate); parseErr == nil {
			resolvedAt = parsed
		}
		if o.deps.PredictionSettler != nil {
			if _, err := o.deps.PredictionSettler.SettleMarket(ctx, domain.MarketTypePolymarket, m.Slug, winningSide, resolvedAt); err != nil {
				return fmt.Errorf("polymarket_resolutions: settle paper market %s: %w", m.Slug, err)
			}
		}
		trades, err := o.deps.PolymarketAccountRepo.ListAllTradesBySlug(ctx, m.Slug, 10000)
		if err != nil {
			continue
		}
		if len(trades) == 0 {
			skippedNoTrades++
		}
		counts := map[string]struct{ won, lost int }{}
		for _, t := range trades {
			if strings.EqualFold(t.Action, "buy") {
				if strings.EqualFold(t.Side, winningSide) {
					c := counts[t.AccountAddress]
					c.won++
					counts[t.AccountAddress] = c
				} else {
					c := counts[t.AccountAddress]
					c.lost++
					counts[t.AccountAddress] = c
				}
			}
		}
		for addr, c := range counts {
			if c.won+c.lost == 0 {
				continue
			}
			if err := o.deps.PolymarketAccountRepo.IncrementAccountResolutionStats(ctx, addr, c.won, c.lost); err == nil {
				updatedAccounts++
			}
		}
		_ = o.deps.PolymarketResolvedRepo.MarkProcessed(ctx, m.Slug, winningSide, resolvedAt)
		processedMarkets++
	}
	_, _ = o.deps.PolymarketAccountRepo.MarkTracked(ctx, 0.70, 20)
	o.logger.Info("polymarket_resolutions: done", slog.Int("markets", processedMarkets), slog.Int("accounts", updatedAccounts), slog.Int("skipped_unresolved", skippedUnresolved), slog.Int("skipped_unsupported", skippedUnsupported), slog.Int("skipped_no_trades", skippedNoTrades))
	return nil
}

func decodeStringArrayMaybeJSON(raw json.RawMessage) ([]string, error) {
	var out []string
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty")
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return decodeStringArrayMaybeJSON(json.RawMessage(s))
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeScalarArrayMaybeJSON(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty")
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return decodeScalarArrayMaybeJSON(json.RawMessage(s))
	}
	var values []any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		switch v := value.(type) {
		case string:
			out = append(out, v)
		case float64:
			out = append(out, strconv.FormatFloat(v, 'f', -1, 64))
		default:
			return nil, fmt.Errorf("unsupported scalar %T", value)
		}
	}
	return out, nil
}

func winnerFromGamma(outcomes, prices []string) (string, bool) {
	if len(outcomes) != len(prices) || len(outcomes) != 2 {
		return "", false
	}
	for i := range prices {
		if isOneValue(prices[i]) {
			return outcomes[i], true
		}
	}
	return "", false
}

func isOneValue(s string) bool {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return err == nil && f == 1
}

func normalizePolymarketSide(side string) (string, bool) {
	switch {
	case strings.EqualFold(side, "yes"):
		return "YES", true
	case strings.EqualFold(side, "no"):
		return "NO", true
	case strings.EqualFold(side, "up"):
		return "Up", true
	case strings.EqualFold(side, "down"):
		return "Down", true
	case strings.EqualFold(side, "over"):
		return "Over", true
	case strings.EqualFold(side, "under"):
		return "Under", true
	default:
		return "", false
	}
}
