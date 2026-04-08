package signal

import (
	"context"
	"log/slog"
	"sync"

	"github.com/google/uuid"
)

// StrategyProvider supplies the hub with the current active strategy set so the
// watch index can be rebuilt and evaluator contexts populated.
type StrategyProvider interface {
	ListActiveWithThesis(ctx context.Context) ([]StrategyWithThesis, error)
}

// SignalHub fans in events from multiple SignalSources, filters them through the
// WatchIndex, scores them with the Evaluator, and emits TriggerEvents for
// urgency ≥ 3 to a downstream channel.
//
// Event flow:
//
//	SignalSource₁ ──┐
//	SignalSource₂ ──┼─→ WatchIndex.Match → Evaluator.Evaluate → TriggerEvent chan
//	SignalSource₃ ──┘
type SignalHub struct {
	sources    []SignalSource
	evaluator  *Evaluator
	watchIndex *WatchIndex
	strategies StrategyProvider
	triggerCh  chan<- TriggerEvent
	store      *EventStore // optional; nil = no persistence
	logger     *slog.Logger

	mu      sync.Mutex
	cancel  context.CancelFunc
	stopped chan struct{}
}

// NewSignalHub constructs a SignalHub. triggerCh receives TriggerEvents for
// urgency ≥ 3; the caller owns the channel and should buffer it. Pass a nil
// evaluator to skip LLM scoring (all matched events get urgency 3).
// store may be nil to skip event persistence.
func NewSignalHub(
	sources []SignalSource,
	evaluator *Evaluator,
	watchIndex *WatchIndex,
	strategies StrategyProvider,
	triggerCh chan<- TriggerEvent,
	store *EventStore,
	logger *slog.Logger,
) *SignalHub {
	if watchIndex == nil {
		watchIndex = NewWatchIndex()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &SignalHub{
		sources:    sources,
		evaluator:  evaluator,
		watchIndex: watchIndex,
		strategies: strategies,
		triggerCh:  triggerCh,
		store:      store,
		logger:     logger,
	}
}

// Start launches all sources and the evaluation loop. Returns immediately;
// call Stop to shut down gracefully.
func (h *SignalHub) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cancel != nil {
		return nil // already running
	}

	runCtx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	h.stopped = make(chan struct{})

	// Build initial watch index.
	if err := h.rebuildLocked(runCtx); err != nil {
		h.logger.Warn("signal hub: initial watch index build failed", slog.Any("error", err))
	}

	// Fan-in all source channels.
	merged := make(chan RawSignalEvent, 256)
	var wg sync.WaitGroup
	for _, src := range h.sources {
		ch, err := src.Start(runCtx)
		if err != nil {
			cancel()
			return err
		}
		wg.Add(1)
		go func(c <-chan RawSignalEvent) {
			defer wg.Done()
			for evt := range c {
				select {
				case merged <- evt:
				case <-runCtx.Done():
					return
				}
			}
		}(ch)
	}

	// Close merged when all sources are done.
	go func() {
		wg.Wait()
		close(merged)
	}()

	// Evaluation loop.
	go func() {
		defer close(h.stopped)
		for {
			select {
			case evt, ok := <-merged:
				if !ok {
					return
				}
				h.process(runCtx, evt)
			case <-runCtx.Done():
				return
			}
		}
	}()

	return nil
}

// Stop shuts down the hub and waits for the evaluation loop to finish.
func (h *SignalHub) Stop() {
	h.mu.Lock()
	cancel := h.cancel
	stopped := h.stopped
	h.cancel = nil
	h.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if stopped != nil {
		<-stopped
	}
}

// RebuildWatchIndex refreshes the watch index from the current strategy list.
// Safe to call at any time (e.g., after a strategy or thesis update).
func (h *SignalHub) RebuildWatchIndex(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rebuildLocked(ctx)
}

func (h *SignalHub) rebuildLocked(ctx context.Context) error {
	if h.strategies == nil {
		return nil
	}
	strategies, err := h.strategies.ListActiveWithThesis(ctx)
	if err != nil {
		return err
	}
	h.watchIndex.Rebuild(strategies)
	h.logger.Info("signal hub: watch index rebuilt", slog.Int("strategies", len(strategies)))
	return nil
}

// process runs a single RawSignalEvent through the filter → evaluate → emit pipeline.
func (h *SignalHub) process(ctx context.Context, evt RawSignalEvent) {
	searchText := evt.Title + " " + evt.Body
	matchedIDs := h.watchIndex.Match(searchText)
	if len(matchedIDs) == 0 {
		return // no strategies care about this event
	}

	h.logger.Debug("signal hub: event matched strategies",
		slog.String("source", evt.Source),
		slog.String("title", evt.Title),
		slog.Int("strategies", len(matchedIDs)),
	)

	// Build StrategyContext list for the evaluator.
	matchedSet := make(map[uuid.UUID]struct{}, len(matchedIDs))
	for _, id := range matchedIDs {
		matchedSet[id] = struct{}{}
	}

	var strategies []StrategyContext
	if h.strategies != nil {
		all, err := h.strategies.ListActiveWithThesis(ctx)
		if err != nil {
			h.logger.Warn("signal hub: failed to load strategies for evaluation", slog.Any("error", err))
		} else {
			for _, s := range all {
				if _, ok := matchedSet[s.ID]; ok {
					strategies = append(strategies, StrategyContext{
						ID:         s.ID,
						Ticker:     s.Ticker,
						WatchTerms: s.WatchTerms,
					})
				}
			}
		}
	}
	if len(strategies) == 0 {
		// Fallback: use bare IDs with no thesis context.
		for _, id := range matchedIDs {
			strategies = append(strategies, StrategyContext{ID: id})
		}
	}

	// Evaluate with LLM (or fallback urgency 3 if evaluator is nil).
	var evaluated *EvaluatedSignal
	if h.evaluator != nil {
		var err error
		evaluated, err = h.evaluator.Evaluate(ctx, evt, strategies)
		if err != nil {
			h.logger.Warn("signal hub: evaluator error", slog.Any("error", err))
		}
	}
	if evaluated == nil {
		// Nil evaluator or evaluation returned nil — use urgency-3 fallback.
		ids := make([]uuid.UUID, len(strategies))
		for i, s := range strategies {
			ids[i] = s.ID
		}
		evaluated = &EvaluatedSignal{
			Raw:                evt,
			AffectedStrategies: ids,
			Urgency:            3,
			Summary:            evt.Title,
			RecommendedAction:  "monitor",
		}
	}

	if h.store != nil {
		h.store.RecordSignal(*evaluated)
	}

	if evaluated.Urgency < 3 {
		h.logger.Debug("signal hub: urgency below threshold, dropping",
			slog.String("title", evt.Title),
			slog.Int("urgency", evaluated.Urgency),
		)
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
		case h.triggerCh <- trigger:
		case <-ctx.Done():
			return
		default:
			h.logger.Warn("signal hub: trigger channel full, dropping event",
				slog.String("strategy_id", stratID.String()),
				slog.String("title", evt.Title),
			)
		}
	}
}

// urgencyToAction maps urgency + recommended_action to a TriggerAction.
func urgencyToAction(urgency int, recommended string) TriggerAction {
	if urgency >= 5 || recommended == "execute_thesis" {
		return TriggerActionExecuteThesis
	}
	if urgency >= 3 || recommended == "re-evaluate" {
		return TriggerActionRunPipeline
	}
	return TriggerActionLogOnly
}
