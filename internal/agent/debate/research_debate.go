package debate

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
)

// ResearchDebate is an orchestration Node that runs the full research debate
// sequence: N rounds of bull/bear arguments followed by a manager (judge) that
// produces an investment plan.
type ResearchDebate struct {
	bull    agent.Node
	bear    agent.Node
	manager agent.Node
	rounds  int
	logger  *slog.Logger
}

// NewResearchDebate returns a ResearchDebate wired to the given bull, bear, and
// manager nodes. rounds controls how many debate rounds are executed (clamped
// to a minimum of 1). A nil logger is replaced with the default logger.
func NewResearchDebate(bull, bear, manager agent.Node, rounds int, logger *slog.Logger) *ResearchDebate {
	if logger == nil {
		logger = slog.Default()
	}
	if rounds < 1 {
		logger.Warn("debate/research_debate: invalid rounds; clamping to 1",
			slog.Int("configured_rounds", rounds),
		)
		rounds = 1
	}
	return &ResearchDebate{
		bull:    bull,
		bear:    bear,
		manager: manager,
		rounds:  rounds,
		logger:  logger,
	}
}

// Name returns the human-readable name for this orchestration node.
func (rd *ResearchDebate) Name() string { return "research_debate" }

// Role returns the agent role for the orchestration node. It delegates to the
// manager (judge) node because the research debate's final output is the
// investment plan produced by the judge.
func (rd *ResearchDebate) Role() agent.AgentRole { return rd.manager.Role() }

// Phase returns the pipeline phase this node belongs to.
func (rd *ResearchDebate) Phase() agent.Phase { return agent.PhaseResearchDebate }

// Execute runs the full research debate sequence:
//  1. For each round (1..N): append a new DebateRound to the state, then
//     execute the bull node followed by the bear node.
//  2. Execute the manager node to synthesize all rounds into an investment plan.
func (rd *ResearchDebate) Execute(ctx context.Context, state *agent.PipelineState) error {
	for i := 1; i <= rd.rounds; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		rd.logger.Info("debate/research_debate: starting round",
			slog.Int("round", i),
			slog.Int("total_rounds", rd.rounds),
		)

		// Append a fresh round so bull and bear can store contributions.
		state.ResearchDebate.Rounds = append(state.ResearchDebate.Rounds, agent.DebateRound{
			Number:        i,
			Contributions: make(map[agent.AgentRole]string),
		})

		if err := rd.bull.Execute(ctx, state); err != nil {
			return fmt.Errorf("debate/research_debate: bull round %d: %w", i, err)
		}
		if err := rd.bear.Execute(ctx, state); err != nil {
			return fmt.Errorf("debate/research_debate: bear round %d: %w", i, err)
		}
	}

	rd.logger.Info("debate/research_debate: all rounds complete; running manager")

	if err := rd.manager.Execute(ctx, state); err != nil {
		return fmt.Errorf("debate/research_debate: manager: %w", err)
	}

	return nil
}
