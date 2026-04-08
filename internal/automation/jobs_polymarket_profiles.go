package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

var polymarketProfilesSpec = scheduler.ScheduleSpec{
	Type: scheduler.ScheduleTypeCron,
	Cron: "*/20 * * * *", // every 20 minutes, 24/7
}

// polymarketProfileMinWinRate is the win rate threshold for auto-flagging accounts.
const polymarketProfileMinWinRate = 0.70

// polymarketProfileMinResolved is the minimum number of resolved markets required.
const polymarketProfileMinResolved = 20

func (o *JobOrchestrator) registerPolymarketProfileJob() {
	if o.deps.PolymarketAccountRepo == nil {
		return // optional dependency — skip if not wired
	}
	o.Register(
		"polymarket_profiles",
		"Fetch recent Polymarket trades and update account profiles",
		polymarketProfilesSpec,
		o.polymarketProfiles,
	)
}

// polymarketProfiles fetches recent trades from the CLOB API, upserts account
// profiles, and auto-flags high-edge accounts as tracked.
func (o *JobOrchestrator) polymarketProfiles(ctx context.Context) error {
	repo := o.deps.PolymarketAccountRepo
	clobURL := o.deps.PolymarketCLOBURL
	if clobURL == "" {
		clobURL = "https://clob.polymarket.com"
	}

	trades, err := fetchRecentCLOBTrades(ctx, clobURL, 500)
	if err != nil {
		return fmt.Errorf("polymarket_profiles: fetch trades: %w", err)
	}

	if len(trades) == 0 {
		o.logger.Info("polymarket_profiles: no recent trades fetched")
		return nil
	}

	// Aggregate per-address stats from the trade batch.
	type accStats struct {
		totalTrades int
		totalVolume float64
		maxPosition float64
		markets     map[string]struct{}
		lastActive  time.Time
	}
	statsMap := make(map[string]*accStats)
	domainTrades := make([]domain.PolymarketAccountTrade, 0, len(trades))

	for _, t := range trades {
		if t.MakerAddress == "" {
			continue
		}
		s, ok := statsMap[t.MakerAddress]
		if !ok {
			s = &accStats{markets: make(map[string]struct{})}
			statsMap[t.MakerAddress] = s
		}
		s.totalTrades++
		s.totalVolume += t.SizeUSDC
		if t.SizeUSDC > s.maxPosition {
			s.maxPosition = t.SizeUSDC
		}
		s.markets[t.MarketSlug] = struct{}{}
		if t.Timestamp.After(s.lastActive) {
			s.lastActive = t.Timestamp
		}

		domainTrades = append(domainTrades, domain.PolymarketAccountTrade{
			AccountAddress: t.MakerAddress,
			MarketSlug:     t.MarketSlug,
			Side:           t.Side,
			Action:         "buy",
			Price:          t.Price,
			SizeUSDC:       t.SizeUSDC,
			Timestamp:      t.Timestamp,
		})
	}

	// Upsert accounts (insert new, update volumes/trades for existing).
	now := time.Now()
	for address, s := range statsMap {
		lastActive := s.lastActive
		acc := &domain.PolymarketAccount{
			Address:        address,
			FirstSeen:      now,
			LastActive:     &lastActive,
			TotalTrades:    s.totalTrades,
			TotalVolume:    s.totalVolume,
			MarketsEntered: len(s.markets),
			MaxPosition:    s.maxPosition,
			UpdatedAt:      now,
		}

		// Fetch existing record to preserve historical stats.
		existing, err := repo.GetAccount(ctx, address)
		if err == nil {
			// Merge: keep historical totals, update recents.
			acc.FirstSeen = existing.FirstSeen
			acc.TotalTrades += existing.TotalTrades
			acc.TotalVolume += existing.TotalVolume
			acc.MarketsEntered += existing.MarketsEntered
			acc.MarketsWon = existing.MarketsWon
			acc.MarketsLost = existing.MarketsLost
			acc.WinRate = existing.WinRate
			acc.Tracked = existing.Tracked
			acc.Tags = existing.Tags
			if existing.MaxPosition > acc.MaxPosition {
				acc.MaxPosition = existing.MaxPosition
			}
		}

		if upsertErr := repo.UpsertAccount(ctx, acc); upsertErr != nil {
			o.logger.Warn("polymarket_profiles: upsert account failed",
				slog.String("address", address),
				slog.Any("error", upsertErr),
			)
		}
	}

	// Persist trade records.
	if err := repo.InsertTrades(ctx, domainTrades); err != nil {
		o.logger.Warn("polymarket_profiles: insert trades failed", slog.Any("error", err))
	}

	// Auto-flag high-edge accounts.
	marked, err := repo.MarkTracked(ctx, polymarketProfileMinWinRate, polymarketProfileMinResolved)
	if err != nil {
		o.logger.Warn("polymarket_profiles: mark tracked failed", slog.Any("error", err))
	} else if marked > 0 {
		o.logger.Info("polymarket_profiles: auto-flagged high-edge accounts",
			slog.Int64("count", marked))
	}

	o.logger.Info("polymarket_profiles: done",
		slog.Int("trades", len(domainTrades)),
		slog.Int("accounts", len(statsMap)),
	)
	return nil
}

// — CLOB trades API —

type clobTrade struct {
	MakerAddress string    `json:"owner"` // wallet address of the taker
	MarketSlug   string    `json:"market"`
	Side         string    `json:"outcome"` // "YES" or "NO"
	Price        float64
	SizeUSDC     float64
	Timestamp    time.Time
}

type clobTradesResponse struct {
	Data []clobTradeRaw `json:"data"`
}

type clobTradeRaw struct {
	Owner      string `json:"owner"`
	Market     string `json:"market"`
	Outcome    string `json:"outcome"`
	Price      string `json:"price"`
	Size       string `json:"size"`
	MatchTime  string `json:"match_time"`
}

func fetchRecentCLOBTrades(ctx context.Context, clobURL string, limit int) ([]clobTrade, error) {
	u, err := url.Parse(clobURL + "/data/trades")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("limit", fmt.Sprintf("%d", limit))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CLOB trades HTTP %d", resp.StatusCode)
	}

	var raw clobTradesResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	trades := make([]clobTrade, 0, len(raw.Data))
	for _, r := range raw.Data {
		var price, size float64
		fmt.Sscanf(r.Price, "%f", &price)
		fmt.Sscanf(r.Size, "%f", &size)

		ts, _ := time.Parse(time.RFC3339, r.MatchTime)

		side := r.Outcome
		if side == "" {
			side = "YES"
		}

		trades = append(trades, clobTrade{
			MakerAddress: r.Owner,
			MarketSlug:   r.Market,
			Side:         side,
			Price:        price,
			SizeUSDC:     size,
			Timestamp:    ts,
		})
	}
	return trades, nil
}
