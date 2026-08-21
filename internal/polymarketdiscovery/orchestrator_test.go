package polymarketdiscovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// --- fakes ----------------------------------------------------------------

type fakeLLM struct {
	resp string
	err  error
}

func (f *fakeLLM) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &llm.CompletionResponse{Content: f.resp}, nil
}

type fakeStrategyRepo struct {
	created []domain.Strategy
	theses  map[uuid.UUID]json.RawMessage
}

func (f *fakeStrategyRepo) Create(_ context.Context, s *domain.Strategy) error {
	f.created = append(f.created, *s)
	return nil
}

func (f *fakeStrategyRepo) Get(_ context.Context, _ uuid.UUID) (*domain.Strategy, error) {
	return nil, repository.ErrNotFound
}

func (f *fakeStrategyRepo) List(_ context.Context, _ repository.StrategyFilter, _, _ int) ([]domain.Strategy, error) {
	return f.created, nil
}

func (f *fakeStrategyRepo) Count(_ context.Context, _ repository.StrategyFilter) (int, error) {
	return len(f.created), nil
}
func (f *fakeStrategyRepo) Update(_ context.Context, _ *domain.Strategy) error { return nil }
func (f *fakeStrategyRepo) Delete(_ context.Context, _ uuid.UUID) error        { return nil }
func (f *fakeStrategyRepo) UpdateThesis(_ context.Context, id uuid.UUID, t json.RawMessage) error {
	if f.theses == nil {
		f.theses = map[uuid.UUID]json.RawMessage{}
	}
	f.theses[id] = t
	return nil
}

func (f *fakeStrategyRepo) GetThesisRaw(_ context.Context, id uuid.UUID) (json.RawMessage, error) {
	return f.theses[id], nil
}

// --- tests ----------------------------------------------------------------

func TestValidateProposal(t *testing.T) {
	good := &Proposal{
		Template:         TemplateConvergence,
		Name:             "Convergence on X",
		Summary:          "X is 0.9 with no remaining catalyst.",
		Direction:        "YES",
		Conviction:       0.7,
		TimeHorizon:      "days",
		EntryPriceMax:    0.95,
		WatchTerms:       []string{"X resolution"},
		SourceReferences: []string{"official filing"},
		MaxSpreadPct:     5,
		MinLiquidity:     1000,
		StopPolicy:       "exit if official filing reverses",
		TargetPolicy:     "take profit at 0.85",
	}
	if err := validateProposal(good); err != nil {
		t.Fatalf("good proposal rejected: %v", err)
	}

	bad := &Proposal{
		Template: "bogus", Name: "n", Summary: "s", Direction: "YES",
		Conviction: 0.5, TimeHorizon: "days", WatchTerms: []string{"a"},
		SourceReferences: []string{"source"}, MaxSpreadPct: 1, MinLiquidity: 1, StopPolicy: "stop", TargetPolicy: "target", EntryPriceMax: 0.5,
	}
	if err := validateProposal(bad); err == nil {
		t.Fatal("expected error for invalid template")
	}

	skip := &Proposal{Skip: true}
	if err := validateProposal(skip); err == nil {
		t.Fatal("expected error for skip without reason")
	}
	skip.SkipReason = "thin liquidity"
	if err := validateProposal(skip); err != nil {
		t.Fatalf("valid skip rejected: %v", err)
	}
}

func TestValidateProposalRequiresExecutionMetadata(t *testing.T) {
	base := &Proposal{
		Template:         TemplateConvergence,
		Name:             "Convergence on X",
		Summary:          "X is 0.9 with no remaining catalyst.",
		Direction:        "YES",
		Conviction:       0.7,
		TimeHorizon:      "days",
		EntryPriceMax:    0.95,
		WatchTerms:       []string{"X resolution"},
		SourceReferences: []string{"official filing"},
		MaxSpreadPct:     5,
		MinLiquidity:     1000,
		StopPolicy:       "exit if official filing reverses",
		TargetPolicy:     "take profit at 0.85",
	}
	cases := []struct {
		name string
		mut  func(*Proposal)
	}{
		{name: "missing direction", mut: func(p *Proposal) { p.Direction = "" }},
		{name: "bad entry price", mut: func(p *Proposal) { p.EntryPriceMax = 0 }},
		{name: "empty watch terms", mut: func(p *Proposal) { p.WatchTerms = nil }},
		{name: "missing sources", mut: func(p *Proposal) { p.SourceReferences = nil }},
		{name: "bad spread", mut: func(p *Proposal) { p.MaxSpreadPct = 0 }},
		{name: "bad liquidity", mut: func(p *Proposal) { p.MinLiquidity = 0 }},
		{name: "missing stop", mut: func(p *Proposal) { p.StopPolicy = "   " }},
		{name: "missing target", mut: func(p *Proposal) { p.TargetPolicy = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proposal := *base
			tc.mut(&proposal)
			if err := validateProposal(&proposal); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

func TestValidateProposalMatchesMarketRejectsOffTopicProposal(t *testing.T) {
	proposal := &Proposal{
		Template:      TemplateNewsCatalyst,
		Name:          "Trump legal case resolution edge trade",
		Summary:       "Court filings could move this legal market toward acquittal.",
		Direction:     "YES",
		Conviction:    0.75,
		TimeHorizon:   "days",
		EntryPriceMax: 0.62,
		WatchTerms:    []string{"Donald Trump", "legal ruling"},
	}
	mc := MarketContext{Market: GammaMarket{
		Slug:     "will-the-new-york-knicks-win-the-2026-nba-finals",
		Question: "Will the New York Knicks win the 2026 NBA Finals?",
	}}
	if err := validateProposalMatchesMarket(proposal, mc); err == nil {
		t.Fatal("expected off-topic proposal to be rejected")
	}
}

func TestValidateProposalMatchesMarketAcceptsReferencedSubject(t *testing.T) {
	proposal := &Proposal{
		Template:      TemplateNewsCatalyst,
		Name:          "Knicks injury catalyst trade",
		Summary:       "New York Knicks roster news could move NBA Finals pricing.",
		Direction:     "YES",
		Conviction:    0.75,
		TimeHorizon:   "days",
		EntryPriceMax: 0.62,
		WatchTerms:    []string{"Knicks", "Jalen Brunson"},
	}
	mc := MarketContext{Market: GammaMarket{
		Slug:     "will-the-new-york-knicks-win-the-2026-nba-finals",
		Question: "Will the New York Knicks win the 2026 NBA Finals?",
	}}
	if err := validateProposalMatchesMarket(proposal, mc); err != nil {
		t.Fatalf("expected matching proposal to pass: %v", err)
	}
}

func TestValidateProposalRejectsStockLanguage(t *testing.T) {
	base := &Proposal{
		Template:      TemplateNewsCatalyst,
		Name:          "Knicks catalyst trade",
		Summary:       "New York Knicks news could reprice this market quickly.",
		Direction:     "YES",
		Conviction:    0.75,
		TimeHorizon:   "days",
		EntryPriceMax: 0.62,
		WatchTerms:    []string{"Knicks", "injury report"},
	}

	cases := []struct {
		name string
		mut  func(*Proposal)
	}{
		{name: "rsi", mut: func(p *Proposal) { p.Summary = "RSI breakout looks strong" }},
		{name: "ohlcv candles", mut: func(p *Proposal) { p.WatchTerms = []string{"ohlcv candles"} }},
		{name: "ema and mean reversion", mut: func(p *Proposal) { p.InvalidateIf = []string{"EMA crossover fails", "mean reversion does not hold"} }},
		{name: "vwap and z-score", mut: func(p *Proposal) { p.Name = "VWAP z-score setup" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proposal := *base
			tc.mut(&proposal)
			if err := validateProposal(&proposal); err == nil {
				t.Fatalf("expected stock-language rejection for %s", tc.name)
			}
		})
	}
}

func TestScreenMarkets_FiltersByLiquidityAndVolume(t *testing.T) {
	now := time.Now()
	in := []GammaMarket{
		{
			Slug: "good", AcceptingOrders: true,
			Outcomes:   gammaString(`["Yes","No"]`),
			Volume24Hr: gammaString("20000"), Liquidity: gammaString("10000"),
			EndDate: now.Add(30 * 24 * time.Hour).Format(time.RFC3339),
		},
		{
			Slug: "thin", AcceptingOrders: true,
			Outcomes:   gammaString(`["Yes","No"]`),
			Volume24Hr: gammaString("100"), Liquidity: gammaString("100"),
			EndDate: now.Add(30 * 24 * time.Hour).Format(time.RFC3339),
		},
		{
			Slug: "closed", Closed: true, AcceptingOrders: false,
			Outcomes:   gammaString(`["Yes","No"]`),
			Volume24Hr: gammaString("999999"), Liquidity: gammaString("999999"),
			EndDate: now.Add(30 * 24 * time.Hour).Format(time.RFC3339),
		},
	}
	out := ScreenMarkets(in, DefaultScreenerConfig())
	if len(out) != 1 || out[0].Slug != "good" {
		t.Fatalf("unexpected screened set: %+v", out)
	}
}

func TestRun_HappyPath(t *testing.T) {
	now := time.Now()
	// Mock Gamma API.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		markets := []GammaMarket{{
			Slug:            "test-market",
			Question:        "Will Example happen?",
			AcceptingOrders: true,
			Outcomes:        gammaString(`["Yes","No"]`),
			Volume24Hr:      gammaString("50000"),
			Liquidity:       gammaString("20000"),
			EndDate:         now.Add(20 * 24 * time.Hour).Format(time.RFC3339),
		}}
		_ = json.NewEncoder(w).Encode(markets)
	}))
	defer srv.Close()

	prop := Proposal{
		Template:         TemplateConvergence,
		Name:             "Convergence on Example",
		Summary:          "Strong Example convergence setup.",
		Direction:        "YES",
		Conviction:       0.75,
		TimeHorizon:      "days",
		EntryPriceMax:    0.95,
		WatchTerms:       []string{"Example resolution", "Example ruling"},
		InvalidateIf:     []string{"Example is overturned"},
		SourceReferences: []string{"official filing", "court docket"},
		MaxSpreadPct:     4.5,
		MinLiquidity:     1500,
		StopPolicy:       "exit if official filing reverses",
		TargetPolicy:     "take profit at 0.85",
	}
	propJSON, _ := json.Marshal(prop)

	repo := &fakeStrategyRepo{}
	runCfg := Config{
		GammaBaseURL:   srv.URL,
		Screener:       DefaultScreenerConfig(),
		MaxDeployments: 1,
		MinConviction:  0.5,
	}
	res, err := Run(context.Background(), runCfg, Deps{
		LLMProvider: &fakeLLM{resp: string(propJSON)},
		Strategies:  repo,
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(res.Deployed) != 1 {
		t.Fatalf("expected 1 deployed, got %d (errors: %v)", len(res.Deployed), res.Errors)
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 created strategy, got %d", len(repo.created))
	}
	created := repo.created[0]
	if created.MarketType != domain.MarketTypePolymarket {
		t.Fatalf("market_type=%q", created.MarketType)
	}
	if created.Ticker != "test-market" {
		t.Fatalf("ticker=%q", created.Ticker)
	}
	if !created.IsPaper {
		t.Fatal("strategy should be paper")
	}
	if created.Status != domain.StrategyStatusInactive {
		t.Fatalf("strategy status = %q, want inactive", created.Status)
	}
	if created.ScheduleCron != "" {
		t.Fatalf("strategy schedule = %q, want unscheduled research idea", created.ScheduleCron)
	}
	var configJSON map[string]any
	if err := json.Unmarshal(created.Config, &configJSON); err != nil {
		t.Fatalf("unmarshal created config: %v", err)
	}
	meta, ok := configJSON["discovery_meta"].(map[string]any)
	if !ok {
		t.Fatalf("discovery_meta missing or wrong type: %#v", configJSON["discovery_meta"])
	}
	if lifecycle, ok := configJSON["research_lifecycle"].(map[string]any); !ok || lifecycle["stage"] != "idea" || lifecycle["auto_activation_blocked"] != true {
		t.Fatalf("research_lifecycle = %#v, want blocked idea", configJSON["research_lifecycle"])
	}
	for _, key := range []string{"source", "market_slug", "condition_id", "template", "direction", "conviction", "time_horizon", "entry_price_max", "source_references", "max_spread_pct", "min_liquidity", "stop_policy", "target_policy", "native_execution_required", "activation_blocked_reason"} {
		if _, ok := meta[key]; !ok {
			t.Fatalf("missing discovery_meta key %q in %v", key, meta)
		}
	}
	if got := meta["source_references"]; got == nil {
		t.Fatal("source_references not persisted")
	}
	if got := meta["stop_policy"]; got == "" || strings.TrimSpace(got.(string)) == "" {
		t.Fatal("stop_policy not persisted")
	}
	if got := repo.theses[created.ID]; len(got) == 0 {
		t.Fatal("thesis was not persisted")
	}
}

func TestDeployStrategyRejectsSkippedProposal(t *testing.T) {
	repo := &fakeStrategyRepo{}
	mc := MarketContext{Market: GammaMarket{
		Slug:        "skip-market",
		Question:    "Will Example happen?",
		ConditionID: "0xskip",
	}}
	proposal := Proposal{Skip: true, SkipReason: "thin liquidity"}

	_, err := DeployStrategy(context.Background(), Config{}, Deps{Strategies: repo}, mc, proposal)
	if err == nil {
		t.Fatal("expected skipped proposal deployment to fail")
	}
	if !strings.Contains(err.Error(), "cannot deploy skipped proposal") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatalf("expected no created strategies, got %d", len(repo.created))
	}
}

func TestRun_SkipBelowConviction(t *testing.T) {
	now := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		markets := []GammaMarket{{
			Slug: "mkt", Question: "Will Widget happen?",
			AcceptingOrders: true,
			Outcomes:        gammaString(`["Yes","No"]`),
			Volume24Hr:      gammaString("50000"), Liquidity: gammaString("20000"),
			EndDate: now.Add(10 * 24 * time.Hour).Format(time.RFC3339),
		}}
		_ = json.NewEncoder(w).Encode(markets)
	}))
	defer srv.Close()

	prop := Proposal{
		Template: TemplateConvergence, Name: "Low Widget conviction", Summary: "Widget edge is weak",
		Direction: "YES", Conviction: 0.2, TimeHorizon: "days",
		EntryPriceMax: 0.5, WatchTerms: []string{"Widget"},
		SourceReferences: []string{"source"}, MaxSpreadPct: 5, MinLiquidity: 1000,
		StopPolicy: "stop", TargetPolicy: "target",
	}
	propJSON, _ := json.Marshal(prop)

	repo := &fakeStrategyRepo{}
	res, err := Run(context.Background(), Config{
		GammaBaseURL: srv.URL, MinConviction: 0.5,
	}, Deps{LLMProvider: &fakeLLM{resp: string(propJSON)}, Strategies: repo})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Deployed) != 0 {
		t.Fatalf("expected 0 deployed, got %d", len(res.Deployed))
	}
	if res.Skipped != 1 {
		t.Fatalf("expected 1 skipped, got %d", res.Skipped)
	}
}

func TestRun_RejectsInvalidMetadataBeforeActivation(t *testing.T) {
	now := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		markets := []GammaMarket{{
			Slug:            "bad-market",
			Question:        "Will Example happen?",
			AcceptingOrders: true,
			Outcomes:        gammaString(`["Yes","No"]`),
			Volume24Hr:      gammaString("50000"),
			Liquidity:       gammaString("20000"),
			EndDate:         now.Add(20 * 24 * time.Hour).Format(time.RFC3339),
		}}
		_ = json.NewEncoder(w).Encode(markets)
	}))
	defer srv.Close()

	prop := Proposal{
		Template:      TemplateConvergence,
		Name:          "Missing metadata",
		Summary:       "Strong Example convergence setup.",
		Direction:     "YES",
		Conviction:    0.75,
		TimeHorizon:   "days",
		EntryPriceMax: 0.95,
		WatchTerms:    []string{"Example resolution"},
	}
	propJSON, _ := json.Marshal(prop)
	repo := &fakeStrategyRepo{}
	res, err := Run(context.Background(), Config{GammaBaseURL: srv.URL, MinConviction: 0.5}, Deps{LLMProvider: &fakeLLM{resp: string(propJSON)}, Strategies: repo})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatalf("expected no created strategy, got %d", len(repo.created))
	}
	if len(res.Deployed) != 0 {
		t.Fatalf("expected no deployed strategy, got %d", len(res.Deployed))
	}
	if len(res.Errors) == 0 {
		t.Fatal("expected proposal validation error")
	}
}

func TestFetchOpenMarkets_DecodesNumericFields(t *testing.T) {
	now := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"slug":"numeric-market","question":"Q","acceptingOrders":true,"outcomes":"[\"Yes\",\"No\"]","volume":12345.67,"volume24hr":50000,"liquidity":20000,"endDate":"` + now.Add(20*24*time.Hour).Format(time.RFC3339) + `"}]`))
	}))
	defer srv.Close()

	markets, err := FetchOpenMarkets(context.Background(), srv.URL, 1)
	if err != nil {
		t.Fatalf("FetchOpenMarkets error: %v", err)
	}
	if len(markets) != 1 {
		t.Fatalf("expected 1 market, got %d", len(markets))
	}
	if got := markets[0].Volume24HrFloat(); got != 50000 {
		t.Fatalf("Volume24HrFloat = %v, want 50000", got)
	}
	if !markets[0].IsBinaryYesNo() {
		t.Fatal("expected binary yes/no outcomes")
	}
}
