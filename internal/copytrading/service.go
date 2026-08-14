package copytrading

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data/edgar"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/google/uuid"
)

type ThirteenFFetcher interface {
	FetchLatest13F(ctx context.Context, cik string) (*edgar.ThirteenFFiling, error)
}

type PriceProvider interface {
	Snapshots(ctx context.Context, tickers []string) (map[string]PriceSnapshot, error)
}

type PaperOrderRequest struct {
	Subscription domain.CopySubscription
	Intent       domain.CopyTradeIntent
	Run          domain.PipelineRun
}

type PaperOrderResult struct {
	OrderID *uuid.UUID
	Status  domain.OrderStatus
}

type PaperOrderExecutor interface {
	ExecuteCopyOrder(ctx context.Context, request PaperOrderRequest) (PaperOrderResult, error)
}

type ServiceDeps struct {
	Repo       repository.CopyTradingRepository
	Strategies repository.StrategyRepository
	Runs       repository.PipelineRunRepository
	Positions  repository.PositionRepository
	EDGAR      ThirteenFFetcher
	Prices     PriceProvider
	Executor   PaperOrderExecutor
	Logger     *slog.Logger
	Now        func() time.Time
}

type Service struct{ deps ServiceDeps }

func NewService(deps ServiceDeps) *Service {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Service{deps: deps}
}

type LeaderDetail struct {
	Leader  domain.CopyLeader         `json:"leader"`
	Sources []domain.CopyLeaderSource `json:"sources"`
}

func (s *Service) CreateLeader(ctx context.Context, leader *domain.CopyLeader) error {
	if s == nil || s.deps.Repo == nil {
		return fmt.Errorf("copy trading repository is unavailable")
	}
	if err := leader.Validate(); err != nil {
		return err
	}
	return s.deps.Repo.CreateLeader(ctx, leader)
}

func (s *Service) GetLeader(ctx context.Context, id uuid.UUID) (*LeaderDetail, error) {
	leader, err := s.deps.Repo.GetLeader(ctx, id)
	if err != nil {
		return nil, err
	}
	sources, err := s.deps.Repo.ListSourcesByLeader(ctx, id)
	if err != nil {
		return nil, err
	}
	return &LeaderDetail{Leader: *leader, Sources: sources}, nil
}

func (s *Service) ListLeaders(ctx context.Context, filter repository.CopyLeaderFilter, limit, offset int) ([]domain.CopyLeader, int, error) {
	items, err := s.deps.Repo.ListLeaders(ctx, filter, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.deps.Repo.CountLeaders(ctx, filter)
	return items, total, err
}

func (s *Service) AddSource(ctx context.Context, source *domain.CopyLeaderSource) error {
	if err := source.Validate(); err != nil {
		return err
	}
	leader, err := s.deps.Repo.GetLeader(ctx, source.LeaderID)
	if err != nil {
		return err
	}
	if source.SourceType == domain.CopySourceSEC13F && leader.EntityType != domain.CopyLeaderInstitution {
		return fmt.Errorf("sec_13f sources require an institutional leader")
	}
	if (source.SourceType == domain.CopySourceSEC13F || source.SourceType == domain.CopySourceSECForm4) && source.Provider != "sec" {
		return fmt.Errorf("SEC sources require provider=sec")
	}
	return s.deps.Repo.CreateSource(ctx, source)
}

type RefreshResult struct {
	Created     bool                         `json:"created"`
	Observation domain.CopySourceObservation `json:"observation"`
	Snapshot    domain.CopyPortfolioSnapshot `json:"snapshot"`
}

func (s *Service) RefreshSource(ctx context.Context, sourceID uuid.UUID) (*RefreshResult, error) {
	source, err := s.deps.Repo.GetSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	if source.SourceType != domain.CopySourceSEC13F {
		return nil, fmt.Errorf("source refresh supports sec_13f only")
	}
	if s.deps.EDGAR == nil {
		return nil, fmt.Errorf("SEC EDGAR provider is unavailable")
	}
	filing, err := s.deps.EDGAR.FetchLatest13F(ctx, source.ExternalKey)
	if err != nil {
		return nil, err
	}
	now := s.deps.Now().UTC()
	payload, _ := json.Marshal(map[string]any{"cik": filing.CIK, "accession": filing.Accession, "form": filing.Form, "holding_count": len(filing.Holdings)})
	observation := domain.CopySourceObservation{SourceID: source.ID, ProviderObservationID: filing.Accession, ObservationKind: "portfolio_snapshot", SchemaVersion: 1, EffectiveAt: filing.ReportPeriod.UTC(), PublishedAt: filing.FiledAt.UTC(), ObservedAt: now, Status: "active", ContentHash: filing.ContentHash, NormalizedPayload: payload, SourceURL: filing.SourceURL}
	if strings.HasSuffix(filing.Form, "/A") {
		observation.AmendmentNumber = 1
		previous, previousSnapshot, getErr := s.deps.Repo.GetLatest13FSnapshot(ctx, source.ID)
		if getErr == nil && previousSnapshot.ReportPeriod.Equal(filing.ReportPeriod) && previous.ProviderObservationID != filing.Accession {
			observation.SupersedesID = &previous.ID
		} else if getErr != nil && !errors.Is(getErr, repository.ErrNotFound) {
			return nil, getErr
		}
	}
	total := 0.0
	for _, holding := range filing.Holdings {
		total += holding.DisclosedValue
	}
	snapshot := domain.CopyPortfolioSnapshot{ReportPeriod: filing.ReportPeriod, TotalDisclosedValue: total, HoldingCount: len(filing.Holdings), Holdings: filing.Holdings}
	created, err := s.deps.Repo.Save13FSnapshot(ctx, &observation, &snapshot)
	if err != nil {
		return nil, err
	}
	checkpoint, _ := json.Marshal(map[string]any{"accession": filing.Accession, "content_hash": filing.ContentHash})
	if err := s.deps.Repo.UpdateSourceObserved(ctx, source.ID, now, checkpoint); err != nil {
		return nil, err
	}
	if err := s.deps.Repo.UpdateLeaderIdentityStatus(ctx, source.LeaderID, domain.CopyIdentityPublicFiling); err != nil {
		return nil, err
	}
	if !created {
		existingObservation, existingSnapshot, getErr := s.deps.Repo.GetLatest13FSnapshot(ctx, source.ID)
		if getErr == nil {
			observation, snapshot = *existingObservation, *existingSnapshot
		}
	}
	return &RefreshResult{Created: created, Observation: observation, Snapshot: snapshot}, nil
}

func (s *Service) UpsertMapping(ctx context.Context, mapping *domain.CopyInstrumentMapping) error {
	if err := mapping.Validate(); err != nil {
		return err
	}
	return s.deps.Repo.UpsertInstrumentMapping(ctx, mapping)
}

func (s *Service) CreateSubscription(ctx context.Context, subscription *domain.CopySubscription) error {
	if s.deps.Strategies == nil {
		return fmt.Errorf("strategy repository is unavailable")
	}
	if err := subscription.Validate(); err != nil {
		return err
	}
	leader, err := s.deps.Repo.GetLeader(ctx, subscription.LeaderID)
	if err != nil {
		return err
	}
	source, err := s.deps.Repo.GetSource(ctx, subscription.SourceID)
	if err != nil {
		return err
	}
	if source.LeaderID != leader.ID {
		return fmt.Errorf("source does not belong to leader")
	}
	if source.SourceType != domain.CopySourceSEC13F {
		return fmt.Errorf("MVP subscriptions require a sec_13f source")
	}
	config, _ := json.Marshal(map[string]any{"kind": "copy_subscription", "leader_id": leader.ID, "source_id": source.ID})
	strategy := &domain.Strategy{Name: "Replicate: " + leader.DisplayName, Description: "Internal paper strategy backing stock copy subscription", Ticker: "13F:" + source.ExternalKey, MarketType: domain.MarketTypeStock, Config: config, Status: domain.StrategyStatusPaused, IsPaper: true}
	if err := strategy.Validate(); err != nil {
		return err
	}
	if err := s.deps.Strategies.Create(ctx, strategy); err != nil {
		return err
	}
	subscription.StrategyID = strategy.ID
	if err := s.deps.Repo.CreateSubscription(ctx, subscription); err != nil {
		if cleanupErr := s.deps.Strategies.Delete(ctx, strategy.ID); cleanupErr != nil {
			s.deps.Logger.Error("copy trading: backing strategy cleanup failed", "strategy_id", strategy.ID, "error", cleanupErr)
		}
		return err
	}
	return nil
}

func (s *Service) ListSubscriptions(ctx context.Context, filter repository.CopySubscriptionFilter, limit, offset int) ([]domain.CopySubscription, int, error) {
	items, err := s.deps.Repo.ListSubscriptions(ctx, filter, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.deps.Repo.CountSubscriptions(ctx, filter)
	return items, total, err
}

func (s *Service) GetSubscription(ctx context.Context, id uuid.UUID) (*domain.CopySubscription, error) {
	return s.deps.Repo.GetSubscription(ctx, id)
}

func (s *Service) UpdateSubscription(ctx context.Context, id uuid.UUID, replacement *domain.CopySubscription) (*domain.CopySubscription, error) {
	current, err := s.deps.Repo.GetSubscription(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status != domain.CopySubscriptionDraft && current.Status != domain.CopySubscriptionPreviewed && current.Status != domain.CopySubscriptionPaused {
		return nil, fmt.Errorf("subscription can only be edited while draft, previewed, or paused")
	}
	replacement.ID, replacement.LeaderID, replacement.SourceID, replacement.StrategyID = current.ID, current.LeaderID, current.SourceID, current.StrategyID
	replacement.Status, replacement.IsPaper, replacement.CreatedBy, replacement.CreatedAt = current.Status, true, current.CreatedBy, current.CreatedAt
	if err := replacement.Validate(); err != nil {
		return nil, err
	}
	if err := s.deps.Repo.UpdateSubscription(ctx, replacement); err != nil {
		return nil, err
	}
	return replacement, nil
}

func (s *Service) Preview(ctx context.Context, subscriptionID uuid.UUID) (*Preview, error) {
	subscription, err := s.deps.Repo.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	observation, snapshot, err := s.deps.Repo.GetLatest13FSnapshot(ctx, subscription.SourceID)
	if err != nil {
		return nil, err
	}
	identifiers := make([]string, 0, len(snapshot.Holdings))
	for _, holding := range snapshot.Holdings {
		identifiers = append(identifiers, holding.CUSIP)
	}
	mappings, err := s.deps.Repo.ListInstrumentMappings(ctx, "sec", "cusip", identifiers)
	if err != nil {
		return nil, err
	}
	tickers := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		tickers = append(tickers, mapping.Ticker)
	}
	prices := map[string]PriceSnapshot{}
	if s.deps.Prices != nil && len(tickers) > 0 {
		prices, err = s.deps.Prices.Snapshots(ctx, tickers)
		if err != nil {
			return nil, fmt.Errorf("copy trading prices: %w", err)
		}
	}
	positions := []domain.Position{}
	if s.deps.Positions != nil {
		positions, err = s.deps.Positions.GetByStrategy(ctx, subscription.StrategyID, repository.PositionFilter{}, 1000, 0)
		if err != nil {
			return nil, err
		}
	}
	preview := Build13FTarget(TargetInput{Subscription: *subscription, Observation: *observation, Snapshot: *snapshot, Mappings: mappings, Prices: prices, Positions: positions})
	if subscription.Status == domain.CopySubscriptionDraft {
		subscription.Status = domain.CopySubscriptionPreviewed
		if err := s.deps.Repo.UpdateSubscription(ctx, subscription); err != nil {
			return nil, err
		}
	}
	return &preview, nil
}

func (s *Service) SetStatus(ctx context.Context, id uuid.UUID, next domain.CopySubscriptionStatus) (*domain.CopySubscription, error) {
	subscription, err := s.deps.Repo.GetSubscription(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := validateStatusTransition(subscription.Status, next); err != nil {
		return nil, err
	}
	if next == domain.CopySubscriptionPaperActive && subscription.Status == domain.CopySubscriptionDraft {
		if _, err := s.Preview(ctx, id); err != nil {
			return nil, fmt.Errorf("activation preview: %w", err)
		}
		subscription, err = s.deps.Repo.GetSubscription(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	subscription.Status = next
	if next == domain.CopySubscriptionStopped {
		now := s.deps.Now().UTC()
		subscription.StoppedAt = &now
	}
	if err := s.deps.Repo.UpdateSubscription(ctx, subscription); err != nil {
		return nil, err
	}
	if s.deps.Strategies != nil {
		strategy, getErr := s.deps.Strategies.Get(ctx, subscription.StrategyID)
		if getErr != nil {
			return nil, getErr
		}
		if next == domain.CopySubscriptionPaperActive {
			strategy.Status = domain.StrategyStatusActive
		} else {
			strategy.Status = domain.StrategyStatusPaused
		}
		if err := s.deps.Strategies.Update(ctx, strategy); err != nil {
			return nil, err
		}
	}
	return subscription, nil
}

func validateStatusTransition(current, next domain.CopySubscriptionStatus) error {
	allowed := map[domain.CopySubscriptionStatus]map[domain.CopySubscriptionStatus]bool{
		domain.CopySubscriptionDraft:       {domain.CopySubscriptionPreviewed: true, domain.CopySubscriptionPaperActive: true, domain.CopySubscriptionStopped: true},
		domain.CopySubscriptionPreviewed:   {domain.CopySubscriptionPaperActive: true, domain.CopySubscriptionStopped: true},
		domain.CopySubscriptionPaperActive: {domain.CopySubscriptionPaused: true, domain.CopySubscriptionStopped: true},
		domain.CopySubscriptionPaused:      {domain.CopySubscriptionPaperActive: true, domain.CopySubscriptionStopped: true},
	}
	if current == next {
		return nil
	}
	if !allowed[current][next] {
		return fmt.Errorf("invalid copy subscription transition %s -> %s", current, next)
	}
	return nil
}

type RebalanceResult struct {
	Run     domain.PipelineRun       `json:"run"`
	Preview Preview                  `json:"preview"`
	Intents []domain.CopyTradeIntent `json:"intents"`
}

type SyncSummary struct {
	Subscriptions  int `json:"subscriptions"`
	SourcesChecked int `json:"sources_checked"`
	NewFilings     int `json:"new_filings"`
	Rebalanced     int `json:"rebalanced"`
}

// Sync13FSubscriptions refreshes each subscribed source once and only
// rebalances active paper subscriptions when that source produced a new
// immutable observation. Paused subscriptions continue collecting filings.
func (s *Service) Sync13FSubscriptions(ctx context.Context) (SyncSummary, error) {
	var summary SyncSummary
	subscriptions := make([]domain.CopySubscription, 0)
	for offset := 0; ; offset += 100 {
		page, err := s.deps.Repo.ListSubscriptions(ctx, repository.CopySubscriptionFilter{}, 100, offset)
		if err != nil {
			return summary, err
		}
		subscriptions = append(subscriptions, page...)
		if len(page) < 100 {
			break
		}
	}
	summary.Subscriptions = len(subscriptions)
	newSource := make(map[uuid.UUID]bool)
	for _, subscription := range subscriptions {
		if subscription.Status == domain.CopySubscriptionStopped {
			continue
		}
		if _, checked := newSource[subscription.SourceID]; checked {
			continue
		}
		result, err := s.RefreshSource(ctx, subscription.SourceID)
		if err != nil {
			return summary, fmt.Errorf("refresh source %s: %w", subscription.SourceID, err)
		}
		summary.SourcesChecked++
		newSource[subscription.SourceID] = result.Created
		if result.Created {
			summary.NewFilings++
		}
	}
	for _, subscription := range subscriptions {
		if subscription.Status != domain.CopySubscriptionPaperActive || !newSource[subscription.SourceID] {
			continue
		}
		if _, err := s.Rebalance(ctx, subscription.ID); err != nil {
			return summary, fmt.Errorf("rebalance subscription %s: %w", subscription.ID, err)
		}
		summary.Rebalanced++
	}
	return summary, nil
}

func (s *Service) Rebalance(ctx context.Context, id uuid.UUID) (*RebalanceResult, error) {
	if s.deps.Runs == nil {
		return nil, fmt.Errorf("pipeline run repository is unavailable")
	}
	subscription, err := s.deps.Repo.GetSubscription(ctx, id)
	if err != nil {
		return nil, err
	}
	if subscription.Status != domain.CopySubscriptionPaperActive || !subscription.IsPaper {
		return nil, fmt.Errorf("subscription must be paper_active")
	}
	preview, err := s.Preview(ctx, id)
	if err != nil {
		return nil, err
	}
	now := s.deps.Now().UTC()
	config, _ := json.Marshal(map[string]any{"copy_subscription_id": id, "source_observation_id": preview.Observation.ID, "calculation_version": CalculationVersion})
	run := domain.PipelineRun{StrategyID: subscription.StrategyID, Ticker: "13F:" + subscription.SourceID.String(), TradeDate: now, Status: domain.PipelineStatusRunning, StartedAt: now, ConfigSnapshot: config}
	if err := s.deps.Runs.Create(ctx, &run); err != nil {
		return nil, err
	}
	result := &RebalanceResult{Run: run, Preview: *preview, Intents: make([]domain.CopyTradeIntent, 0, len(preview.Intents))}
	createdOrders := 0
	buyOrders := 0
	sellOrders := 0
	unexpectedErrors := 0
	for _, candidate := range preview.Intents {
		candidate.PipelineRunID = &run.ID
		created, createErr := s.deps.Repo.CreateIntent(ctx, &candidate)
		if createErr != nil {
			unexpectedErrors++
			continue
		}
		if !created {
			continue // idempotent replay of an already-processed source observation
		}
		if candidate.PolicyStatus != "approved" {
			result.Intents = append(result.Intents, candidate)
			continue
		}
		if s.deps.Executor == nil {
			candidate.Status = "failed"
			candidate.RiskStatus = "rejected"
			candidate.RiskReasons = []string{"paper_executor_unavailable"}
			unexpectedErrors++
		} else {
			executionResult, executeErr := s.deps.Executor.ExecuteCopyOrder(ctx, PaperOrderRequest{Subscription: *subscription, Intent: candidate, Run: run})
			if executeErr != nil {
				candidate.Status = "risk_rejected"
				candidate.RiskStatus = "rejected"
				candidate.RiskReasons = []string{executeErr.Error()}
			} else {
				candidate.OrderID = executionResult.OrderID
				candidate.RiskStatus = "approved"
				candidate.Status = "ordered"
				if executionResult.Status == domain.OrderStatusFilled {
					candidate.Status = "filled"
				}
				createdOrders++
				if candidate.Side == domain.OrderSideSell {
					sellOrders++
				} else {
					buyOrders++
				}
			}
		}
		if updateErr := s.deps.Repo.UpdateIntent(ctx, &candidate); updateErr != nil {
			unexpectedErrors++
		}
		result.Intents = append(result.Intents, candidate)
	}
	completed := s.deps.Now().UTC()
	status := domain.PipelineStatusCompleted
	signal := domain.PipelineSignalHold
	if createdOrders > 0 {
		signal = domain.PipelineSignalBuy
		if sellOrders > 0 && buyOrders == 0 {
			signal = domain.PipelineSignalSell
		}
	}
	message := ""
	if unexpectedErrors > 0 {
		status = domain.PipelineStatusFailed
		message = fmt.Sprintf("%d unexpected copy-rebalance error(s)", unexpectedErrors)
	}
	if err := s.deps.Runs.UpdateStatus(ctx, run.ID, run.TradeDate, repository.PipelineRunStatusUpdate{Status: status, Signal: &signal, CompletedAt: &completed, ErrorMessage: message}); err != nil {
		return nil, err
	}
	result.Run.Status, result.Run.Signal, result.Run.CompletedAt, result.Run.ErrorMessage = status, signal, &completed, message
	return result, nil
}

func (s *Service) ListIntents(ctx context.Context, subscriptionID uuid.UUID, limit, offset int) ([]domain.CopyTradeIntent, error) {
	return s.deps.Repo.ListIntents(ctx, subscriptionID, limit, offset)
}
