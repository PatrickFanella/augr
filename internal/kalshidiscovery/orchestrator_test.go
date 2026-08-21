package kalshidiscovery

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestDeployStrategyCreatesInactiveKalshiResearchIdeaWithMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newKalshiStrategyRepo()
	candidate := newKalshiCandidate("KX-TEST", "EVT-TEST", "Will test happen?", 0.41, 0.45, 0.55, 0.59, 2500, 1500, 7*24*time.Hour)
	proposal, err := buildDeterministicProposal(candidate)
	if err != nil {
		t.Fatalf("buildDeterministicProposal() error = %v", err)
	}

	deployed, err := DeployStrategy(ctx, Config{ScheduleCron: "0 */6 * * *"}, Deps{Strategies: repo}, candidate, proposal)
	if err != nil {
		t.Fatalf("DeployStrategy() error = %v", err)
	}
	if deployed.Ticker != candidate.Ticker || deployed.Template != kalshiProposalTemplate || deployed.Direction != proposal.Direction {
		t.Fatalf("deployed summary = %#v, want candidate-linked deployment", deployed)
	}
	if !repo.created[0].IsPaper || repo.created[0].Status != domain.StrategyStatusInactive || repo.created[0].MarketType != domain.MarketTypeKalshi {
		t.Fatalf("created strategy = %#v, want inactive paper Kalshi research idea", repo.created[0])
	}
	if repo.created[0].ScheduleCron != "" {
		t.Fatalf("ScheduleCron = %q, want unscheduled", repo.created[0].ScheduleCron)
	}

	var configJSON map[string]any
	if err := json.Unmarshal(repo.created[0].Config, &configJSON); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	meta, ok := configJSON["discovery_meta"].(map[string]any)
	if !ok {
		t.Fatalf("discovery_meta missing or wrong type: %#v", configJSON["discovery_meta"])
	}
	for _, key := range []string{"source", "market_ticker", "event_ticker", "template", "direction", "conviction", "time_horizon", "entry_price_max", "source_references", "max_spread_pct", "min_liquidity", "stop_policy", "target_policy", "native_execution_required"} {
		if _, ok := meta[key]; !ok {
			t.Fatalf("missing discovery_meta key %q in %v", key, meta)
		}
	}
	if got := meta["source"]; got != "kalshi_discovery" {
		t.Fatalf("source = %v, want kalshi_discovery", got)
	}
	if got := meta["market_ticker"]; got != candidate.Ticker {
		t.Fatalf("market_ticker = %v, want %s", got, candidate.Ticker)
	}
	if got := meta["native_execution_required"]; got != true {
		t.Fatalf("native_execution_required = %v, want true", got)
	}
	if lifecycle, ok := configJSON["research_lifecycle"].(map[string]any); !ok || lifecycle["stage"] != "idea" || lifecycle["auto_activation_blocked"] != true {
		t.Fatalf("research_lifecycle = %#v, want blocked idea", configJSON["research_lifecycle"])
	}
}

func TestDeployStrategyDryRunDoesNotPersistStrategy(t *testing.T) {
	t.Parallel()

	candidate := newKalshiCandidate("KX-DRY", "EVT-DRY", "Dry run market?", 0.40, 0.43, 0.56, 0.60, 2000, 1500, 10*24*time.Hour)
	proposal, err := buildDeterministicProposal(candidate)
	if err != nil {
		t.Fatalf("buildDeterministicProposal() error = %v", err)
	}
	repo := newKalshiStrategyRepo()
	deployed, err := DeployStrategy(context.Background(), Config{DryRun: true}, Deps{Strategies: repo}, candidate, proposal)
	if err != nil {
		t.Fatalf("DeployStrategy() error = %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatalf("expected no persisted strategy, got %d", len(repo.created))
	}
	if deployed.StrategyID == uuid.Nil {
		t.Fatal("dry-run should still return a generated strategy ID")
	}
}

func TestDeployStrategyRejectsInvalidProposalAndWrongSourceReference(t *testing.T) {
	t.Parallel()

	candidate := newKalshiCandidate("KX-ERR", "EVT-ERR", "Will error?", 0.42, 0.45, 0.55, 0.58, 2000, 1500, 10*24*time.Hour)
	proposal, err := buildDeterministicProposal(candidate)
	if err != nil {
		t.Fatalf("buildDeterministicProposal() error = %v", err)
	}

	t.Run("invalid proposal", func(t *testing.T) {
		bad := proposal
		bad.Direction = "MAYBE"
		_, err := DeployStrategy(context.Background(), Config{}, Deps{Strategies: newKalshiStrategyRepo()}, candidate, bad)
		if err == nil {
			t.Fatal("expected invalid proposal to reject deployment")
		}
	})

	t.Run("wrong source ref", func(t *testing.T) {
		bad := proposal
		bad.SourceReferences = []string{"kalshi_market:OTHER"}
		_, err := DeployStrategy(context.Background(), Config{}, Deps{Strategies: newKalshiStrategyRepo()}, candidate, bad)
		if err == nil || !strings.Contains(err.Error(), "source_references") {
			t.Fatalf("expected source reference rejection, got %v", err)
		}
	})
}

func TestDeployStrategyReusesExistingPaperStrategyForSameMarketTicker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newKalshiStrategyRepo()
	candidate := newKalshiCandidate("KX-REUSE", "EVT-REUSE", "Reuse market?", 0.43, 0.46, 0.54, 0.57, 2100, 1600, 9*24*time.Hour)
	proposal, err := buildDeterministicProposal(candidate)
	if err != nil {
		t.Fatalf("buildDeterministicProposal() error = %v", err)
	}

	first, err := DeployStrategy(ctx, Config{}, Deps{Strategies: repo}, candidate, proposal)
	if err != nil {
		t.Fatalf("first DeployStrategy() error = %v", err)
	}
	second, err := DeployStrategy(ctx, Config{}, Deps{Strategies: repo}, candidate, proposal)
	if err != nil {
		t.Fatalf("second DeployStrategy() error = %v", err)
	}
	if first.StrategyID != second.StrategyID || !second.Reused {
		t.Fatalf("expected reuse, got first=%#v second=%#v", first, second)
	}
	if got := len(repo.created); got != 1 {
		t.Fatalf("expected 1 persisted strategy, got %d", got)
	}
}

func TestRunFetchesScreensDeploysBoundedNumberAndRecordsRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	markets := []MarketCandidate{
		newKalshiCandidate("KX-1", "EVT-1", "Market 1?", 0.40, 0.43, 0.56, 0.59, 5000, 3000, 12*24*time.Hour),
		newKalshiCandidate("KX-2", "EVT-2", "Market 2?", 0.41, 0.44, 0.55, 0.58, 4800, 2800, 11*24*time.Hour),
		newKalshiCandidate("KX-3", "EVT-3", "Market 3?", 0.39, 0.42, 0.57, 0.60, 4700, 2600, 10*24*time.Hour),
		newKalshiCandidate("KX-4", "EVT-4", "Market 4?", 0.38, 0.41, 0.58, 0.61, 4600, 2500, 9*24*time.Hour),
		newKalshiCandidate("KX-5", "EVT-5", "Market 5?", 0.37, 0.40, 0.59, 0.62, 4500, 2400, 8*24*time.Hour),
	}
	client := &fakeKalshiClient{pages: []fakeKalshiPage{{markets: markets}}}
	strategyRepo := newKalshiStrategyRepo()
	runRepo := &fakeKalshiDiscoveryRunRepo{}
	watchedRepo := &fakeKalshiWatchedRepo{}
	snapshotsRepo := &fakeKalshiSnapshotsRepo{}

	res, err := Run(ctx, Config{FetchLimit: 7, MaxDeployments: 2, MinConviction: 0.55, Screener: ScreenerConfig{MaxCandidates: 5, MinVolume: 100, MinOpenInterest: 50, MaxSpreadPct: 20, MinDaysToClose: 1}}, Deps{Catalog: client, Strategies: strategyRepo, Watched: watchedRepo, Snapshots: snapshotsRepo, DiscoveryRuns: runRepo})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.FetchedAll != 5 || res.Screened != 5 || res.Proposed != 2 || len(res.Deployed) != 2 {
		t.Fatalf("Run() result = %#v, want bounded deployment of 2", res)
	}
	if len(strategyRepo.created) != 2 {
		t.Fatalf("expected 2 created strategies, got %d", len(strategyRepo.created))
	}
	if got := client.calls; len(got) != 1 || got[0].Limit != 7 || got[0].Status != "open" {
		t.Fatalf("ListMarkets calls = %#v, want one open request with fetch limit", got)
	}
	if runRepo.created != 1 || runRepo.finished != 1 || runRepo.lastFinished == nil || runRepo.lastFinished.Status != domain.KalshiDiscoveryStatusCompleted {
		t.Fatalf("discovery run recording = created:%d finished:%d last:%#v", runRepo.created, runRepo.finished, runRepo.lastFinished)
	}
	if len(watchedRepo.tickers) != 5 || len(snapshotsRepo.tickers) != 5 {
		t.Fatalf("expected all accepted markets to persist watched/snapshots, got watched=%d snapshots=%d", len(watchedRepo.tickers), len(snapshotsRepo.tickers))
	}
}

func TestRunCapsPaginatedCatalogAtFetchLimit(t *testing.T) {
	t.Parallel()

	client := &fakeKalshiClient{pages: []fakeKalshiPage{
		{markets: []MarketCandidate{
			newKalshiCandidate("KX-1", "EVT-1", "Market 1?", 0.40, 0.43, 0.56, 0.59, 5000, 3000, 12*24*time.Hour),
			newKalshiCandidate("KX-2", "EVT-2", "Market 2?", 0.41, 0.44, 0.55, 0.58, 4800, 2800, 11*24*time.Hour),
		}, cursor: "next"},
		{markets: []MarketCandidate{
			newKalshiCandidate("KX-3", "EVT-3", "Market 3?", 0.39, 0.42, 0.57, 0.60, 4700, 2600, 10*24*time.Hour),
			newKalshiCandidate("KX-4", "EVT-4", "Market 4?", 0.38, 0.41, 0.58, 0.61, 4600, 2500, 9*24*time.Hour),
		}, cursor: "more"},
	}}

	res, err := Run(context.Background(), Config{
		DryRun:         true,
		FetchLimit:     3,
		MaxDeployments: 1,
		MinConviction:  0.55,
		Screener:       ScreenerConfig{MaxCandidates: 5, MinVolume: 100, MinOpenInterest: 50, MaxSpreadPct: 20, MinDaysToClose: 1},
	}, Deps{Catalog: client})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.FetchedAll != 3 {
		t.Fatalf("FetchedAll = %d, want hard cap 3", res.FetchedAll)
	}
	if len(client.calls) != 2 {
		t.Fatalf("ListMarkets calls = %d, want 2 pages to reach cap", len(client.calls))
	}
}

func TestRunDryRunDoesNotPersistDiscoverySideEffects(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	markets := []MarketCandidate{
		newKalshiCandidate("KX-DRY-RUN", "EVT-DRY-RUN", "Dry run market?", 0.40, 0.43, 0.56, 0.59, 5000, 3000, 12*24*time.Hour),
	}
	client := &fakeKalshiClient{pages: []fakeKalshiPage{{markets: markets}}}
	strategyRepo := newKalshiStrategyRepo()
	runRepo := &fakeKalshiDiscoveryRunRepo{}
	watchedRepo := &fakeKalshiWatchedRepo{}
	snapshotsRepo := &fakeKalshiSnapshotsRepo{}

	res, err := Run(ctx, Config{DryRun: true, FetchLimit: 1, MaxDeployments: 1, MinConviction: 0.55, Screener: ScreenerConfig{MaxCandidates: 1, MinVolume: 100, MinOpenInterest: 50, MaxSpreadPct: 20, MinDaysToClose: 1}}, Deps{Catalog: client, Strategies: strategyRepo, Watched: watchedRepo, Snapshots: snapshotsRepo, DiscoveryRuns: runRepo})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.FetchedAll != 1 || res.Screened != 1 || res.Proposed != 1 || len(res.Deployed) != 1 || !res.DryRun {
		t.Fatalf("dry-run result = %#v, want in-memory discovery/deployment summary", res)
	}
	if len(strategyRepo.created) != 0 || runRepo.created != 0 || runRepo.finished != 0 || len(watchedRepo.tickers) != 0 || len(snapshotsRepo.tickers) != 0 {
		t.Fatalf("dry-run persisted side effects: strategies=%d runs=%d/%d watched=%d snapshots=%d", len(strategyRepo.created), runRepo.created, runRepo.finished, len(watchedRepo.tickers), len(snapshotsRepo.tickers))
	}
}

func newKalshiCandidate(ticker, eventTicker, title string, yesBid, yesAsk, noBid, noAsk, volume, openInterest float64, closeIn time.Duration) MarketCandidate {
	closeTime := time.Now().UTC().Add(closeIn)
	return MarketCandidate{
		Ticker:       ticker,
		EventTicker:  eventTicker,
		Title:        title,
		Category:     "weather",
		Status:       "open",
		YesBid:       yesBid,
		YesAsk:       yesAsk,
		NoBid:        noBid,
		NoAsk:        noAsk,
		Volume:       volume,
		OpenInterest: openInterest,
		CloseTime:    &closeTime,
	}
}

func TestBuildDeterministicProposalChoosesTighterSide(t *testing.T) {
	t.Parallel()

	candidate := newKalshiCandidate("KX-SIDE", "EVT-SIDE", "Side market?", 0.42, 0.51, 0.39, 0.44, 2000, 1200, 6*24*time.Hour)
	proposal, err := buildDeterministicProposal(candidate)
	if err != nil {
		t.Fatalf("buildDeterministicProposal() error = %v", err)
	}
	if proposal.Direction != "NO" || proposal.EntryPriceMax <= 0.44 || proposal.EntryPriceMax > 1 {
		t.Fatalf("proposal = %#v, want tighter NO side with buffered ceiling", proposal)
	}
}

type fakeKalshiPage struct {
	markets []MarketCandidate
	cursor  string
}

type fakeKalshiClient struct {
	pages []fakeKalshiPage
	calls []ListOptions
	idx   int
}

func (f *fakeKalshiClient) ListMarkets(_ context.Context, opts ListOptions) ([]MarketCandidate, string, error) {
	f.calls = append(f.calls, opts)
	if f.idx >= len(f.pages) {
		return nil, "", nil
	}
	page := f.pages[f.idx]
	f.idx++
	return page.markets, page.cursor, nil
}

type fakeKalshiStrategyRepo struct {
	created      []domain.Strategy
	theses       map[uuid.UUID]json.RawMessage
	fail         error
	conflictOnce bool
	conflicted   bool
}

func newKalshiStrategyRepo() *fakeKalshiStrategyRepo {
	return &fakeKalshiStrategyRepo{theses: map[uuid.UUID]json.RawMessage{}}
}

func (r *fakeKalshiStrategyRepo) Create(_ context.Context, strategy *domain.Strategy) error {
	if r.fail != nil {
		return r.fail
	}
	if strategy.ID == uuid.Nil {
		strategy.ID = uuid.New()
	}
	if r.conflictOnce && !r.conflicted {
		r.conflicted = true
		r.created = append(r.created, *strategy)
		return errors.New(`ERROR: duplicate key value violates unique constraint "idx_strategies_discovery_unique" (SQLSTATE 23505)`)
	}
	r.created = append(r.created, *strategy)
	return nil
}

func (r *fakeKalshiStrategyRepo) Get(_ context.Context, id uuid.UUID) (*domain.Strategy, error) {
	for i := range r.created {
		if r.created[i].ID == id {
			cloned := r.created[i]
			return &cloned, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *fakeKalshiStrategyRepo) List(_ context.Context, filter repository.StrategyFilter, limit, offset int) ([]domain.Strategy, error) {
	var filtered []domain.Strategy
	for _, strategy := range r.created {
		if filter.Ticker != "" && strategy.Ticker != filter.Ticker {
			continue
		}
		if filter.MarketType != "" && strategy.MarketType != filter.MarketType {
			continue
		}
		if filter.Status != "" && strategy.Status != filter.Status {
			continue
		}
		if filter.IsPaper != nil && strategy.IsPaper != *filter.IsPaper {
			continue
		}
		filtered = append(filtered, strategy)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Name < filtered[j].Name })
	if offset > len(filtered) {
		return []domain.Strategy{}, nil
	}
	filtered = filtered[offset:]
	if limit > 0 && limit < len(filtered) {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (r *fakeKalshiStrategyRepo) Count(ctx context.Context, filter repository.StrategyFilter) (int, error) {
	listed, err := r.List(ctx, filter, 0, 0)
	if err != nil {
		return 0, err
	}
	return len(listed), nil
}

func (r *fakeKalshiStrategyRepo) Update(_ context.Context, strategy *domain.Strategy) error {
	for i := range r.created {
		if r.created[i].ID == strategy.ID {
			r.created[i] = *strategy
			return nil
		}
	}
	return repository.ErrNotFound
}

func (r *fakeKalshiStrategyRepo) Delete(_ context.Context, id uuid.UUID) error {
	for i := range r.created {
		if r.created[i].ID == id {
			r.created = append(r.created[:i], r.created[i+1:]...)
			return nil
		}
	}
	return repository.ErrNotFound
}

func (r *fakeKalshiStrategyRepo) UpdateThesis(_ context.Context, id uuid.UUID, thesis json.RawMessage) error {
	if r.theses == nil {
		r.theses = map[uuid.UUID]json.RawMessage{}
	}
	r.theses[id] = thesis
	return nil
}

func (r *fakeKalshiStrategyRepo) GetThesisRaw(_ context.Context, id uuid.UUID) (json.RawMessage, error) {
	return r.theses[id], nil
}

var _ repository.StrategyRepository = (*fakeKalshiStrategyRepo)(nil)

type fakeKalshiDiscoveryRunRepo struct {
	created      int
	finished     int
	lastCreated  *domain.KalshiDiscoveryRun
	lastFinished *domain.KalshiDiscoveryRun
}

func (r *fakeKalshiDiscoveryRunRepo) Create(_ context.Context, run *domain.KalshiDiscoveryRun) error {
	r.created++
	cloned := *run
	r.lastCreated = &cloned
	return nil
}

func (r *fakeKalshiDiscoveryRunRepo) GetActive(context.Context) (*domain.KalshiDiscoveryRun, error) {
	return nil, repository.ErrNotFound
}

func (r *fakeKalshiDiscoveryRunRepo) Finish(_ context.Context, run *domain.KalshiDiscoveryRun) error {
	r.finished++
	cloned := *run
	r.lastFinished = &cloned
	return nil
}

func (r *fakeKalshiDiscoveryRunRepo) ListLatest(context.Context, int) ([]domain.KalshiDiscoveryRun, error) {
	return nil, nil
}

var _ repository.KalshiDiscoveryRunRepository = (*fakeKalshiDiscoveryRunRepo)(nil)

type fakeKalshiWatchedRepo struct{ tickers []string }

func (r *fakeKalshiWatchedRepo) Upsert(_ context.Context, market *domain.KalshiWatchedMarket) error {
	r.tickers = append(r.tickers, market.Ticker)
	return nil
}
func (r *fakeKalshiWatchedRepo) SetEnabled(context.Context, string, bool) error { return nil }
func (r *fakeKalshiWatchedRepo) ListEnabled(context.Context) ([]domain.KalshiWatchedMarket, error) {
	return nil, nil
}

var _ repository.KalshiWatchedMarketsRepository = (*fakeKalshiWatchedRepo)(nil)

type fakeKalshiSnapshotsRepo struct{ tickers []string }

func (r *fakeKalshiSnapshotsRepo) Create(_ context.Context, snapshot *domain.KalshiMarketSnapshot) error {
	r.tickers = append(r.tickers, snapshot.Ticker)
	return nil
}

func (r *fakeKalshiSnapshotsRepo) ListLatestByTicker(context.Context, string, int) ([]domain.KalshiMarketSnapshot, error) {
	return nil, nil
}

func (r *fakeKalshiSnapshotsRepo) ListRecent(context.Context, int) ([]domain.KalshiMarketSnapshot, error) {
	return nil, nil
}

var _ repository.KalshiMarketSnapshotsRepository = (*fakeKalshiSnapshotsRepo)(nil)

func TestCreateOrReusePaperStrategyKeepsSingleKalshiPaperStrategy(t *testing.T) {
	t.Parallel()

	repo := newKalshiStrategyRepo()
	ctx := context.Background()
	strategy := domain.Strategy{Name: kalshiStrategyName(MarketCandidate{Ticker: "KX-HELPER"}), Ticker: "KX-HELPER", MarketType: domain.MarketTypeKalshi, IsPaper: true, Status: domain.StrategyStatusActive}
	created, didCreate, err := discovery.CreateOrReusePaperStrategy(ctx, repo, strategy)
	if err != nil || !didCreate {
		t.Fatalf("CreateOrReusePaperStrategy() first = created %v err %v", didCreate, err)
	}
	second, didCreate, err := discovery.CreateOrReusePaperStrategy(ctx, repo, strategy)
	if err != nil || didCreate {
		t.Fatalf("CreateOrReusePaperStrategy() second = created %v err %v", didCreate, err)
	}
	if created.ID != second.ID {
		t.Fatalf("reused strategy ID = %s, want %s", second.ID, created.ID)
	}
}
