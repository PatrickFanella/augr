package signal

import (
	"context"
	"log/slog"
)

// OrchestratorConfig holds all configuration for the signal intelligence stack.
type OrchestratorConfig struct {
	EventStoreSize int // number of events to retain in memory (default 200)

	// LLM evaluator (optional; nil = urgency-3 fallback for all events).
	LLMEvaluator *Evaluator

	// Signal sources to fan-in. If empty, default RSS + Reddit sources are used.
	Sources []SignalSource

	// Trigger channel buffer size (default 64).
	TriggerChanSize int
}

// OrchestratorDeps holds external dependencies for the signal stack.
type OrchestratorDeps struct {
	StrategyProvider StrategyProvider // required for hub watch-index rebuilds
	StrategyLoader   StrategyLoader   // required for trigger handler to look up strategies
	ThesisLoader     ThesisLoader     // optional; nil disables thesis fast-path
	Runner           StrategyTriggerer
	Logger           *slog.Logger
}

// Orchestrator owns the full signal intelligence lifecycle:
// EventStore, WatchIndex, SignalHub, and TriggerHandler.
type Orchestrator struct {
	store      *EventStore
	watchIndex *WatchIndex
	hub        *SignalHub
	handler    *TriggerHandler
	cancel     context.CancelFunc
	logger     *slog.Logger
}

// NewOrchestrator assembles the signal stack but does not start it.
// Call Start to begin processing.
func NewOrchestrator(cfg OrchestratorConfig, deps OrchestratorDeps) *Orchestrator {
	storeSize := cfg.EventStoreSize
	if storeSize <= 0 {
		storeSize = 200
	}
	trigSize := cfg.TriggerChanSize
	if trigSize <= 0 {
		trigSize = 64
	}

	store := NewEventStore(storeSize)
	watchIndex := NewWatchIndex()
	triggerCh := make(chan TriggerEvent, trigSize)

	hub := NewSignalHub(cfg.Sources, cfg.LLMEvaluator, watchIndex, deps.StrategyProvider, triggerCh, store, deps.Logger)

	handler := NewTriggerHandler(triggerCh, deps.StrategyLoader, deps.ThesisLoader, deps.Runner, store, deps.Logger)

	return &Orchestrator{
		store:      store,
		watchIndex: watchIndex,
		hub:        hub,
		handler:    handler,
		logger:     deps.Logger,
	}
}

// Start launches the hub and trigger handler. Safe to call once.
// If the Runner dependency is nil, Start is a no-op (returns nil immediately).
func (o *Orchestrator) Start(ctx context.Context) error {
	if o.handler == nil {
		return nil
	}
	if err := o.hub.Start(ctx); err != nil {
		return err
	}
	handlerCtx, cancel := context.WithCancel(ctx)
	o.cancel = cancel
	go o.handler.Run(handlerCtx)
	return nil
}

// Stop gracefully shuts down the hub and trigger handler.
func (o *Orchestrator) Stop() {
	o.hub.Stop()
	if o.cancel != nil {
		o.cancel()
	}
}

// Store returns the event store for API reads.
func (o *Orchestrator) Store() *EventStore { return o.store }

// WatchIndex returns the watch index for API reads and manual term management.
func (o *Orchestrator) WatchIndex() *WatchIndex { return o.watchIndex }
