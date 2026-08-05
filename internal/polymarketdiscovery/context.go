package polymarketdiscovery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// MarketContext bundles a candidate market with supporting evidence used by
// the LLM strategy generator.
type MarketContext struct {
	Market         GammaMarket
	DaysToResolve  int
	WhaleTrades    []domain.PolymarketAccountTrade
	TrackedWallets []TrackedWalletStat
}

// TrackedWalletStat summarises a tracked wallet's involvement in this market.
type TrackedWalletStat struct {
	Address    string
	WinRate    float64
	BuyVolume  float64
	SellVolume float64
	LastSide   string
	LastAction string
	LastPrice  float64
	LastAt     time.Time
}

// BuildMarketContext gathers wallet evidence for a single market slug.
func BuildMarketContext(
	ctx context.Context,
	m GammaMarket,
	accountRepo repository.PolymarketAccountRepository,
) (MarketContext, error) {
	mc := MarketContext{Market: m}
	if end := m.EndTime(); !end.IsZero() {
		mc.DaysToResolve = int(time.Until(end).Hours() / 24)
	}

	if accountRepo == nil {
		return mc, nil
	}

	trades, err := accountRepo.ListAllTradesBySlug(ctx, m.Slug, 500)
	if err != nil {
		return mc, fmt.Errorf("list trades by slug: %w", err)
	}
	// Keep most recent 25 trades for context size.
	if len(trades) > 25 {
		mc.WhaleTrades = trades[:25]
	} else {
		mc.WhaleTrades = trades
	}

	// Aggregate per-wallet stats from the full trade list and look up tracked.
	stats := map[string]*TrackedWalletStat{}
	for _, t := range trades {
		s, ok := stats[t.AccountAddress]
		if !ok {
			s = &TrackedWalletStat{Address: t.AccountAddress}
			stats[t.AccountAddress] = s
		}
		if strings.EqualFold(t.Action, "buy") {
			s.BuyVolume += t.SizeUSDC
		} else {
			s.SellVolume += t.SizeUSDC
		}
		if t.Timestamp.After(s.LastAt) {
			s.LastAt = t.Timestamp
			s.LastSide = t.Side
			s.LastAction = t.Action
			s.LastPrice = t.Price
		}
	}

	// Hydrate win rates only for accounts the repo has profiled.
	for addr, s := range stats {
		acc, err := accountRepo.GetAccount(ctx, addr)
		if err != nil || acc == nil {
			continue
		}
		s.WinRate = acc.WinRate
		if !acc.Tracked {
			continue
		}
		mc.TrackedWallets = append(mc.TrackedWallets, *s)
	}
	sort.SliceStable(mc.TrackedWallets, func(i, j int) bool {
		return mc.TrackedWallets[i].WinRate > mc.TrackedWallets[j].WinRate
	})
	if len(mc.TrackedWallets) > 10 {
		mc.TrackedWallets = mc.TrackedWallets[:10]
	}
	return mc, nil
}

// promptSummary returns a concise textual snapshot fed to the LLM.
func (mc MarketContext) promptSummary() string {
	var sb strings.Builder
	m := mc.Market

	fmt.Fprintf(&sb, "Market slug: %s\n", m.Slug)
	fmt.Fprintf(&sb, "Question: %s\n", m.Question)
	if m.Category != "" {
		fmt.Fprintf(&sb, "Category: %s\n", m.Category)
	}
	if !m.EndTime().IsZero() {
		fmt.Fprintf(&sb, "Resolves: %s (T-%d days)\n", m.EndTime().Format(time.RFC3339), mc.DaysToResolve)
	}
	fmt.Fprintf(&sb, "Volume24h: $%.0f  Liquidity: $%.0f\n", m.Volume24HrFloat(), m.LiquidityFloat())
	if bid, ok := m.BestBidFloat(); ok {
		ask, _ := m.BestAskFloat()
		fmt.Fprintf(&sb, "YES bid/ask: %.3f / %.3f\n", bid, ask)
	}
	if last, ok := m.LastPriceFloat(); ok {
		fmt.Fprintf(&sb, "Last trade price (YES): %.3f\n", last)
	}
	if m.ResolutionSource != "" {
		fmt.Fprintf(&sb, "Resolution source: %s\n", m.ResolutionSource)
	}
	if d := strings.TrimSpace(m.Description); d != "" {
		if len(d) > 600 {
			d = d[:600] + "..."
		}
		fmt.Fprintf(&sb, "Description: %s\n", d)
	}

	if len(mc.TrackedWallets) > 0 {
		sb.WriteString("\nTracked wallets active in this market (high WR):\n")
		for _, w := range mc.TrackedWallets {
			fmt.Fprintf(&sb,
				"  %s  WR=%.1f%%  buy=$%.0f sell=$%.0f  last=%s %s @ %.3f (%s)\n",
				short(w.Address), w.WinRate*100, w.BuyVolume, w.SellVolume,
				w.LastAction, w.LastSide, w.LastPrice, w.LastAt.Format("2006-01-02 15:04"),
			)
		}
	} else if len(mc.WhaleTrades) > 0 {
		sb.WriteString("\nNo tracked wallets yet. Recent trades in this market:\n")
		for _, t := range mc.WhaleTrades[:min(5, len(mc.WhaleTrades))] {
			fmt.Fprintf(&sb,
				"  %s  %s %s @ %.3f size=$%.0f (%s)\n",
				short(t.AccountAddress), t.Action, t.Side, t.Price, t.SizeUSDC,
				t.Timestamp.Format("2006-01-02 15:04"),
			)
		}
	}
	return sb.String()
}

func short(addr string) string {
	if len(addr) <= 10 {
		return addr
	}
	return addr[:6] + ".." + addr[len(addr)-4:]
}
