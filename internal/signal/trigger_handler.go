package signal

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// StrategyLoader loads a strategy by ID for the trigger handler.
type StrategyLoader interface {
	Get(ctx context.Context, id uuid.UUID) (*domain.Strategy, error)
}

// StrategyTriggerer triggers an immediate pipeline run for a strategy.
type StrategyTriggerer interface {
	TriggerStrategy(strategy domain.Strategy)
}

// ThesisLoader loads the serialised thesis JSON from persistent storage.
// Returns nil, nil when no thesis is stored.
type ThesisLoader interface {
	GetThesisRaw(ctx context.Context, strategyID uuid.UUID) (json.RawMessage, error)
}

// TriggerHandler consumes TriggerEvents from the SignalHub and dispatches them
// according to their urgency:
//
//	Urgency 1-2: logged, no action (hub normally drops these, but handler guards).
//	Urgency 3-4: queue pipeline run via StrategyTriggerer.
//	Urgency 5:   load stored thesis; if valid → TriggerActionExecuteThesis queues
//	             a fast-track run; if thesis is missing/expired → queue normal run.
type TriggerHandler struct {
	triggerCh  <-chan TriggerEvent
	strategies StrategyLoader
	thesis     ThesisLoader // optional; nil disables thesis-aware fast-path
	runner     StrategyTriggerer
	store      *EventStore // optional; nil = no trigger persistence
	logger     *slog.Logger
}

// NewTriggerHandler constructs a TriggerHandler. thesisLoader and store may be nil.
func NewTriggerHandler(
	triggerCh <-chan TriggerEvent,
	strategies StrategyLoader,
	thesisLoader ThesisLoader,
	runner StrategyTriggerer,
	store *EventStore,
	logger *slog.Logger,
) *TriggerHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &TriggerHandler{
		triggerCh:  triggerCh,
		strategies: strategies,
		thesis:     thesisLoader,
		runner:     runner,
		store:      store,
		logger:     logger,
	}
}

// Run reads from the trigger channel until ctx is cancelled.
// Call in a goroutine: go handler.Run(ctx)
func (h *TriggerHandler) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-h.triggerCh:
			if !ok {
				return
			}
			h.handle(ctx, evt)
		}
	}
}

func (h *TriggerHandler) handle(ctx context.Context, evt TriggerEvent) {
	if h.store != nil {
		h.store.RecordTrigger(evt)
	}

	log := h.logger.With(
		slog.String("strategy_id", evt.StrategyID.String()),
		slog.String("title", evt.Signal.Summary),
		slog.Int("urgency", evt.Signal.Urgency),
		slog.String("action", string(evt.Action)),
	)

	switch evt.Action {
	case TriggerActionLogOnly:
		log.Info("signal trigger: logging only")
		return

	case TriggerActionRunPipeline:
		strategy, err := h.loadStrategy(ctx, evt.StrategyID)
		if err != nil {
			log.Warn("signal trigger: strategy load failed", slog.Any("error", err))
			return
		}
		log.Info("signal trigger: queuing pipeline run")
		h.runner.TriggerStrategy(*strategy)

	case TriggerActionExecuteThesis:
		strategy, err := h.loadStrategy(ctx, evt.StrategyID)
		if err != nil {
			log.Warn("signal trigger: strategy load failed", slog.Any("error", err))
			return
		}

		thesis := h.loadValidThesis(ctx, evt.StrategyID, log)
		if thesis != nil {
			log.Info("signal trigger: executing standing thesis (fast-track run)",
				slog.Float64("conviction", thesis.Conviction),
				slog.String("direction", thesis.Direction),
			)
		} else {
			log.Info("signal trigger: no valid thesis, queuing pipeline run")
		}
		// In both cases dispatch a pipeline run; the runner uses the stored
		// thesis for fast-track execution if it has one.
		h.runner.TriggerStrategy(*strategy)
	}
}

func (h *TriggerHandler) loadStrategy(ctx context.Context, id uuid.UUID) (*domain.Strategy, error) {
	return h.strategies.Get(ctx, id)
}

func (h *TriggerHandler) loadValidThesis(ctx context.Context, strategyID uuid.UUID, log *slog.Logger) *agent.Thesis {
	if h.thesis == nil {
		return nil
	}
	raw, err := h.thesis.GetThesisRaw(ctx, strategyID)
	if err != nil || len(raw) == 0 {
		if err != nil {
			log.Warn("signal trigger: thesis load error", slog.Any("error", err))
		}
		return nil
	}

	var t agent.Thesis
	if err := json.Unmarshal(raw, &t); err != nil {
		log.Warn("signal trigger: thesis unmarshal failed", slog.Any("error", err))
		return nil
	}
	if t.IsExpired() {
		log.Info("signal trigger: thesis expired, falling back to pipeline run",
			slog.Time("invalid_after", *t.InvalidAfter))
		return nil
	}
	return &t
}
