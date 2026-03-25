package agent

import (
	"context"
	"log/slog"
	"time"
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
			if err := debater.Execute(phaseCtx, state); err != nil {
				return err
			}
			roundNumber := i
			output, llmResponse, err := d.pipeline.decisionPayload(state, debater, &roundNumber)
			if err != nil {
				return err
			}
			if err := d.pipeline.persister.PersistDecision(phaseCtx, state.PipelineRunID, debater, &roundNumber, output, llmResponse); err != nil {
				return err
			}
		}

		// Emit DebateRoundCompleted event.
		if d.pipeline.events != nil {
			event := PipelineEvent{
				Type:          DebateRoundCompleted,
				PipelineRunID: state.PipelineRunID,
				StrategyID:    state.StrategyID,
				Ticker:        state.Ticker,
				Phase:         d.config.Phase,
				Round:         i,
				OccurredAt:    time.Now().UTC(),
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
	if err := d.config.Judge.Execute(phaseCtx, state); err != nil {
		return err
	}
	output, llmResponse, err := d.pipeline.decisionPayload(state, d.config.Judge, nil)
	if err != nil {
		return err
	}
	if err := d.pipeline.persister.PersistDecision(phaseCtx, state.PipelineRunID, d.config.Judge, nil, output, llmResponse); err != nil {
		return err
	}

	return nil
}
