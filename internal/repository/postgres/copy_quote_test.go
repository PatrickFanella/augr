package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/copytrading"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestCopyQuoteRetainedQualification(t *testing.T) {
	databaseURL := os.Getenv("COPY_QUOTE_QUALIFICATION_DB_URL")
	if databaseURL == "" {
		t.Skip("set COPY_QUOTE_QUALIFICATION_DB_URL to a dedicated empty schema-89 database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version, existing int
	if err = pool.QueryRow(ctx, `SELECT version FROM schema_migrations WHERE NOT dirty`).Scan(&version); err != nil || version != 89 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM copy_trade_intents WHERE quote_gate_version=1`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("existing=%d/%v", existing, err)
	}
	now := time.Date(2026, 8, 20, 18, 0, 0, 0, time.UTC)
	leaderID, sourceID := uuid.New(), uuid.New()
	if _, err = pool.Exec(ctx, `INSERT INTO copy_leaders(id,entity_type,display_name) VALUES($1,'institution','OVR502 fixture')`, leaderID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO copy_leader_sources(id,leader_id,provider,source_type,external_key) VALUES($1,$2,'sec','sec_13f',$3)`, sourceID, leaderID, sourceID.String()); err != nil {
		t.Fatal(err)
	}
	subscription := domain.DefaultCopySubscription()
	subscription.ID, subscription.LeaderID, subscription.SourceID = uuid.New(), leaderID, sourceID
	subscription.OriginType, subscription.OriginID, subscription.CreatedBy = "copy_subscription", subscription.ID, "ovr502"
	subscription.Status, subscription.MinAvgDollarVolume, subscription.MinPrice = domain.CopySubscriptionPaperActive, 0, 0
	if err = subscription.Validate(); err != nil {
		t.Fatal(err)
	}
	repo := NewCopyTradingRepo(pool)
	if err = repo.CreateSubscription(ctx, &subscription); err != nil {
		t.Fatal(err)
	}
	aapl, aaplQuote := copyQuoteFixture(t, ctx, pool, now, "aapl", "99.5", "100.5", now.Add(-60*time.Second))
	msft, msftQuote := copyQuoteFixture(t, ctx, pool, now, "msft", "49.99", "50.01", now.Add(-time.Second))
	_ = aapl
	_ = msft
	observationID := insertCopyQuoteObservation(t, ctx, pool, sourceID, "filing-approved", now)
	preview := copytrading.Build13FTarget(copytrading.TargetInput{
		Subscription: subscription,
		Observation:  domain.CopySourceObservation{ID: observationID},
		Snapshot:     domain.CopyPortfolioSnapshot{TotalDisclosedValue: 100, Holdings: []domain.CopyPortfolioHolding{{CUSIP: "AAA", DisclosedValue: 100}}},
		Mappings:     []domain.CopyInstrumentMapping{{IdentifierValue: "AAA", Ticker: "AAPL", Confidence: "manual_verified"}},
		Prices: map[string]copytrading.PriceSnapshot{
			"AAPL": copyQuotePrice(aaplQuote, "AAPL"),
			"MSFT": copyQuotePrice(msftQuote, "MSFT"),
		},
		Positions:  []domain.Position{{Ticker: "MSFT", Quantity: 10, AvgEntry: 50}},
		DecisionAt: now,
	})
	if len(preview.Intents) != 2 {
		t.Fatalf("approved preview=%+v", preview.Intents)
	}
	for i := range preview.Intents {
		if preview.Intents[i].PolicyStatus != "approved" {
			t.Fatalf("approved intent=%+v", preview.Intents[i])
		}
		if created, createErr := repo.CreateIntent(ctx, &preview.Intents[i]); createErr != nil || !created {
			t.Fatalf("approved create=%t/%v", created, createErr)
		}
	}
	identical := preview.Intents[0]
	if created, replayErr := repo.CreateIntent(ctx, &identical); replayErr != nil || created {
		t.Fatalf("identical retry=%t/%v", created, replayErr)
	}
	changed := preview.Intents[0]
	changed.RequestedNotional++
	if created, replayErr := repo.CreateIntent(ctx, &changed); replayErr == nil || created || !errors.Is(replayErr, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed retry=%t/%v", created, replayErr)
	}
	staleObservation := insertCopyQuoteObservation(t, ctx, pool, sourceID, "filing-stale", now.Add(time.Second))
	_, persistedStaleQuote := copyQuoteFixture(t, ctx, pool, now, "stale", "99.99", "100.01", now.Add(-61*time.Second))
	staleQuote := copyQuotePrice(persistedStaleQuote, "AAPL")
	rejected := copytrading.Build13FTarget(copytrading.TargetInput{Subscription: subscription, Observation: domain.CopySourceObservation{ID: staleObservation}, Snapshot: domain.CopyPortfolioSnapshot{TotalDisclosedValue: 100, Holdings: []domain.CopyPortfolioHolding{{CUSIP: "AAA", DisclosedValue: 100}}}, Mappings: []domain.CopyInstrumentMapping{{IdentifierValue: "AAA", Ticker: "AAPL", Confidence: "manual_verified"}}, Prices: map[string]copytrading.PriceSnapshot{"AAPL": staleQuote}, DecisionAt: now})
	if len(rejected.Intents) != 1 || rejected.Intents[0].PolicyStatus != "skipped" || !containsCopyReason(rejected.Intents[0].PolicyReasons, "stale_quote") {
		t.Fatalf("rejected=%+v", rejected.Intents)
	}
	if created, createErr := repo.CreateIntent(ctx, &rejected.Intents[0]); createErr != nil || !created {
		t.Fatalf("rejected create=%t/%v", created, createErr)
	}
	loaded, err := repo.ListIntents(ctx, subscription.ID, 10, 0)
	if err != nil || len(loaded) != 3 {
		t.Fatalf("loaded=%+v/%v", loaded, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE copy_trade_intents SET decision_spread_bps=decision_spread_bps+1 WHERE id=$1`, preview.Intents[0].ID); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("forged spread=%v", err)
	}
	var approved, skipped int
	if err = pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE policy_status='approved'),count(*) FILTER(WHERE policy_status='skipped') FROM copy_trade_intents WHERE quote_gate_version=1`).Scan(&approved, &skipped); err != nil || approved != 2 || skipped != 1 {
		t.Fatalf("counts=%d/%d/%v", approved, skipped, err)
	}
	t.Logf("subscription=%s approved=%d skipped=%d buy_quote=%s sell_quote=%s buy_spread=%s sell_spread=%s", subscription.ID, approved, skipped, aaplQuote.ID, msftQuote.ID, preview.Intents[0].DecisionSpreadBPS, preview.Intents[1].DecisionSpreadBPS)
}

func copyQuoteFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time, key, bidValue, askValue string, available time.Time) (*instrument.Instrument, *marketdata.QuoteSnapshot) {
	t.Helper()
	reference, err := instrument.NewInstrument(instrument.InstrumentInput{IdentityKey: "equity:" + key, AssetClass: instrument.AssetClassEquity, PrimaryVenue: "alpaca", Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1), SettlementMethod: instrument.SettlementPhysical, Status: instrument.StatusActive, Metadata: json.RawMessage(`{}`), CreatedAt: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	reference, err = NewInstrumentRepo(pool).CreateInstrument(ctx, reference)
	if err != nil {
		t.Fatal(err)
	}
	exchange := available.Add(-time.Second)
	bid, ask := decimal.RequireFromString(bidValue), decimal.RequireFromString(askValue)
	quote, err := marketdata.NewQuoteSnapshot(marketdata.QuoteSnapshotInput{InstrumentID: reference.ID, Provider: "alpaca", Venue: "alpaca", Source: "fixture", ObservationNamespace: "quotes/alpaca", ObservationID: "quote-" + key, ExchangeAt: &exchange, ReceivedAt: exchange.Add(time.Millisecond), AvailableAt: &available, Bid: &bid, Ask: &ask, MarketStatus: "open", SessionStatus: "regular", Metadata: json.RawMessage(`{}`), CreatedAt: available})
	if err != nil {
		t.Fatal(err)
	}
	quote, err = NewQuoteSnapshotRepo(pool).RecordQuoteSnapshot(ctx, quote)
	if err != nil {
		t.Fatal(err)
	}
	return reference, quote
}

func copyQuotePrice(quote *marketdata.QuoteSnapshot, ticker string) copytrading.PriceSnapshot {
	return copytrading.PriceSnapshot{Ticker: ticker, QuoteSnapshotID: quote.ID, Bid: quote.Bid.String(), Ask: quote.Ask.String(), AvailableAt: quote.AvailableAt, MarketStatus: quote.MarketStatus, SessionStatus: quote.SessionStatus}
}

func insertCopyQuoteObservation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sourceID uuid.UUID, key string, now time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO copy_source_observations(id,source_id,provider_observation_id,observation_kind,effective_at,published_at,observed_at,content_hash,normalized_payload) VALUES($1,$2,$3,'portfolio_snapshot',$4,$5,$6,$7,'{}')`, id, sourceID, key, now.Add(-24*time.Hour), now.Add(-time.Hour), now, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	return id
}

func containsCopyReason(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
