package agent

import (
	"context"
	"log/slog"
)

// DebateConfig configures a debate phase execution.
type DebateConfig struct {
	Phase       Phase
	Rounds      int
	Debaters    []Node
	Judge       Node
	AppendRound func(state *PipelineState, round DebateRound)
}

// DebateExecutor runs a multi-round debate followed by a judge decision.
type DebateExecutor struct {
	pipeline *Pipeline
	config   DebateConfig
}

// NewDebateExecutor constructs a DebateExecutor with the supplied Pipeline and
// configuration.
func NewDebateExecutor(p *Pipeline, cfg DebateConfig) *DebateExecutor {
	return &DebateExecutor{
		pipeline: p,
		config:   cfg,
	}
}

// Execute runs the configured number of debate rounds, executing each debater
// sequentially within a round, then runs the judge node to produce a final
// decision. It applies the pipeline's PhaseTimeout, clamps rounds to >= 1,
// persists decisions, and emits DebateRoundCompleted events.
func (d *DebateExecutor) Execute(ctx context.Context, state *PipelineState) error {
	phaseCtx := ctx
	if d.pipeline.config.PhaseTimeout > 0 {
		var cancel context.CancelFunc
		phaseCtx, cancel = context.WithTimeout(ctx, d.pipeline.config.PhaseTimeout)
		defer cancel()
	}

	rounds := d.config.Rounds
	if rounds < 1 {
		d.pipeline.logger.Warn("agent/pipeline: invalid debate rounds; clamping to 1",
			slog.String("phase", string(d.config.Phase)),
			slog.Int("configured_rounds", d.config.Rounds),
		)
		rounds = 1
	}

	for i := 1; i <= rounds; i++ {
		roundNumber := i

		// Check for context cancellation before starting the round.
		if err := phaseCtx.Err(); err != nil {
			return err
		}

		// Prepare the round structure so nodes can write contributions.
		d.config.AppendRound(state, DebateRound{
			Number:        i,
			Contributions: make(map[AgentRole]string),
		})

		// Execute each debater sequentially.
		for _, debater := range d.config.Debaters {
			d.pipeline.persistStructuredEvent(phaseCtx, d.pipeline.newStructuredEvent(
				state.PipelineRunID,
				state.StrategyID,
				AgentEventKindAgentStarted,
				debater.Role(),
				"Agent started",
				"",
				map[string]any{
					"phase":        d.config.Phase.String(),
					"agent_role":   debater.Role().String(),
					"round_number": roundNumber,
				},
				[]string{"agent", d.config.Phase.String()},
			))

			if dn, ok := debater.(DebaterNode); ok {
				input := DebateInput{
					Ticker:         state.Ticker,
					Rounds:         d.debateRounds(state),
					ContextReports: d.contextReports(state),
				}
				result, err := dn.Debate(phaseCtx, input)
				if err != nil {
					return err
				}
				ApplyDebateOutput(state, debater.Role(), d.config.Phase, d.debateRounds(state), result)
			} else {
				if err := debater.Execute(phaseCtx, state); err != nil {
					return err
				}
			}
			output, llmResponse, err := d.pipeline.decisionPayload(state, debater, &roundNumber)
			if err != nil {
				return err
			}
			if err := d.pipeline.persister.PersistDecision(phaseCtx, state.PipelineRunID, debater, &roundNumber, output, llmResponse); err != nil {
				return err
			}
			d.pipeline.persistStructuredEvent(phaseCtx, d.pipeline.newStructuredEvent(
				state.PipelineRunID,
				state.StrategyID,
				AgentEventKindAgentCompleted,
				debater.Role(),
				"Agent completed",
				"",
				map[string]any{
					"phase":        d.config.Phase.String(),
					"agent_role":   debater.Role().String(),
					"round_number": roundNumber,
				},
				[]string{"agent", d.config.Phase.String()},
			))
		}
		d.pipeline.persistStructuredEvent(phaseCtx, d.pipeline.newStructuredEvent(
			state.PipelineRunID,
			state.StrategyID,
			AgentEventKindDebateRoundCompleted,
			"",
			"Debate round completed",
			"",
			map[string]any{
				"phase":        d.config.Phase.String(),
				"round_number": roundNumber,
			},
			[]string{"debate", d.config.Phase.String()},
		))

		// Emit DebateRoundCompleted event.
		if d.pipeline.events != nil {
			event := PipelineEvent{
				Type:          DebateRoundCompleted,
				PipelineRunID: state.PipelineRunID,
				StrategyID:    state.StrategyID,
				Ticker:        state.Ticker,
				Phase:         d.config.Phase,
				Round:         i,
				OccurredAt:    d.pipeline.currentTime().UTC(),
			}
			select {
			case d.pipeline.events <- event:
			case <-phaseCtx.Done():
				d.pipeline.logger.Debug("agent/pipeline: DebateRoundCompleted event dropped; phase context cancelled",
					slog.Int("round", i),
				)
			default:
				d.pipeline.logger.Debug("agent/pipeline: DebateRoundCompleted event dropped; events channel full",
					slog.Int("round", i),
				)
			}
		}
	}

	// Execute the judge node.
	d.pipeline.persistStructuredEvent(phaseCtx, d.pipeline.newStructuredEvent(
		state.PipelineRunID,
		state.StrategyID,
		AgentEventKindAgentStarted,
		d.config.Judge.Role(),
		"Agent started",
		"",
		map[string]any{
			"phase":      d.config.Phase.String(),
			"agent_role": d.config.Judge.Role().String(),
		},
		[]string{"agent", d.config.Phase.String()},
	))
	if rj, ok := d.config.Judge.(RiskJudgeNode); ok {
		input := RiskJudgeInput{
			Ticker:      state.Ticker,
			Rounds:      d.debateRounds(state),
			TradingPlan: state.TradingPlan,
		}
		result, err := rj.JudgeRisk(phaseCtx, input)
		if err != nil {
			return err
		}
		applyRiskJudgeOutput(state, result)
	} else {
		if err := d.config.Judge.Execute(phaseCtx, state); err != nil {
			return err
		}
	}
	output, llmResponse, err := d.pipeline.decisionPayload(state, d.config.Judge, nil)
	if err != nil {
		return err
	}
	if err := d.pipeline.persister.PersistDecision(phaseCtx, state.PipelineRunID, d.config.Judge, nil, output, llmResponse); err != nil {
		return err
	}
	d.pipeline.persistStructuredEvent(phaseCtx, d.pipeline.newStructuredEvent(
		state.PipelineRunID,
		state.StrategyID,
		AgentEventKindAgentCompleted,
		d.config.Judge.Role(),
		"Agent completed",
		"",
		map[string]any{
			"phase":      d.config.Phase.String(),
			"agent_role": d.config.Judge.Role().String(),
		},
		[]string{"agent", d.config.Phase.String()},
	))
	if d.config.Phase == PhaseRiskDebate && state.RiskDebate.FinalSignal != "" {
		d.pipeline.persistStructuredEvent(phaseCtx, d.pipeline.newStructuredEvent(
			state.PipelineRunID,
			state.StrategyID,
			AgentEventKindSignalProduced,
			d.config.Judge.Role(),
			"Signal produced",
			"",
			map[string]any{
				"phase":        d.config.Phase.String(),
				"agent_role":   d.config.Judge.Role().String(),
				"signal_value": state.RiskDebate.FinalSignal,
			},
			[]string{"signal", d.config.Phase.String()},
		))
	}

	return nil
}

// debateRounds returns the current debate rounds from the pipeline state based
// on the configured phase.
func (d *DebateExecutor) debateRounds(state *PipelineState) []DebateRound {
	switch d.config.Phase {
	case PhaseResearchDebate:
		return state.ResearchDebate.Rounds
	case PhaseRiskDebate:
		return state.RiskDebate.Rounds
	default:
		return nil
	}
}

// contextReports returns the context reports for the debaters based on the
// configured phase.
func (d *DebateExecutor) contextReports(state *PipelineState) map[AgentRole]string {
	switch d.config.Phase {
	case PhaseResearchDebate:
		return state.AnalystReports
	case PhaseRiskDebate:
		return map[AgentRole]string{
			AgentRoleTrader: MarshalTradingPlanSafe(state.TradingPlan),
		}
	default:
		return nil
	}
}
