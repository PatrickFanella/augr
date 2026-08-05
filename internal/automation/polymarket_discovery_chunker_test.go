package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/polymarketdiscovery"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type inMemoryPolymarketDiscoveryRunRepo struct {
	run                      *domain.PolymarketDiscoveryRun
	updated                  []*domain.PolymarketDiscoveryRun
	failOnCancelledUpdateCtx bool
}

func (r *inMemoryPolymarketDiscoveryRunRepo) clone(run *domain.PolymarketDiscoveryRun) *domain.PolymarketDiscoveryRun {
	if run == nil {
		return nil
	}
	cp := *run
	cp.Candidates = append([]domain.PolymarketDiscoveryCandidate(nil), run.Candidates...)
	cp.Accepted = append([]domain.PolymarketDiscoveryAccepted(nil), run.Accepted...)
	cp.Deployed = append([]domain.PolymarketDiscoveryDeployed(nil), run.Deployed...)
	cp.Errors = append([]string(nil), run.Errors...)
	return &cp
}

func (r *inMemoryPolymarketDiscoveryRunRepo) Create(_ context.Context, run *domain.PolymarketDiscoveryRun) error {
	r.run = r.clone(run)
	return nil
}

func (r *inMemoryPolymarketDiscoveryRunRepo) Get(_ context.Context, id uuid.UUID) (*domain.PolymarketDiscoveryRun, error) {
	if r.run == nil || r.run.ID != id {
		return nil, repository.ErrNotFound
	}
	return r.clone(r.run), nil
}

func (r *inMemoryPolymarketDiscoveryRunRepo) GetActive(_ context.Context) (*domain.PolymarketDiscoveryRun, error) {
	if r.run == nil || r.run.Status != domain.PolymarketDiscoveryStatusRunning {
		return nil, repository.ErrNotFound
	}
	return r.clone(r.run), nil
}

func (r *inMemoryPolymarketDiscoveryRunRepo) Update(ctx context.Context, run *domain.PolymarketDiscoveryRun) error {
	if r.failOnCancelledUpdateCtx {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	r.updated = append(r.updated, r.clone(run))
	r.run = r.clone(run)
	return nil
}

func (r *inMemoryPolymarketDiscoveryRunRepo) ListLatest(_ context.Context, _ int) ([]domain.PolymarketDiscoveryRun, error) {
	if r.run == nil {
		return nil, nil
	}
	return []domain.PolymarketDiscoveryRun{*r.clone(r.run)}, nil
}

func TestPolymarketDiscoveryChunkerProposeBudget(t *testing.T) {
	defer restoreDiscoverySeams()
	repo := &inMemoryPolymarketDiscoveryRunRepo{run: &domain.PolymarketDiscoveryRun{ID: uuid.New(), Status: domain.PolymarketDiscoveryStatusRunning, Phase: domain.PolymarketDiscoveryPhasePropose, Candidates: makeCandidates(5)}}
	polymarketDiscoveryGenerateProposal = func(_ context.Context, _ polymarketdiscovery.GeneratorConfig, mc polymarketdiscovery.MarketContext, _ *slog.Logger) (*polymarketdiscovery.Proposal, error) {
		return &polymarketdiscovery.Proposal{Conviction: 0.9, Name: mc.Market.Slug}, nil
	}
	deployed := 0
	polymarketDiscoveryDeployStrategy = func(_ context.Context, _ polymarketdiscovery.Config, _ polymarketdiscovery.Deps, mc polymarketdiscovery.MarketContext, prop polymarketdiscovery.Proposal) (polymarketdiscovery.DeployedStrategy, error) {
		deployed++
		return polymarketdiscovery.DeployedStrategy{StrategyID: uuid.New(), Slug: mc.Market.Slug, Name: prop.Name, Direction: prop.Direction, Conviction: prop.Conviction}, nil
	}
	c := polymarketDiscoveryChunker{progress: repo, deps: OrchestratorDeps{LLMProvider: llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil })}, proposePerChunk: 2}
	if err := c.runPropose(context.Background(), repo.run); err != nil {
		t.Fatal(err)
	}
	if deployed != 2 || repo.run.CandidateIndex != 2 || len(repo.run.Accepted) != 2 || len(repo.run.Deployed) != 2 || repo.run.Phase != domain.PolymarketDiscoveryPhasePropose {
		t.Fatalf("unexpected run: %+v", repo.run)
	}
}

func TestPolymarketDiscoveryChunkerReplacesStaleRun(t *testing.T) {
	defer restoreDiscoverySeams()
	staleID := uuid.New()
	repo := &inMemoryPolymarketDiscoveryRunRepo{run: &domain.PolymarketDiscoveryRun{ID: staleID, Status: domain.PolymarketDiscoveryStatusRunning, Phase: domain.PolymarketDiscoveryPhasePropose, StartedAt: time.Now().Add(-polymarketDiscoveryMaxRunAge - time.Hour), Candidates: makeCandidates(1)}}
	polymarketDiscoveryFetchOpenMarkets = func(_ context.Context, _ string, _ int) ([]polymarketdiscovery.GammaMarket, error) {
		return []polymarketdiscovery.GammaMarket{{Slug: "fresh", Question: "Will fresh happen?", Active: true, AcceptingOrders: true}}, nil
	}
	polymarketDiscoveryScreenMarkets = func(markets []polymarketdiscovery.GammaMarket, _ polymarketdiscovery.ScreenerConfig) []polymarketdiscovery.GammaMarket {
		return markets
	}

	c := polymarketDiscoveryChunker{progress: repo, deps: OrchestratorDeps{}, logger: slog.Default()}
	if err := c.RunChunk(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repo.run.ID == staleID {
		t.Fatalf("expected replacement run, still have stale id %s", staleID)
	}
	if repo.run.Status != domain.PolymarketDiscoveryStatusRunning || repo.run.Phase != domain.PolymarketDiscoveryPhasePropose {
		t.Fatalf("replacement run not screened: %+v", repo.run)
	}
	if repo.run.Summary.FetchedAll != 1 || repo.run.Summary.Screened != 1 {
		t.Fatalf("replacement summary = %+v", repo.run.Summary)
	}
}

func TestPolymarketDiscoveryChunkerStopsAtMaxDeployments(t *testing.T) {
	defer restoreDiscoverySeams()
	repo := &inMemoryPolymarketDiscoveryRunRepo{run: &domain.PolymarketDiscoveryRun{ID: uuid.New(), Status: domain.PolymarketDiscoveryStatusRunning, Phase: domain.PolymarketDiscoveryPhasePropose, Candidates: makeCandidates(5)}}
	polymarketDiscoveryGenerateProposal = func(_ context.Context, _ polymarketdiscovery.GeneratorConfig, mc polymarketdiscovery.MarketContext, _ *slog.Logger) (*polymarketdiscovery.Proposal, error) {
		return &polymarketdiscovery.Proposal{Conviction: 0.9, Name: mc.Market.Slug}, nil
	}
	polymarketDiscoveryDeployStrategy = func(_ context.Context, _ polymarketdiscovery.Config, _ polymarketdiscovery.Deps, mc polymarketdiscovery.MarketContext, prop polymarketdiscovery.Proposal) (polymarketdiscovery.DeployedStrategy, error) {
		return polymarketdiscovery.DeployedStrategy{StrategyID: uuid.New(), Slug: mc.Market.Slug, Name: prop.Name, Direction: prop.Direction, Conviction: prop.Conviction}, nil
	}
	c := polymarketDiscoveryChunker{progress: repo, deps: OrchestratorDeps{LLMProvider: llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil })}, proposePerChunk: 5}
	if err := c.runPropose(context.Background(), repo.run); err != nil {
		t.Fatal(err)
	}
	if len(repo.run.Accepted) != 3 || len(repo.run.Deployed) != 3 || repo.run.Phase != domain.PolymarketDiscoveryPhaseDone || repo.run.Status != domain.PolymarketDiscoveryStatusCompleted || repo.run.CandidateIndex != 3 {
		t.Fatalf("unexpected run: %+v", repo.run)
	}
}

func TestPolymarketDiscoveryChunkerUsesFiveProposalChunks(t *testing.T) {
	defer restoreDiscoverySeams()
	chunker := newPolymarketDiscoveryChunker(OrchestratorDeps{}, nil)
	if chunker.proposePerChunk != 5 {
		t.Fatalf("proposePerChunk = %d, want 5", chunker.proposePerChunk)
	}
}

func TestPolymarketDiscoveryChunkerDeploysAcceptedProposalImmediately(t *testing.T) {
	defer restoreDiscoverySeams()
	repo := &inMemoryPolymarketDiscoveryRunRepo{run: &domain.PolymarketDiscoveryRun{ID: uuid.New(), Status: domain.PolymarketDiscoveryStatusRunning, Phase: domain.PolymarketDiscoveryPhasePropose, Candidates: makeCandidates(1)}}
	polymarketDiscoveryBuildContext = func(_ context.Context, m polymarketdiscovery.GammaMarket, _ repository.PolymarketAccountRepository) (polymarketdiscovery.MarketContext, error) {
		return polymarketdiscovery.MarketContext{Market: m}, nil
	}
	polymarketDiscoveryGenerateProposal = func(_ context.Context, _ polymarketdiscovery.GeneratorConfig, mc polymarketdiscovery.MarketContext, _ *slog.Logger) (*polymarketdiscovery.Proposal, error) {
		return &polymarketdiscovery.Proposal{Conviction: 0.9, Name: mc.Market.Slug}, nil
	}
	deployed := 0
	polymarketDiscoveryDeployStrategy = func(_ context.Context, _ polymarketdiscovery.Config, _ polymarketdiscovery.Deps, mc polymarketdiscovery.MarketContext, prop polymarketdiscovery.Proposal) (polymarketdiscovery.DeployedStrategy, error) {
		deployed++
		return polymarketdiscovery.DeployedStrategy{StrategyID: uuid.New(), Slug: mc.Market.Slug, Name: prop.Name, Direction: prop.Direction, Conviction: prop.Conviction}, nil
	}
	c := polymarketDiscoveryChunker{progress: repo, deps: OrchestratorDeps{LLMProvider: llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil })}, proposePerChunk: 5, logger: slog.Default()}
	if err := c.runPropose(context.Background(), repo.run); err != nil {
		t.Fatal(err)
	}
	if deployed != 1 {
		t.Fatalf("deployed = %d, want 1", deployed)
	}
	if repo.run.Phase != domain.PolymarketDiscoveryPhaseDone || repo.run.Status != domain.PolymarketDiscoveryStatusCompleted {
		t.Fatalf("run not completed immediately: %+v", repo.run)
	}
	if len(repo.run.Accepted) != 1 || len(repo.run.Deployed) != 1 {
		t.Fatalf("run not deployed immediately: %+v", repo.run)
	}
}

func TestPolymarketDiscoveryChunkerAdvancesOnErrorsAndSkips(t *testing.T) {
	defer restoreDiscoverySeams()
	repo := &inMemoryPolymarketDiscoveryRunRepo{run: &domain.PolymarketDiscoveryRun{ID: uuid.New(), Status: domain.PolymarketDiscoveryStatusRunning, Phase: domain.PolymarketDiscoveryPhasePropose, Candidates: []domain.PolymarketDiscoveryCandidate{{Slug: "bad-context"}, {Slug: "skip"}, {Slug: "low"}, {Slug: "ok"}}}}
	polymarketDiscoveryGenerateProposal = func(_ context.Context, _ polymarketdiscovery.GeneratorConfig, mc polymarketdiscovery.MarketContext, _ *slog.Logger) (*polymarketdiscovery.Proposal, error) {
		switch mc.Market.Slug {
		case "skip":
			return &polymarketdiscovery.Proposal{Skip: true}, nil
		case "low":
			return &polymarketdiscovery.Proposal{Conviction: 0.1}, nil
		default:
			return &polymarketdiscovery.Proposal{Conviction: 0.9, Name: mc.Market.Slug}, nil
		}
	}
	polymarketDiscoveryDeployStrategy = func(_ context.Context, _ polymarketdiscovery.Config, _ polymarketdiscovery.Deps, mc polymarketdiscovery.MarketContext, prop polymarketdiscovery.Proposal) (polymarketdiscovery.DeployedStrategy, error) {
		return polymarketdiscovery.DeployedStrategy{StrategyID: uuid.New(), Slug: mc.Market.Slug, Name: prop.Name, Direction: prop.Direction, Conviction: prop.Conviction}, nil
	}
	polymarketDiscoveryBuildContext = func(_ context.Context, m polymarketdiscovery.GammaMarket, _ repository.PolymarketAccountRepository) (polymarketdiscovery.MarketContext, error) {
		if m.Slug == "bad-context" {
			return polymarketdiscovery.MarketContext{}, errors.New("boom")
		}
		return polymarketdiscovery.MarketContext{Market: m}, nil
	}
	c := polymarketDiscoveryChunker{progress: repo, deps: OrchestratorDeps{LLMProvider: llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil })}, proposePerChunk: 4}
	if err := c.runPropose(context.Background(), repo.run); err != nil {
		t.Fatal(err)
	}
	if repo.run.CandidateIndex != 4 {
		t.Fatalf("candidate index = %d", repo.run.CandidateIndex)
	}
	if len(repo.run.Accepted) != 1 || len(repo.run.Deployed) != 1 || repo.run.Phase != domain.PolymarketDiscoveryPhaseDone || repo.run.Status != domain.PolymarketDiscoveryStatusCompleted {
		t.Fatalf("run not completed as expected: %+v", repo.run)
	}
	if len(repo.run.Errors) == 0 || fmt.Sprint(repo.run.Errors) == "" || !contains(repo.run.Errors, "bad-context") {
		t.Fatalf("errors = %#v", repo.run.Errors)
	}
}

func TestPolymarketDiscoveryChunkerDeployCompletesAndStoresResult(t *testing.T) {
	defer restoreDiscoverySeams()
	repo := &inMemoryPolymarketDiscoveryRunRepo{run: &domain.PolymarketDiscoveryRun{ID: uuid.New(), Status: domain.PolymarketDiscoveryStatusRunning, Phase: domain.PolymarketDiscoveryPhaseDeploy, StartedAt: time.Now().Add(-time.Minute), Accepted: []domain.PolymarketDiscoveryAccepted{{Candidate: domain.PolymarketDiscoveryCandidate{Slug: "aaa"}, Proposal: json.RawMessage(`{"conviction":0.9,"name":"aaa"}`)}}}}
	var stored *polymarketdiscovery.Result
	polymarketDiscoveryDeployStrategy = func(_ context.Context, _ polymarketdiscovery.Config, _ polymarketdiscovery.Deps, mc polymarketdiscovery.MarketContext, prop polymarketdiscovery.Proposal) (polymarketdiscovery.DeployedStrategy, error) {
		return polymarketdiscovery.DeployedStrategy{StrategyID: uuid.New(), Slug: mc.Market.Slug, Name: prop.Name, Direction: prop.Direction, Conviction: prop.Conviction}, nil
	}
	polymarketDiscoveryStoreLastResult = func(r *polymarketdiscovery.Result) { stored = r }
	c := polymarketDiscoveryChunker{progress: repo, deps: OrchestratorDeps{LLMProvider: llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil })}, logger: slog.Default()}
	if err := c.runDeploy(context.Background(), repo.run); err != nil {
		t.Fatal(err)
	}
	if repo.run.Status != domain.PolymarketDiscoveryStatusCompleted || repo.run.Phase != domain.PolymarketDiscoveryPhaseDone || repo.run.CompletedAt == nil {
		t.Fatalf("run not completed: %+v", repo.run)
	}
	if stored == nil || len(stored.Deployed) != 1 {
		t.Fatalf("stored result = %#v", stored)
	}
}

func TestDiscoveryCandidateToMarketContextPreservesNumericFields(t *testing.T) {
	cand := domain.PolymarketDiscoveryCandidate{Slug: "aaa", Question: "q", Description: "d", Category: "cat", ConditionID: "cid", EndDate: "2026-01-01T00:00:00Z", Volume24Hr: 12.5, Liquidity: 34.5, BestBid: 0.11, BestAsk: 0.22, Spread: 0.11, LastTradePrice: 0.15, ResolutionSource: "source"}
	mc, err := discoveryCandidateToMarketContext(cand)
	if err != nil {
		t.Fatal(err)
	}
	if got := mc.Market.Volume24HrFloat(); got != 12.5 {
		t.Fatalf("Volume24HrFloat=%v", got)
	}
	if got := mc.Market.LiquidityFloat(); got != 34.5 {
		t.Fatalf("LiquidityFloat=%v", got)
	}
	if got, ok := mc.Market.BestBidFloat(); !ok || got != 0.11 {
		t.Fatalf("BestBidFloat=%v %v", got, ok)
	}
	if got, ok := mc.Market.BestAskFloat(); !ok || got != 0.22 {
		t.Fatalf("BestAskFloat=%v %v", got, ok)
	}
	if got, ok := mc.Market.SpreadFloat(); !ok || got != 0.11 {
		t.Fatalf("SpreadFloat=%v %v", got, ok)
	}
	if got, ok := mc.Market.LastPriceFloat(); !ok || got != 0.15 {
		t.Fatalf("LastPriceFloat=%v %v", got, ok)
	}
}

func TestPolymarketDiscoveryChunkerPersistsCandidateErrorsPerAttempt(t *testing.T) {
	defer restoreDiscoverySeams()
	repo := &inMemoryPolymarketDiscoveryRunRepo{run: &domain.PolymarketDiscoveryRun{ID: uuid.New(), Status: domain.PolymarketDiscoveryStatusRunning, Phase: domain.PolymarketDiscoveryPhasePropose, Candidates: []domain.PolymarketDiscoveryCandidate{{Slug: "bad"}}}}
	polymarketDiscoveryBuildContext = func(_ context.Context, _ polymarketdiscovery.GammaMarket, _ repository.PolymarketAccountRepository) (polymarketdiscovery.MarketContext, error) {
		return polymarketdiscovery.MarketContext{}, errors.New("boom")
	}
	c := polymarketDiscoveryChunker{progress: repo, deps: OrchestratorDeps{LLMProvider: llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil })}, proposePerChunk: 1}
	if err := c.runPropose(context.Background(), repo.run); err != nil {
		t.Fatal(err)
	}
	if repo.run.CandidateIndex != 1 || len(repo.run.Errors) != 1 {
		t.Fatalf("run not persisted after error: %+v", repo.run)
	}
}

func TestPolymarketDiscoveryChunkerPersistsProgressAfterProposalTimeout(t *testing.T) {
	defer restoreDiscoverySeams()
	repo := &inMemoryPolymarketDiscoveryRunRepo{
		run: &domain.PolymarketDiscoveryRun{
			ID:         uuid.New(),
			Status:     domain.PolymarketDiscoveryStatusRunning,
			Phase:      domain.PolymarketDiscoveryPhasePropose,
			Candidates: []domain.PolymarketDiscoveryCandidate{{Slug: "slow", Question: "q"}},
		},
		failOnCancelledUpdateCtx: true,
	}
	polymarketDiscoveryGenerateProposal = func(ctx context.Context, _ polymarketdiscovery.GeneratorConfig, _ polymarketdiscovery.MarketContext, _ *slog.Logger) (*polymarketdiscovery.Proposal, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	c := polymarketDiscoveryChunker{
		progress:        repo,
		deps:            OrchestratorDeps{LLMProvider: llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil })},
		proposePerChunk: 1,
		proposalTimeout: 5 * time.Millisecond,
		progressTimeout: time.Second,
	}
	if err := c.runPropose(context.Background(), repo.run); err != nil {
		t.Fatal(err)
	}
	if repo.run.CandidateIndex != 1 {
		t.Fatalf("candidate index = %d, want 1", repo.run.CandidateIndex)
	}
	if len(repo.run.Errors) != 1 || !contains(repo.run.Errors, "context deadline exceeded") {
		t.Fatalf("errors = %#v", repo.run.Errors)
	}
	if repo.run.Phase != domain.PolymarketDiscoveryPhaseDone || repo.run.Status != domain.PolymarketDiscoveryStatusCompleted {
		t.Fatalf("run not completed: %+v", repo.run)
	}
}

func TestPolymarketDiscoveryChunkerDeploySkipsAlreadyDeployed(t *testing.T) {
	defer restoreDiscoverySeams()
	repo := &inMemoryPolymarketDiscoveryRunRepo{run: &domain.PolymarketDiscoveryRun{ID: uuid.New(), Status: domain.PolymarketDiscoveryStatusRunning, Phase: domain.PolymarketDiscoveryPhaseDeploy, StartedAt: time.Now().Add(-time.Minute), Accepted: []domain.PolymarketDiscoveryAccepted{{Candidate: domain.PolymarketDiscoveryCandidate{Slug: "aaa"}, Proposal: json.RawMessage(`{"conviction":0.9,"name":"aaa"}`)}}, Deployed: []domain.PolymarketDiscoveryDeployed{{StrategyID: uuid.NewString(), Slug: "aaa"}}, Summary: domain.PolymarketDiscoverySummary{Deployed: 1}}}
	called := 0
	polymarketDiscoveryDeployStrategy = func(_ context.Context, _ polymarketdiscovery.Config, _ polymarketdiscovery.Deps, _ polymarketdiscovery.MarketContext, _ polymarketdiscovery.Proposal) (polymarketdiscovery.DeployedStrategy, error) {
		called++
		return polymarketdiscovery.DeployedStrategy{}, nil
	}
	c := polymarketDiscoveryChunker{progress: repo, deps: OrchestratorDeps{LLMProvider: llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil })}, logger: slog.Default()}
	if err := c.runDeploy(context.Background(), repo.run); err != nil {
		t.Fatal(err)
	}
	if called != 0 || repo.run.Summary.Deployed != 1 || len(repo.run.Deployed) != 1 {
		t.Fatalf("unexpected deploy retry behavior: called=%d run=%+v", called, repo.run)
	}
}

func TestPolymarketDiscoveryChunkerMaxAgeFailsRunAndStartsReplacement(t *testing.T) {
	defer restoreDiscoverySeams()
	started := time.Now().Add(-(polymarketDiscoveryMaxRunAge + time.Minute))
	staleID := uuid.New()
	repo := &inMemoryPolymarketDiscoveryRunRepo{run: &domain.PolymarketDiscoveryRun{ID: staleID, Status: domain.PolymarketDiscoveryStatusRunning, Phase: domain.PolymarketDiscoveryPhasePropose, StartedAt: started}}
	polymarketDiscoveryFetchOpenMarkets = func(_ context.Context, _ string, _ int) ([]polymarketdiscovery.GammaMarket, error) {
		return []polymarketdiscovery.GammaMarket{{Slug: "fresh", Question: "Will fresh happen?", Active: true, AcceptingOrders: true}}, nil
	}
	polymarketDiscoveryScreenMarkets = func(markets []polymarketdiscovery.GammaMarket, _ polymarketdiscovery.ScreenerConfig) []polymarketdiscovery.GammaMarket {
		return markets
	}
	c := polymarketDiscoveryChunker{progress: repo, deps: OrchestratorDeps{LLMProvider: llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil })}, logger: slog.Default()}
	if err := c.RunChunk(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.updated) == 0 || repo.updated[0].ID != staleID || repo.updated[0].Status != domain.PolymarketDiscoveryStatusFailed || repo.updated[0].Phase != domain.PolymarketDiscoveryPhaseDone {
		t.Fatalf("stale run not failed: updated=%+v", repo.updated)
	}
	if repo.run.ID == staleID || repo.run.Status != domain.PolymarketDiscoveryStatusRunning || repo.run.Phase != domain.PolymarketDiscoveryPhasePropose {
		t.Fatalf("replacement run not started: %+v", repo.run)
	}
}

func TestDiscoveryCandidateToMarketContextRehydratesRawMarket(t *testing.T) {
	raw := []byte(`{"slug":"aaa","question":"q","bestBid":"0.11"}`)
	mc, err := discoveryCandidateToMarketContext(domain.PolymarketDiscoveryCandidate{RawMarket: raw})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := mc.Market.BestBidFloat(); !ok || got != 0.11 {
		t.Fatalf("BestBidFloat=%v %v", got, ok)
	}
	if _, ok := mc.Market.BestAskFloat(); ok {
		t.Fatal("expected absent BestAskFloat")
	}
	if _, ok := mc.Market.SpreadFloat(); ok {
		t.Fatal("expected absent SpreadFloat")
	}
	if _, ok := mc.Market.LastPriceFloat(); ok {
		t.Fatal("expected absent LastPriceFloat")
	}
}

func restoreDiscoverySeams() {
	polymarketDiscoveryFetchOpenMarkets = polymarketdiscovery.FetchOpenMarkets
	polymarketDiscoveryScreenMarkets = polymarketdiscovery.ScreenMarkets
	polymarketDiscoveryBuildContext = polymarketdiscovery.BuildMarketContext
	polymarketDiscoveryGenerateProposal = polymarketdiscovery.GenerateProposal
	polymarketDiscoveryDeployStrategy = polymarketdiscovery.DeployStrategy
	polymarketDiscoveryStoreLastResult = polymarketdiscovery.StoreLastResult
}

func makeCandidates(n int) []domain.PolymarketDiscoveryCandidate {
	out := make([]domain.PolymarketDiscoveryCandidate, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, domain.PolymarketDiscoveryCandidate{Slug: fmt.Sprintf("cand-%d", i), Question: "q"})
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want || strings.Contains(s, want) {
			return true
		}
	}
	return false
}
