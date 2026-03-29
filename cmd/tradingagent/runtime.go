package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

func newAPIServer(ctx context.Context, cfg config.Config, logger *slog.Logger) (*api.Server, func(), error) {
	db, err := pgrepo.NewDB(ctx, cfg.Database.URL)
	if err != nil {
		return nil, nil, err
	}

	strategyRepo := pgrepo.NewStrategyRepo(db.Pool)
	runRepo := pgrepo.NewPipelineRunRepo(db.Pool)
	decisionRepo := pgrepo.NewAgentDecisionRepo(db.Pool)
	orderRepo := pgrepo.NewOrderRepo(db.Pool)
	positionRepo := pgrepo.NewPositionRepo(db.Pool)
	tradeRepo := pgrepo.NewTradeRepo(db.Pool)
	memoryRepo := pgrepo.NewMemoryRepo(db.Pool)
	apiKeyRepo := pgrepo.NewAPIKeyRepo(db.Pool)
	auditLogRepo := pgrepo.NewAuditLogRepo(db.Pool)

	riskEngine := risk.NewRiskEngine(
		risk.PositionLimits{
			MaxPerPositionPct: cfg.Risk.MaxPositionSizePct,
			MaxTotalPct:       1.0,
			MaxConcurrent:     cfg.Risk.MaxOpenPositions,
			MaxPerMarketPct:   0.50,
		},
		risk.CircuitBreakerConfig{
			MaxDailyLossPct:      cfg.Risk.MaxDailyLossPct,
			MaxDrawdownPct:       cfg.Risk.MaxDrawdownPct,
			MaxConsecutiveLosses: 5,
			CooldownDuration:     cfg.Risk.CircuitBreakerCooldown,
		},
		positionRepo,
		logger,
	)

	deps := api.Deps{
		Strategies: strategyRepo,
		Runs:       runRepo,
		Decisions:  decisionRepo,
		Orders:     orderRepo,
		Positions:  positionRepo,
		Trades:     tradeRepo,
		Memories:   memoryRepo,
		APIKeys:    apiKeyRepo,
		Risk:       riskEngine,
		Settings:   api.NewMemorySettingsServiceFromConfig(cfg),
	}

	if strings.EqualFold(cfg.Environment, "smoke") {
		deps.Runner = newSmokeStrategyRunner(runRepo, decisionRepo, orderRepo, positionRepo, tradeRepo, auditLogRepo, riskEngine, logger)
	}

	apiCfg := api.DefaultServerConfig()
	apiCfg.Host = cfg.Server.Host
	apiCfg.Port = cfg.Server.Port
	apiCfg.JWTSecret = cfg.Server.JWTSecret
	apiCfg.RefreshTokenTTL = 24 * time.Hour

	server, err := api.NewServer(apiCfg, deps, logger)
	if err != nil {
		db.Close()
		return nil, nil, err
	}

	return server, db.Close, nil
}

type smokeStrategyRunner struct {
	pipeline     *agent.Pipeline
	runRepo      repository.PipelineRunRepository
	orderRepo    repository.OrderRepository
	positionRepo repository.PositionRepository
	orderManager *execution.OrderManager
}

func newSmokeStrategyRunner(
	runRepo repository.PipelineRunRepository,
	decisionRepo repository.AgentDecisionRepository,
	orderRepo repository.OrderRepository,
	positionRepo repository.PositionRepository,
	tradeRepo repository.TradeRepository,
	auditLogRepo repository.AuditLogRepository,
	riskEngine risk.RiskEngine,
	logger *slog.Logger,
) api.StrategyRunner {
	pipeline := newSmokePipeline(runRepo, decisionRepo, logger)
	orderManager := execution.NewOrderManager(
		paper.NewPaperBroker(100_000, 0, 0),
		"paper",
		riskEngine,
		positionRepo,
		orderRepo,
		tradeRepo,
		auditLogRepo,
		execution.SizingConfig{
			Method:      execution.PositionSizingMethodFixedFractional,
			FractionPct: 0.05,
		},
		logger,
	)

	return &smokeStrategyRunner{
		pipeline:     pipeline,
		runRepo:      runRepo,
		orderRepo:    orderRepo,
		positionRepo: positionRepo,
		orderManager: orderManager,
	}
}

func (r *smokeStrategyRunner) RunStrategy(ctx context.Context, strategy domain.Strategy) (*api.StrategyRunResult, error) {
	state, err := r.pipeline.Execute(ctx, strategy.ID, strategy.Ticker)
	if err != nil {
		return nil, err
	}

	run, err := r.findRun(ctx, state.PipelineRunID)
	if err != nil {
		return nil, err
	}

	signal := state.FinalSignal.Signal
	if signal == "" {
		signal = state.TradingPlan.Action
	}
	if signal == "" {
		signal = domain.PipelineSignalHold
	}

	update := repository.PipelineRunStatusUpdate{
		Status:       run.Status,
		Signal:       &signal,
		CompletedAt:  run.CompletedAt,
		ErrorMessage: run.ErrorMessage,
	}
	if err := r.runRepo.UpdateStatus(ctx, run.ID, run.TradeDate, update); err != nil {
		return nil, err
	}
	run.Signal = signal

	if err := r.orderManager.ProcessSignal(
		ctx,
		execution.FinalSignal{
			Signal:     signal,
			Confidence: state.FinalSignal.Confidence,
		},
		execution.TradingPlan{
			Action:       signal,
			Ticker:       state.TradingPlan.Ticker,
			EntryType:    state.TradingPlan.EntryType,
			EntryPrice:   state.TradingPlan.EntryPrice,
			PositionSize: state.TradingPlan.PositionSize,
			StopLoss:     state.TradingPlan.StopLoss,
			TakeProfit:   state.TradingPlan.TakeProfit,
			TimeHorizon:  state.TradingPlan.TimeHorizon,
			Confidence:   state.TradingPlan.Confidence,
			Rationale:    state.TradingPlan.Rationale,
			RiskReward:   state.TradingPlan.RiskReward,
		},
		strategy.ID,
		run.ID,
	); err != nil {
		return nil, err
	}

	orders, err := r.orderRepo.GetByRun(ctx, run.ID, repository.OrderFilter{}, 10, 0)
	if err != nil {
		return nil, err
	}
	positions, err := r.positionRepo.GetByStrategy(ctx, strategy.ID, repository.PositionFilter{}, 10, 0)
	if err != nil {
		return nil, err
	}

	return &api.StrategyRunResult{
		Run:       *run,
		Signal:    signal,
		Orders:    orders,
		Positions: positions,
	}, nil
}

func (r *smokeStrategyRunner) findRun(ctx context.Context, runID uuid.UUID) (*domain.PipelineRun, error) {
	tradeDate := time.Now().UTC().Truncate(24 * time.Hour)
	run, err := r.runRepo.Get(ctx, runID, tradeDate)
	if err == nil {
		return run, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	const pageSize = 100
	for offset := 0; ; offset += pageSize {
		runs, err := r.runRepo.List(ctx, repository.PipelineRunFilter{}, pageSize, offset)
		if err != nil {
			return nil, err
		}
		if len(runs) == 0 {
			break
		}
		for i := range runs {
			if runs[i].ID == runID {
				return &runs[i], nil
			}
		}
		if len(runs) < pageSize {
			break
		}
	}
	return nil, fmt.Errorf("run %s: %w", runID, repository.ErrNotFound)
}

func newSmokePipeline(
	runRepo repository.PipelineRunRepository,
	decisionRepo repository.AgentDecisionRepository,
	logger *slog.Logger,
) *agent.Pipeline {
	pipeline := agent.NewPipeline(
		agent.PipelineConfig{
			ResearchDebateRounds: 1,
			RiskDebateRounds:     1,
		},
		agent.NewRepoPersister(runRepo, decisionRepo, logger),
		nil,
		logger,
	)

	pipeline.RegisterNode(smokeNode{
		name:  "smoke-market-analyst",
		role:  agent.AgentRoleMarketAnalyst,
		phase: agent.PhaseAnalysis,
		exec: func(state *agent.PipelineState) error {
			const report = "Smoke test market analysis indicates bullish momentum."
			state.SetAnalystReport(agent.AgentRoleMarketAnalyst, report)
			state.RecordDecision(agent.AgentRoleMarketAnalyst, agent.PhaseAnalysis, nil, report, nil)
			return nil
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-bull-researcher",
		role:  agent.AgentRoleBullResearcher,
		phase: agent.PhaseResearchDebate,
		exec: func(state *agent.PipelineState) error {
			return recordResearchDebateContribution(&state.ResearchDebate, agent.AgentRoleBullResearcher, "Bull case: strong setup for a paper-trade entry.")
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-bear-researcher",
		role:  agent.AgentRoleBearResearcher,
		phase: agent.PhaseResearchDebate,
		exec: func(state *agent.PipelineState) error {
			return recordResearchDebateContribution(&state.ResearchDebate, agent.AgentRoleBearResearcher, "Bear case: downside risk is bounded by the configured stop.")
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-invest-judge",
		role:  agent.AgentRoleInvestJudge,
		phase: agent.PhaseResearchDebate,
		exec: func(state *agent.PipelineState) error {
			const plan = "Proceed with a small paper buy to validate the execution path."
			state.ResearchDebate.InvestmentPlan = plan
			state.RecordDecision(agent.AgentRoleInvestJudge, agent.PhaseResearchDebate, nil, plan, nil)
			return nil
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-trader",
		role:  agent.AgentRoleTrader,
		phase: agent.PhaseTrading,
		exec: func(state *agent.PipelineState) error {
			state.TradingPlan = agent.TradingPlan{
				Action:       domain.PipelineSignalBuy,
				Ticker:       state.Ticker,
				EntryType:    "market",
				EntryPrice:   100,
				PositionSize: 0.05,
				StopLoss:     95,
				TakeProfit:   110,
				TimeHorizon:  "1d",
				Confidence:   0.92,
				Rationale:    "Smoke test deterministic trading plan",
				RiskReward:   2,
			}
			payload, err := json.Marshal(state.TradingPlan)
			if err != nil {
				return err
			}
			state.RecordDecision(agent.AgentRoleTrader, agent.PhaseTrading, nil, string(payload), nil)
			return nil
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-aggressive-risk",
		role:  agent.AgentRoleAggressiveAnalyst,
		phase: agent.PhaseRiskDebate,
		exec: func(state *agent.PipelineState) error {
			return recordRiskDebateContribution(&state.RiskDebate, agent.AgentRoleAggressiveAnalyst, "Aggressive view: approve the trade.")
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-conservative-risk",
		role:  agent.AgentRoleConservativeAnalyst,
		phase: agent.PhaseRiskDebate,
		exec: func(state *agent.PipelineState) error {
			return recordRiskDebateContribution(&state.RiskDebate, agent.AgentRoleConservativeAnalyst, "Conservative view: size is acceptable for smoke validation.")
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-neutral-risk",
		role:  agent.AgentRoleNeutralAnalyst,
		phase: agent.PhaseRiskDebate,
		exec: func(state *agent.PipelineState) error {
			return recordRiskDebateContribution(&state.RiskDebate, agent.AgentRoleNeutralAnalyst, "Neutral view: proceed and observe the paper execution.")
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-risk-manager",
		role:  agent.AgentRoleRiskManager,
		phase: agent.PhaseRiskDebate,
		exec: func(state *agent.PipelineState) error {
			state.FinalSignal = agent.FinalSignal{
				Signal:     domain.PipelineSignalBuy,
				Confidence: 0.92,
			}
			const storedSignal = `{"action":"buy","confidence":0.92}`
			state.RiskDebate.FinalSignal = storedSignal
			state.RecordDecision(agent.AgentRoleRiskManager, agent.PhaseRiskDebate, nil, storedSignal, nil)
			return nil
		},
	})

	return pipeline
}

type smokeNode struct {
	name  string
	role  agent.AgentRole
	phase agent.Phase
	exec  func(state *agent.PipelineState) error
}

func (n smokeNode) Name() string          { return n.name }
func (n smokeNode) Role() agent.AgentRole { return n.role }
func (n smokeNode) Phase() agent.Phase    { return n.phase }

func (n smokeNode) Execute(ctx context.Context, state *agent.PipelineState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if n.exec == nil {
		return nil
	}
	return n.exec(state)
}

func recordResearchDebateContribution(state *agent.ResearchDebateState, role agent.AgentRole, contribution string) error {
	if state == nil {
		return fmt.Errorf("research debate state is required")
	}
	return recordDebateRoundContribution(state.Rounds, func(rounds []agent.DebateRound) {
		state.Rounds = rounds
	}, role, contribution)
}

func recordRiskDebateContribution(state *agent.RiskDebateState, role agent.AgentRole, contribution string) error {
	if state == nil {
		return fmt.Errorf("risk debate state is required")
	}
	return recordDebateRoundContribution(state.Rounds, func(rounds []agent.DebateRound) {
		state.Rounds = rounds
	}, role, contribution)
}

func recordDebateRoundContribution(
	rounds []agent.DebateRound,
	setRounds func([]agent.DebateRound),
	role agent.AgentRole,
	contribution string,
) error {
	if len(rounds) == 0 {
		return fmt.Errorf("debate round is not initialized")
	}

	round := rounds[len(rounds)-1]
	if round.Contributions == nil {
		round.Contributions = make(map[agent.AgentRole]string)
	}
	round.Contributions[role] = contribution
	rounds[len(rounds)-1] = round
	setRounds(rounds)

	return nil
}
