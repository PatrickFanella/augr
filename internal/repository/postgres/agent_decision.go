package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// AgentDecisionRepo implements repository.AgentDecisionRepository using PostgreSQL.
type AgentDecisionRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that AgentDecisionRepo satisfies AgentDecisionRepository.
var _ repository.AgentDecisionRepository = (*AgentDecisionRepo)(nil)

// NewAgentDecisionRepo returns an AgentDecisionRepo backed by the given connection
// pool.
func NewAgentDecisionRepo(pool *pgxpool.Pool) *AgentDecisionRepo {
	return &AgentDecisionRepo{pool: pool}
}

// Create inserts a new agent decision and populates the generated ID and
// CreatedAt on the provided struct.
func (r *AgentDecisionRepo) Create(ctx context.Context, decision *domain.AgentDecision) error {
	outputStructured, err := marshalOutputStructured(decision.OutputStructured)
	if err != nil {
		return err
	}

	row := r.pool.QueryRow(ctx,
		`INSERT INTO agent_decisions (
			pipeline_run_id, agent_role, phase, round_number, input_summary,
			output_text, output_structured, llm_provider, llm_model,
			prompt_tokens, completion_tokens, latency_ms
		)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 RETURNING id, created_at`,
		decision.PipelineRunID,
		decision.AgentRole,
		decision.Phase,
		decision.RoundNumber,
		decision.InputSummary,
		decision.OutputText,
		outputStructured,
		decision.LLMProvider,
		decision.LLMModel,
		decision.PromptTokens,
		decision.CompletionTokens,
		decision.LatencyMS,
	)

	if err := row.Scan(&decision.ID, &decision.CreatedAt); err != nil {
		return fmt.Errorf("postgres: create agent decision: %w", err)
	}

	return nil
}

// GetByRun returns agent decisions for the given pipeline run, with optional
// filtering and pagination. Results are ordered by phase, round number, then
// creation time to satisfy the audit-trail ordering requirement.
func (r *AgentDecisionRepo) GetByRun(ctx context.Context, runID uuid.UUID, filter repository.AgentDecisionFilter, limit, offset int) ([]domain.AgentDecision, error) {
	query, args := buildGetByRunQuery(runID, filter, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: get agent decisions by run: %w", err)
	}
	defer rows.Close()

	var decisions []domain.AgentDecision
	for rows.Next() {
		d, err := scanAgentDecision(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: get agent decisions by run scan: %w", err)
		}
		decisions = append(decisions, *d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: get agent decisions by run rows: %w", err)
	}

	return decisions, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// scanAgentDecision scans a single row (pgx.Row or pgx.Rows) into an
// AgentDecision. Nullable columns are scanned via pointer intermediates and
// converted to the Go zero value when NULL.
func scanAgentDecision(sc scanner) (*domain.AgentDecision, error) {
	var (
		d                    domain.AgentDecision
		inputSummary         *string
		outputStructuredJSON []byte
		llmProvider          *string
		llmModel             *string
		promptTokens         *int
		completionTokens     *int
		latencyMS            *int
	)

	err := sc.Scan(
		&d.ID,
		&d.PipelineRunID,
		&d.AgentRole,
		&d.Phase,
		&d.RoundNumber,
		&inputSummary,
		&d.OutputText,
		&outputStructuredJSON,
		&llmProvider,
		&llmModel,
		&promptTokens,
		&completionTokens,
		&latencyMS,
		&d.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	if inputSummary != nil {
		d.InputSummary = *inputSummary
	}
	if outputStructuredJSON != nil {
		d.OutputStructured = json.RawMessage(outputStructuredJSON)
	}
	if llmProvider != nil {
		d.LLMProvider = *llmProvider
	}
	if llmModel != nil {
		d.LLMModel = *llmModel
	}
	if promptTokens != nil {
		d.PromptTokens = *promptTokens
	}
	if completionTokens != nil {
		d.CompletionTokens = *completionTokens
	}
	if latencyMS != nil {
		d.LatencyMS = *latencyMS
	}

	return &d, nil
}

// buildGetByRunQuery constructs the SELECT query and arguments for GetByRun
// with dynamic WHERE conditions. All values are parameterized. runID is always
// included as a condition; filter fields narrow the result further.
func buildGetByRunQuery(runID uuid.UUID, filter repository.AgentDecisionFilter, limit, offset int) (string, []any) {
	var (
		conditions []string
		args       []any
		argIdx     int
	)

	nextArg := func(v any) string {
		argIdx++
		args = append(args, v)
		return fmt.Sprintf("$%d", argIdx)
	}

	conditions = append(conditions, "pipeline_run_id = "+nextArg(runID))

	if filter.AgentRole != "" {
		conditions = append(conditions, "agent_role = "+nextArg(filter.AgentRole))
	}

	if filter.Phase != "" {
		conditions = append(conditions, "phase = "+nextArg(filter.Phase))
	}

	if filter.RoundNumber != nil {
		conditions = append(conditions, "round_number = "+nextArg(*filter.RoundNumber))
	}

	base := `SELECT id, pipeline_run_id, agent_role, phase, round_number, input_summary,
		 output_text, output_structured, llm_provider, llm_model,
		 prompt_tokens, completion_tokens, latency_ms, created_at
		 FROM agent_decisions`

	base += " WHERE " + strings.Join(conditions, " AND ")
	base += " ORDER BY phase, round_number NULLS LAST, created_at, id"
	base += fmt.Sprintf(" LIMIT %s OFFSET %s", nextArg(limit), nextArg(offset))

	return base, args
}

// marshalOutputStructured ensures the output_structured JSONB value is valid
// JSON. A nil or empty value is stored as SQL NULL.
func marshalOutputStructured(data json.RawMessage) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}

	if !json.Valid(data) {
		return nil, fmt.Errorf("postgres: agent decision output_structured is not valid JSON")
	}

	return data, nil
}
