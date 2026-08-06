package signal

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
)

// Lifecycle owns the signal flow from source ingestion to trigger emission.
type Lifecycle struct {
	watchIndex *WatchIndex
	strategies StrategyProvider
	evaluator  SignalEvaluator
	triggerCh  chan<- TriggerEvent
	store      *EventStore
	recorder   EventRecorder
	logger     *slog.Logger
}

// WithEventRecorder enables fail-closed durable signal evaluation lineage.
func (l *Lifecycle) WithEventRecorder(recorder EventRecorder) *Lifecycle {
	if l != nil {
		l.recorder = recorder
	}
	return l
}

// NewLifecycle assembles the explicit signal flow.
func NewLifecycle(
	watchIndex *WatchIndex,
	strategies StrategyProvider,
	evaluator SignalEvaluator,
	triggerCh chan<- TriggerEvent,
	store *EventStore,
	logger *slog.Logger,
) *Lifecycle {
	if watchIndex == nil {
		watchIndex = NewWatchIndex()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Lifecycle{
		watchIndex: watchIndex,
		strategies: strategies,
		evaluator:  evaluator,
		triggerCh:  triggerCh,
		store:      store,
		logger:     logger,
	}
}

// RebuildWatchIndex refreshes the keyword index from the active strategies.
func (l *Lifecycle) RebuildWatchIndex(ctx context.Context) error {
	if l == nil || l.watchIndex == nil || l.strategies == nil {
		return nil
	}
	strategies, err := l.strategies.ListActiveWithThesis(ctx)
	if err != nil {
		return err
	}
	l.watchIndex.Rebuild(strategies)
	l.logger.Info("signal lifecycle: watch index rebuilt", slog.Int("strategies", len(strategies)))
	return nil
}

// Process runs one raw signal through match → strategy lookup → evaluate → emit.
func (l *Lifecycle) Process(ctx context.Context, evt RawSignalEvent) {
	if l == nil || l.watchIndex == nil {
		return
	}

	matchedIDs := l.watchIndex.Match(evt.Title + " " + evt.Body)
	if len(matchedIDs) == 0 {
		return
	}

	l.logger.Debug("signal lifecycle: event matched strategies",
		slog.String("source", evt.Source),
		slog.String("title", evt.Title),
		slog.Int("strategies", len(matchedIDs)),
	)

	strategies := l.lookupStrategies(ctx, matchedIDs)
	evaluated := l.evaluate(ctx, evt, strategies)
	if evaluated == nil {
		return
	}
	if l.recorder != nil {
		if err := l.recorder.RecordEvaluated(ctx, *evaluated); err != nil {
			l.logger.Error("signal lifecycle: failed to persist evaluated event; dropping",
				slog.String("source", evt.Source),
				slog.String("title", evt.Title),
				slog.Any("error", err),
			)
			return
		}
	}

	if l.store != nil {
		l.store.RecordSignal(*evaluated)
	}

	if evaluated.Urgency < 3 {
		l.logger.Debug("signal lifecycle: urgency below threshold, dropping",
			slog.String("title", evt.Title),
			slog.Int("urgency", evaluated.Urgency),
		)
		return
	}

	if l.triggerCh == nil {
		return
	}

	action := urgencyToAction(evaluated.Urgency, evaluated.RecommendedAction)
	for _, stratID := range evaluated.AffectedStrategies {
		trigger := TriggerEvent{
			Signal:     *evaluated,
			StrategyID: stratID,
			Action:     action,
			Priority:   evaluated.Urgency,
		}
		select {
		case l.triggerCh <- trigger:
		case <-ctx.Done():
			return
		default:
			l.logger.Warn("signal lifecycle: trigger channel full, dropping event",
				slog.String("strategy_id", stratID.String()),
				slog.String("title", evt.Title),
			)
		}
	}
}

func (l *Lifecycle) lookupStrategies(ctx context.Context, matchedIDs []uuid.UUID) []StrategyContext {
	matchedSet := make(map[uuid.UUID]struct{}, len(matchedIDs))
	for _, id := range matchedIDs {
		matchedSet[id] = struct{}{}
	}

	if l.strategies != nil {
		all, err := l.strategies.ListActiveWithThesis(ctx)
		if err != nil {
			l.logger.Warn("signal lifecycle: failed to load strategies for evaluation", slog.Any("error", err))
		} else {
			strategies := make([]StrategyContext, 0, len(matchedIDs))
			for _, s := range all {
				if _, ok := matchedSet[s.ID]; ok {
					strategies = append(strategies, StrategyContext{
						ID:         s.ID,
						Ticker:     s.Ticker,
						WatchTerms: s.WatchTerms,
					})
				}
			}
			if len(strategies) != 0 {
				return strategies
			}
		}
	}

	strategies := make([]StrategyContext, 0, len(matchedIDs))
	for _, id := range matchedIDs {
		strategies = append(strategies, StrategyContext{ID: id})
	}
	return strategies
}

func (l *Lifecycle) evaluate(ctx context.Context, evt RawSignalEvent, strategies []StrategyContext) *EvaluatedSignal {
	if l.evaluator != nil {
		evaluated, err := l.evaluator.Evaluate(ctx, evt, strategies)
		if err == nil && evaluated != nil {
			return evaluated
		}
		if err != nil {
			l.logger.Warn("signal lifecycle: evaluator error", slog.Any("error", err))
		}
	}

	// An unavailable or failed evaluator must not manufacture pipeline work.
	return &EvaluatedSignal{
		Raw:                evt,
		AffectedStrategies: nil,
		Urgency:            1,
		Summary:            evt.Title,
		RecommendedAction:  "monitor",
	}
}

func urgencyToAction(urgency int, recommended string) TriggerAction {
	if urgency >= 5 || recommended == "execute_thesis" {
		return TriggerActionExecuteThesis
	}
	if urgency >= 3 || recommended == "re-evaluate" {
		return TriggerActionRunPipeline
	}
	return TriggerActionLogOnly
}
