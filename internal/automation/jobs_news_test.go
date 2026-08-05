package automation

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestJobOrchestratorSocialScan_SkipsStockTwitsForPolymarketStrategies(t *testing.T) {
	origTransport := http.DefaultTransport
	t.Cleanup(func() {
		http.DefaultTransport = origTransport
	})

	var (
		mu    sync.Mutex
		calls = map[string]int{}
	)
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		calls[req.URL.Path]++
		mu.Unlock()

		switch req.URL.Path {
		case "/api/2/trending/symbols.json":
			return jsonResponse(`{"symbols":[]}`), nil
		case "/api/2/streams/symbol/AAPL.json":
			return jsonResponse(`{"messages":[]}`), nil
		case "/api/2/streams/symbol/SLUG-1.json":
			return jsonResponse(`{"messages":[]}`), nil
		default:
			t.Fatalf("unexpected request: %s", req.URL.String())
			return nil, nil
		}
	})

	orch := NewJobOrchestrator(OrchestratorDeps{
		NewsFeedRepo: &pgrepo.NewsFeedRepo{},
		StrategyRepo: &stubStrategyRepoForReports{
			strategies: []domain.Strategy{
				{ID: uuid.New(), Name: "stock", Status: domain.StrategyStatusActive, Ticker: "AAPL", MarketType: domain.MarketTypeStock},
				{ID: uuid.New(), Name: "polymarket", Status: domain.StrategyStatusActive, Ticker: "slug-1", MarketType: domain.MarketTypePolymarket},
			},
		},
	})

	if err := orch.socialScan(context.Background()); err != nil {
		t.Fatalf("socialScan() error = %v", err)
	}

	mu.Lock()
	gotStock := calls["/api/2/streams/symbol/AAPL.json"]
	gotPolymarket := calls["/api/2/streams/symbol/SLUG-1.json"]
	mu.Unlock()

	if gotStock != 1 {
		t.Fatalf("stock sentiment requests = %d, want 1", gotStock)
	}
	if gotPolymarket != 0 {
		t.Fatalf("polymarket sentiment requests = %d, want 0", gotPolymarket)
	}
}

func TestJobOrchestratorSocialScan_SkipsPredictionMarketPositions(t *testing.T) {
	origTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = origTransport })

	var (
		mu    sync.Mutex
		calls = map[string]int{}
	)
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		calls[req.URL.Path]++
		mu.Unlock()
		switch req.URL.Path {
		case "/api/2/trending/symbols.json":
			return jsonResponse(`{"symbols":[]}`), nil
		case "/api/2/streams/symbol/AAPL.json":
			return jsonResponse(`{"messages":[]}`), nil
		case "/api/2/streams/symbol/KXTEST:YES.json":
			return jsonResponse(`{"messages":[]}`), nil
		default:
			t.Fatalf("unexpected request: %s", req.URL.String())
			return nil, nil
		}
	})

	orch := NewJobOrchestrator(OrchestratorDeps{
		NewsFeedRepo: &pgrepo.NewsFeedRepo{},
		PositionRepo: newRecordingPositionRepo(
			&domain.Position{ID: uuid.New(), Ticker: "AAPL", MarketType: domain.MarketTypeStock},
			&domain.Position{ID: uuid.New(), Ticker: "KXTEST:YES", MarketType: domain.MarketTypeKalshi},
		),
	})
	if err := orch.socialScan(context.Background()); err != nil {
		t.Fatalf("socialScan() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if calls["/api/2/streams/symbol/AAPL.json"] != 1 {
		t.Fatalf("stock position calls = %d, want 1", calls["/api/2/streams/symbol/AAPL.json"])
	}
	if calls["/api/2/streams/symbol/KXTEST:YES.json"] != 0 {
		t.Fatalf("Kalshi position calls = %d, want 0", calls["/api/2/streams/symbol/KXTEST:YES.json"])
	}
}

func TestListAllStrategiesPaginatesPastFirstPage(t *testing.T) {
	t.Parallel()

	strategies := make([]domain.Strategy, 141)
	for i := range strategies {
		strategies[i] = domain.Strategy{ID: uuid.New(), Status: domain.StrategyStatusActive}
	}
	repo := &pagedStrategyRepo{stubReportStrategyRepo: stubReportStrategyRepo{strategies: strategies}}

	got, err := listAllStrategies(context.Background(), repo, repository.StrategyFilter{Status: domain.StrategyStatusActive})
	if err != nil {
		t.Fatalf("listAllStrategies() error = %v", err)
	}
	if len(got) != 141 {
		t.Fatalf("len(strategies) = %d, want 141", len(got))
	}
	if repo.listCalls != 2 {
		t.Fatalf("List() calls = %d, want 2", repo.listCalls)
	}
}

type pagedStrategyRepo struct {
	stubReportStrategyRepo
	listCalls int
}

func (r *pagedStrategyRepo) List(_ context.Context, _ repository.StrategyFilter, limit, offset int) ([]domain.Strategy, error) {
	r.listCalls++
	if offset >= len(r.strategies) {
		return nil, nil
	}
	end := min(offset+limit, len(r.strategies))
	return append([]domain.Strategy(nil), r.strategies[offset:end]...), nil
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
