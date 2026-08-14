package copytrading

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data/edgar"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/google/uuid"
)

type syncRepo struct {
	repository.CopyTradingRepository
	subscriptions []domain.CopySubscription
	source        domain.CopyLeaderSource
	saves         int
	observed      int
}

func (r *syncRepo) ListSubscriptions(context.Context, repository.CopySubscriptionFilter, int, int) ([]domain.CopySubscription, error) {
	return r.subscriptions, nil
}

func (r *syncRepo) GetSource(context.Context, uuid.UUID) (*domain.CopyLeaderSource, error) {
	sourceCopy := r.source
	return &sourceCopy, nil
}

func (r *syncRepo) Save13FSnapshot(_ context.Context, observation *domain.CopySourceObservation, snapshot *domain.CopyPortfolioSnapshot) (bool, error) {
	r.saves++
	observation.ID = uuid.New()
	snapshot.ID = uuid.New()
	snapshot.ObservationID = observation.ID
	return true, nil
}

func (r *syncRepo) UpdateSourceObserved(context.Context, uuid.UUID, time.Time, json.RawMessage) error {
	r.observed++
	return nil
}

func (r *syncRepo) UpdateLeaderIdentityStatus(context.Context, uuid.UUID, domain.CopyIdentityStatus) error {
	return nil
}

type fixed13FFetcher struct{ calls int }

func (f *fixed13FFetcher) FetchLatest13F(context.Context, string) (*edgar.ThirteenFFiling, error) {
	f.calls++
	return &edgar.ThirteenFFiling{
		CIK: "1067983", Accession: "0000000000-26-000001", Form: "13F-HR",
		ReportPeriod: time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		FiledAt:      time.Date(2026, 8, 14, 20, 0, 0, 0, time.UTC),
		ContentHash:  "hash",
		Holdings:     []domain.CopyPortfolioHolding{{IssuerName: "Example", CUSIP: "123456789", DisclosedValue: 1000, SharesOrPrincipal: 10}},
	}, nil
}

func TestSync13FSubscriptionsRefreshesSharedPausedSourceOnce(t *testing.T) {
	t.Parallel()
	sourceID := uuid.New()
	repo := &syncRepo{
		source: domain.CopyLeaderSource{ID: sourceID, SourceType: domain.CopySourceSEC13F, ExternalKey: "1067983"},
		subscriptions: []domain.CopySubscription{
			{ID: uuid.New(), SourceID: sourceID, Status: domain.CopySubscriptionPaused, IsPaper: true},
			{ID: uuid.New(), SourceID: sourceID, Status: domain.CopySubscriptionPaused, IsPaper: true},
		},
	}
	fetcher := &fixed13FFetcher{}
	service := NewService(ServiceDeps{Repo: repo, EDGAR: fetcher, Now: func() time.Time { return time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC) }})

	summary, err := service.Sync13FSubscriptions(context.Background())
	if err != nil {
		t.Fatalf("Sync13FSubscriptions() error = %v", err)
	}
	if summary.Subscriptions != 2 || summary.SourcesChecked != 1 || summary.NewFilings != 1 || summary.Rebalanced != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	if fetcher.calls != 1 || repo.saves != 1 || repo.observed != 1 {
		t.Fatalf("calls fetch=%d save=%d observed=%d", fetcher.calls, repo.saves, repo.observed)
	}
}
