package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	prediction "github.com/PatrickFanella/get-rich-quick/internal/execution/prediction"
	"github.com/PatrickFanella/get-rich-quick/internal/kalshidiscovery"
	"github.com/google/uuid"
)

type kalshiSettlementCatalogStub struct {
	getMarkets map[string]*kalshidiscovery.MarketCandidate
	fetched    []string
	listCalls  int
}

func (s *kalshiSettlementCatalogStub) ListMarkets(context.Context, kalshidiscovery.ListOptions) ([]kalshidiscovery.MarketCandidate, string, error) {
	s.listCalls++
	return nil, "", errors.New("unexpected list call")
}

func (s *kalshiSettlementCatalogStub) GetMarket(_ context.Context, ticker string) (*kalshidiscovery.MarketCandidate, error) {
	s.fetched = append(s.fetched, ticker)
	if s.getMarkets == nil {
		return nil, nil
	}
	return s.getMarkets[strings.ToUpper(strings.TrimSpace(ticker))], nil
}

type kalshiGateRepoStub struct {
	state       *domain.KalshiSettlementGateState
	gets        int
	failPersist bool
}

type kalshiPendingSettlerStub struct {
	pending    []string
	preview    map[string]int
	previewIDs map[string][]uuid.UUID
	previewErr map[string]error
	settle     []string
}

func (s *kalshiPendingSettlerStub) PendingMarkets(context.Context, domain.MarketType) ([]string, error) {
	return append([]string(nil), s.pending...), nil
}

func (s *kalshiPendingSettlerStub) PreviewMarket(_ context.Context, _ domain.MarketType, ticker string) (int, error) {
	if s.previewErr != nil {
		if err := s.previewErr[strings.ToUpper(strings.TrimSpace(ticker))]; err != nil {
			return 0, err
		}
	}
	if s.preview == nil {
		return 1, nil
	}
	return s.preview[strings.ToUpper(strings.TrimSpace(ticker))], nil
}

func (s *kalshiPendingSettlerStub) SettlePreview(_ context.Context, _ domain.MarketType, ticker string) (*prediction.SettlementPreview, error) {
	count, err := s.PreviewMarket(context.Background(), domain.MarketTypeKalshi, ticker)
	if err != nil {
		return nil, err
	}
	ids := append([]uuid.UUID(nil), s.previewIDs[strings.ToUpper(strings.TrimSpace(ticker))]...)
	if len(ids) == 0 {
		ids = make([]uuid.UUID, count)
		for i := range ids {
			ids[i] = uuid.New()
		}
	}
	return &prediction.SettlementPreview{Instrument: strings.ToUpper(strings.TrimSpace(ticker)), Count: count, DecisionIDs: ids}, nil
}

func (s *kalshiPendingSettlerStub) SettleDecisions(context.Context, domain.MarketType, string, string, time.Time, []uuid.UUID) (int, error) {
	return 1, nil
}

func (s *kalshiPendingSettlerStub) SettleMarket(_ context.Context, _ domain.MarketType, ticker, winner string, _ time.Time) (int, error) {
	s.settle = append(s.settle, ticker+":"+winner)
	return 1, nil
}

func (s *kalshiGateRepoStub) Get(context.Context, string) (*domain.KalshiSettlementGateState, error) {
	s.gets++
	return s.state, nil
}

func (s *kalshiGateRepoStub) RecordSuccess(_ context.Context, _ string, threshold, _, _, _, _ int, fingerprint string, _ time.Time) (*domain.KalshiSettlementGateState, error) {
	if s.failPersist {
		return nil, errors.New("persist failed")
	}
	if s.state == nil {
		s.state = &domain.KalshiSettlementGateState{}
	}
	s.state.ConsecutiveSuccesses++
	s.state.Threshold = threshold
	s.state.ProjectionFingerprint = fingerprint
	s.state.Eligible = threshold > 0 && s.state.ConsecutiveSuccesses >= threshold
	s.state.LastOutcome = "success"
	return s.state, nil
}

func (s *kalshiGateRepoStub) RecordFailure(context.Context, string, int, int, int, int, int, time.Time, string) (*domain.KalshiSettlementGateState, error) {
	if s.failPersist {
		return nil, errors.New("persist failed")
	}
	if s.state == nil {
		s.state = &domain.KalshiSettlementGateState{}
	}
	s.state.ConsecutiveSuccesses = 0
	s.state.LastOutcome = "failure"
	return s.state, nil
}

func TestKalshiSettlementManualDryRunAccumulatesGateAndSkipsMutation(t *testing.T) {
	cat := &kalshiSettlementCatalogStub{getMarkets: map[string]*kalshidiscovery.MarketCandidate{
		"KX-A": {Ticker: "KX-A", Result: "yes"},
		"KX-B": {Ticker: "KX-B", Result: "no"},
		"KX-Z": {Ticker: "KX-Z", Result: ""},
	}}
	settler := &kalshiPendingSettlerStub{pending: []string{"KX-A", "KX-B", "KX-Z", "KX-A"}, preview: map[string]int{"KX-A": 1, "KX-B": 0, "KX-Z": 0}}
	gate := &kalshiGateRepoStub{state: &domain.KalshiSettlementGateState{Threshold: 2}}
	metrics := &stubAutomationMetrics{}
	orch := NewJobOrchestrator(OrchestratorDeps{KalshiCatalog: cat, PredictionSettler: settler, KalshiSettlementGateRepo: gate})
	orch.WithJobMetrics(metrics)
	orch.Register("kalshi_settlement", "test", schedulerSpecEveryMinute(), orch.kalshiSettlement)
	job := orch.jobs["kalshi_settlement"]
	job.Enabled = false
	if err := orch.kalshiSettlement(context.Background()); err != nil {
		t.Fatalf("kalshiSettlement() error = %v", err)
	}
	if len(settler.settle) != 0 {
		t.Fatalf("SettleMarket calls = %v, want none", settler.settle)
	}
	if cat.listCalls != 0 {
		t.Fatalf("ListMarkets calls = %d, want 0", cat.listCalls)
	}
	if got := strings.Join(cat.fetched, ","); got != "KX-A,KX-B,KX-Z,KX-A" {
		t.Fatalf("fetched tickers = %q", got)
	}
	if metrics.kalshiDryRuns["success"] != 1 || metrics.kalshiOutcomes["success"] != 1 {
		t.Fatalf("metrics = %#v", metrics)
	}
}

func TestKalshiSettlementFailsWhenPendingMarketIsMissingFromCatalog(t *testing.T) {
	settler := &kalshiPendingSettlerStub{pending: []string{"KX-MISSING"}}
	gate := &kalshiGateRepoStub{}
	orch := NewJobOrchestrator(OrchestratorDeps{
		KalshiCatalog:            &kalshiSettlementCatalogStub{},
		PredictionSettler:        settler,
		KalshiSettlementGateRepo: gate,
	})
	orch.Register("kalshi_settlement", "test", schedulerSpecEveryMinute(), orch.kalshiSettlement)

	err := orch.kalshiSettlement(context.Background())
	if err == nil || !strings.Contains(err.Error(), "catalog returned no market") {
		t.Fatalf("kalshiSettlement() error = %v, want missing catalog market", err)
	}
	if gate.state == nil || gate.state.LastOutcome != "failure" {
		t.Fatalf("gate state = %#v, want recorded failure", gate.state)
	}
}

func TestKalshiSettlementLivePathBuildsGateEvidenceBeforeMutation(t *testing.T) {
	cat := &kalshiSettlementCatalogStub{getMarkets: map[string]*kalshidiscovery.MarketCandidate{"KX-A": {Ticker: "KX-A", Result: "yes"}}}
	settler := &kalshiPendingSettlerStub{pending: []string{"KX-A"}, preview: map[string]int{"KX-A": 1}}
	gate := &kalshiGateRepoStub{state: &domain.KalshiSettlementGateState{Threshold: 1, Eligible: false}}
	orch := NewJobOrchestrator(OrchestratorDeps{KalshiCatalog: cat, PredictionSettler: settler, KalshiSettlementGateRepo: gate, KalshiSettlementEnabled: true})
	orch.Register("kalshi_settlement", "test", schedulerSpecEveryMinute(), orch.kalshiSettlement)
	job := orch.jobs["kalshi_settlement"]
	job.Enabled = true
	if err := orch.kalshiSettlement(context.Background()); err != nil {
		t.Fatalf("kalshiSettlement() error = %v", err)
	}
	if len(settler.settle) != 0 {
		t.Fatalf("SettleMarket calls = %v, want none", settler.settle)
	}
	if gate.gets == 0 || gate.state.ConsecutiveSuccesses != 1 {
		t.Fatal("gate eligibility was not checked")
	}
	if len(cat.fetched) != 1 || cat.fetched[0] != "KX-A" {
		t.Fatalf("fetched tickers = %#v, want [KX-A]", cat.fetched)
	}
}

func TestKalshiSettlementFailureResetsGateAndEmitsFailureMetrics(t *testing.T) {
	cat := &kalshiSettlementCatalogStub{getMarkets: map[string]*kalshidiscovery.MarketCandidate{"KX-A": {Ticker: "KX-A", Result: "yes"}}}
	settler := &kalshiPendingSettlerStub{pending: []string{"KX-A"}, previewErr: map[string]error{"KX-A": errors.New("preview boom")}}
	gate := &kalshiGateRepoStub{state: &domain.KalshiSettlementGateState{ConsecutiveSuccesses: 3, Threshold: 4}}
	metrics := &stubAutomationMetrics{}
	orch := NewJobOrchestrator(OrchestratorDeps{KalshiCatalog: cat, PredictionSettler: settler, KalshiSettlementGateRepo: gate})
	orch.WithJobMetrics(metrics)
	orch.Register("kalshi_settlement", "test", schedulerSpecEveryMinute(), orch.kalshiSettlement)
	job := orch.jobs["kalshi_settlement"]
	job.Enabled = false
	if err := orch.kalshiSettlement(context.Background()); err == nil {
		t.Fatal("kalshiSettlement() error = nil, want preview failure")
	}
	if gate.state == nil || gate.state.ConsecutiveSuccesses != 0 || gate.state.LastOutcome != "failure" {
		t.Fatalf("gate state = %#v, want failure reset", gate.state)
	}
	if metrics.kalshiDryRuns["failure"] != 1 || metrics.kalshiOutcomes["failure"] != 1 {
		t.Fatalf("failure metrics = %#v", metrics)
	}
}

func TestKalshiSettlementLiveSuccessDoesNotRecordGateSuccess(t *testing.T) {
	cat := &kalshiSettlementCatalogStub{getMarkets: map[string]*kalshidiscovery.MarketCandidate{"KX-A": {Ticker: "KX-A", Result: "yes"}}}
	decisionID := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	settler := &kalshiPendingSettlerStub{pending: []string{"KX-A"}, preview: map[string]int{"KX-A": 1}, previewIDs: map[string][]uuid.UUID{"KX-A": {decisionID}}, settle: []string{}}
	fingerprint := settlementProjectionFingerprint([]string{settlementPreviewFingerprint(&prediction.SettlementPreview{Instrument: "KX-A", Count: 1, DecisionIDs: []uuid.UUID{decisionID}}, "YES")})
	gate := &kalshiGateRepoStub{state: &domain.KalshiSettlementGateState{Threshold: 1, Eligible: true, ProjectionFingerprint: fingerprint}}
	orch := NewJobOrchestrator(OrchestratorDeps{KalshiCatalog: cat, PredictionSettler: settler, KalshiSettlementGateRepo: gate, KalshiSettlementDryRun: false, KalshiSettlementEnabled: true})
	orch.Register("kalshi_settlement", "test", schedulerSpecEveryMinute(), orch.kalshiSettlement)
	job := orch.jobs["kalshi_settlement"]
	job.Enabled = true
	if err := orch.kalshiSettlement(context.Background()); err != nil {
		t.Fatalf("kalshiSettlement() error = %v", err)
	}
	if gate.state == nil || gate.state.ConsecutiveSuccesses != 0 {
		t.Fatalf("gate state after live success = %#v, want unchanged", gate.state)
	}
}

func TestKalshiSettlementLiveFingerprintMismatchSkipsAllMutations(t *testing.T) {
	cat := &kalshiSettlementCatalogStub{getMarkets: map[string]*kalshidiscovery.MarketCandidate{"KX-A": {Ticker: "KX-A", Result: "yes"}}}
	settler := &kalshiPendingSettlerStub{pending: []string{"KX-A"}, preview: map[string]int{"KX-A": 1}}
	gate := &kalshiGateRepoStub{state: &domain.KalshiSettlementGateState{Threshold: 1, Eligible: true, ProjectionFingerprint: "old-fp"}}
	metrics := &stubAutomationMetrics{}
	orch := NewJobOrchestrator(OrchestratorDeps{KalshiCatalog: cat, PredictionSettler: settler, KalshiSettlementGateRepo: gate, KalshiSettlementEnabled: true})
	orch.WithJobMetrics(metrics)
	orch.Register("kalshi_settlement", "test", schedulerSpecEveryMinute(), orch.kalshiSettlement)
	job := orch.jobs["kalshi_settlement"]
	job.Enabled = true
	if err := orch.kalshiSettlement(context.Background()); err != nil {
		t.Fatalf("kalshiSettlement() error = %v", err)
	}
	if len(settler.settle) != 0 {
		t.Fatalf("SettleMarket calls = %v, want none", settler.settle)
	}
	if gate.state == nil || gate.state.ConsecutiveSuccesses != 1 || gate.state.LastOutcome != "success" {
		t.Fatalf("gate state = %#v, want new preview sequence", gate.state)
	}
	if metrics.kalshiDryRuns["success"] != 1 || metrics.kalshiOutcomes["success"] != 1 {
		t.Fatalf("metrics = %#v", metrics)
	}
}

func TestKalshiSettlementDryRunFailsClosedWithoutGateRepo(t *testing.T) {
	cat := &kalshiSettlementCatalogStub{getMarkets: map[string]*kalshidiscovery.MarketCandidate{"KX-A": {Ticker: "KX-A", Result: "yes"}}}
	settler := &kalshiPendingSettlerStub{pending: []string{"KX-A"}, preview: map[string]int{"KX-A": 1}}
	orch := NewJobOrchestrator(OrchestratorDeps{KalshiCatalog: cat, PredictionSettler: settler})
	orch.Register("kalshi_settlement", "test", schedulerSpecEveryMinute(), orch.kalshiSettlement)
	job := orch.jobs["kalshi_settlement"]
	job.Enabled = false
	if err := orch.kalshiSettlement(context.Background()); err == nil {
		t.Fatal("kalshiSettlement() error = nil, want gate unavailable")
	}
	if !orch.kalshiGateUnhealthy {
		t.Fatal("kalshiGateUnhealthy = false, want true")
	}
}

func TestKalshiSettlementDryRunFailsClosedOnGatePersistError(t *testing.T) {
	cat := &kalshiSettlementCatalogStub{getMarkets: map[string]*kalshidiscovery.MarketCandidate{"KX-A": {Ticker: "KX-A", Result: "yes"}}}
	settler := &kalshiPendingSettlerStub{pending: []string{"KX-A"}, preview: map[string]int{"KX-A": 1}}
	gate := &kalshiGateRepoStub{state: &domain.KalshiSettlementGateState{Threshold: 1}, failPersist: true}
	orch := NewJobOrchestrator(OrchestratorDeps{KalshiCatalog: cat, PredictionSettler: settler, KalshiSettlementGateRepo: gate})
	orch.Register("kalshi_settlement", "test", schedulerSpecEveryMinute(), orch.kalshiSettlement)
	job := orch.jobs["kalshi_settlement"]
	job.Enabled = false
	if err := orch.kalshiSettlement(context.Background()); err == nil {
		t.Fatal("kalshiSettlement() error = nil, want persist failure")
	}
}
