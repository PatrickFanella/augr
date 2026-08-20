package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/researchworkflow"
)

type ResearchWorkflowRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ researchworkflow.Store = (*ResearchWorkflowRepo)(nil)

func NewResearchWorkflowRepo(pool *pgxpool.Pool) *ResearchWorkflowRepo {
	return &ResearchWorkflowRepo{pool: pool}
}

type researchHypothesisEnvelope struct {
	Schema      string `json:"schema"`
	State       string `json:"state"`
	WorkflowKey string `json:"workflow_key"`
	Parents     struct {
		ManifestID             string `json:"manifest_id"`
		ManifestSHA256         string `json:"manifest_sha256"`
		RobustnessPolicyID     string `json:"robustness_policy_id"`
		RobustnessPolicySHA256 string `json:"robustness_policy_sha256"`
		RobustnessFamilyID     string `json:"robustness_family_id"`
		RobustnessFamilySHA256 string `json:"robustness_family_sha256"`
		AssessmentID           string `json:"assessment_id"`
		AssessmentSHA256       string `json:"assessment_sha256"`
		SpecID                 string `json:"spec_id"`
		SpecSHA256             string `json:"spec_sha256"`
		VersionID              string `json:"version_id"`
		VersionSHA256          string `json:"version_sha256"`
		ReceiptID              string `json:"receipt_id"`
		ReceiptSHA256          string `json:"receipt_sha256"`
	} `json:"parents"`
	Sources []struct {
		Sequence           int      `json:"sequence"`
		Key                string   `json:"key"`
		URI                string   `json:"uri"`
		Publisher          string   `json:"publisher"`
		Title              string   `json:"title"`
		PublishedAt        string   `json:"published_at"`
		AvailableAt        string   `json:"available_at"`
		ContentSHA256      string   `json:"content_sha256"`
		License            string   `json:"license"`
		ManifestSourceKeys []string `json:"manifest_source_keys"`
	} `json:"sources"`
	Searches []struct {
		Sequence    int    `json:"sequence"`
		Key         string `json:"key"`
		Provider    string `json:"provider"`
		QuerySHA256 string `json:"query_sha256"`
		ExecutedAt  string `json:"executed_at"`
		Results     []struct {
			Sequence  int    `json:"sequence"`
			SourceKey string `json:"source_key"`
			Rank      int    `json:"rank"`
			Selected  bool   `json:"selected"`
		} `json:"results"`
	} `json:"searches"`
	Tests []struct {
		Sequence        int    `json:"sequence"`
		Key             string `json:"key"`
		Type            string `json:"type"`
		ExpectedOutcome string `json:"expected_outcome"`
		AcceptanceRule  string `json:"acceptance_rule"`
		SpecTestKey     string `json:"spec_test_key"`
	} `json:"tests"`
}

type researchCriticEnvelope struct {
	Schema           string `json:"schema"`
	State            string `json:"state"`
	ReviewKey        string `json:"review_key"`
	HypothesisID     string `json:"hypothesis_id"`
	HypothesisSHA256 string `json:"hypothesis_sha256"`
	Recommendation   string `json:"recommendation"`
	Findings         []struct {
		Sequence    int      `json:"sequence"`
		Key         string   `json:"key"`
		Category    string   `json:"category"`
		Severity    string   `json:"severity"`
		Status      string   `json:"status"`
		References  []string `json:"references"`
		Explanation string   `json:"explanation"`
	} `json:"findings"`
	Checks []struct {
		Sequence    int      `json:"sequence"`
		Name        string   `json:"name"`
		State       string   `json:"state"`
		References  []string `json:"references"`
		Explanation string   `json:"explanation"`
	} `json:"checks"`
}

func (r *ResearchWorkflowRepo) RegisterWorkflow(ctx context.Context, hypothesis *researchworkflow.Hypothesis, critic *researchworkflow.Critic, parents researchworkflow.Parents) (*researchworkflow.Hypothesis, *researchworkflow.Critic, error) {
	if r == nil || r.pool == nil || hypothesis == nil || critic == nil || critic.HypothesisID() != hypothesis.ID() {
		return nil, nil, fmt.Errorf("postgres: research workflow is required")
	}
	var h researchHypothesisEnvelope
	var c researchCriticEnvelope
	if err := json.Unmarshal(hypothesis.CanonicalBytes(), &h); err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(critic.CanonicalBytes(), &c); err != nil {
		return nil, nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO research_hypotheses(id,schema_name,state,workflow_key,manifest_id,manifest_sha256,robustness_policy_id,robustness_policy_sha256,robustness_family_id,robustness_family_sha256,assessment_id,assessment_sha256,spec_id,spec_sha256,version_id,version_sha256,receipt_id,receipt_sha256,source_count,search_count,test_count,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,convert_from($23,'UTF8')::jsonb) ON CONFLICT(id) DO NOTHING`, hypothesis.ID(), h.Schema, h.State, h.WorkflowKey, h.Parents.ManifestID, h.Parents.ManifestSHA256, h.Parents.RobustnessPolicyID, h.Parents.RobustnessPolicySHA256, h.Parents.RobustnessFamilyID, h.Parents.RobustnessFamilySHA256, h.Parents.AssessmentID, h.Parents.AssessmentSHA256, h.Parents.SpecID, h.Parents.SpecSHA256, h.Parents.VersionID, h.Parents.VersionSHA256, h.Parents.ReceiptID, h.Parents.ReceiptSHA256, len(h.Sources), len(h.Searches), len(h.Tests), hypothesis.Digest(), hypothesis.CanonicalBytes())
	if err != nil {
		return nil, nil, researchWorkflowWriteError("insert hypothesis", err)
	}
	if err = r.stage("hypothesis"); err != nil {
		return nil, nil, err
	}
	for _, source := range h.Sources {
		raw := mustJSON(source)
		if _, err = tx.Exec(ctx, `INSERT INTO research_hypothesis_sources(hypothesis_id,sequence,source_key,canonical_row) VALUES($1,$2,$3,$4::jsonb) ON CONFLICT(hypothesis_id,sequence) DO NOTHING`, hypothesis.ID(), source.Sequence, source.Key, string(raw)); err != nil {
			return nil, nil, researchWorkflowWriteError("insert source", err)
		}
		for sequence, key := range source.ManifestSourceKeys {
			if _, err = tx.Exec(ctx, `INSERT INTO research_hypothesis_source_manifest_keys(hypothesis_id,source_sequence,sequence,manifest_source_key) VALUES($1,$2,$3,$4) ON CONFLICT(hypothesis_id,source_sequence,sequence) DO NOTHING`, hypothesis.ID(), source.Sequence, sequence, key); err != nil {
				return nil, nil, researchWorkflowWriteError("insert source manifest key", err)
			}
		}
	}
	if err = r.stage("sources"); err != nil {
		return nil, nil, err
	}
	for _, search := range h.Searches {
		if _, err = tx.Exec(ctx, `INSERT INTO research_hypothesis_searches(hypothesis_id,sequence,search_key,canonical_row) VALUES($1,$2,$3,$4::jsonb) ON CONFLICT(hypothesis_id,sequence) DO NOTHING`, hypothesis.ID(), search.Sequence, search.Key, string(mustJSON(search))); err != nil {
			return nil, nil, researchWorkflowWriteError("insert search", err)
		}
		for _, result := range search.Results {
			if _, err = tx.Exec(ctx, `INSERT INTO research_hypothesis_search_results(hypothesis_id,search_sequence,sequence,source_key,rank,selected,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb) ON CONFLICT(hypothesis_id,search_sequence,sequence) DO NOTHING`, hypothesis.ID(), search.Sequence, result.Sequence, result.SourceKey, result.Rank, result.Selected, string(mustJSON(result))); err != nil {
				return nil, nil, researchWorkflowWriteError("insert search result", err)
			}
		}
	}
	if err = r.stage("searches"); err != nil {
		return nil, nil, err
	}
	for _, test := range h.Tests {
		if _, err = tx.Exec(ctx, `INSERT INTO research_hypothesis_tests(hypothesis_id,sequence,test_key,test_type,canonical_row) VALUES($1,$2,$3,$4,$5::jsonb) ON CONFLICT(hypothesis_id,sequence) DO NOTHING`, hypothesis.ID(), test.Sequence, test.Key, test.Type, string(mustJSON(test))); err != nil {
			return nil, nil, researchWorkflowWriteError("insert test", err)
		}
	}
	if err = r.stage("tests"); err != nil {
		return nil, nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO research_critics(id,schema_name,state,review_key,hypothesis_id,hypothesis_sha256,recommendation,finding_count,check_count,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,convert_from($11,'UTF8')::jsonb) ON CONFLICT(id) DO NOTHING`, critic.ID(), c.Schema, c.State, c.ReviewKey, c.HypothesisID, c.HypothesisSHA256, c.Recommendation, len(c.Findings), len(c.Checks), critic.Digest(), critic.CanonicalBytes())
	if err != nil {
		return nil, nil, researchWorkflowWriteError("insert critic", err)
	}
	if err = r.stage("critic"); err != nil {
		return nil, nil, err
	}
	for _, finding := range c.Findings {
		if _, err = tx.Exec(ctx, `INSERT INTO research_critic_findings(critic_id,sequence,finding_key,category,severity,status,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb) ON CONFLICT(critic_id,sequence) DO NOTHING`, critic.ID(), finding.Sequence, finding.Key, finding.Category, finding.Severity, finding.Status, string(mustJSON(finding))); err != nil {
			return nil, nil, researchWorkflowWriteError("insert finding", err)
		}
		for sequence, reference := range finding.References {
			if _, err = tx.Exec(ctx, `INSERT INTO research_critic_finding_references(critic_id,finding_sequence,sequence,reference) VALUES($1,$2,$3,$4) ON CONFLICT(critic_id,finding_sequence,sequence) DO NOTHING`, critic.ID(), finding.Sequence, sequence, reference); err != nil {
				return nil, nil, researchWorkflowWriteError("insert finding reference", err)
			}
		}
	}
	if err = r.stage("findings"); err != nil {
		return nil, nil, err
	}
	for _, check := range c.Checks {
		if _, err = tx.Exec(ctx, `INSERT INTO research_critic_checks(critic_id,sequence,check_name,check_state,canonical_row) VALUES($1,$2,$3,$4,$5::jsonb) ON CONFLICT(critic_id,sequence) DO NOTHING`, critic.ID(), check.Sequence, check.Name, check.State, string(mustJSON(check))); err != nil {
			return nil, nil, researchWorkflowWriteError("insert check", err)
		}
		for sequence, reference := range check.References {
			if _, err = tx.Exec(ctx, `INSERT INTO research_critic_check_references(critic_id,check_sequence,sequence,reference) VALUES($1,$2,$3,$4) ON CONFLICT(critic_id,check_sequence,sequence) DO NOTHING`, critic.ID(), check.Sequence, sequence, reference); err != nil {
				return nil, nil, researchWorkflowWriteError("insert check reference", err)
			}
		}
	}
	if err = r.stage("checks"); err != nil {
		return nil, nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, nil, researchWorkflowWriteError("commit", err)
	}
	loadedHypothesis, loadedCritic, err := r.GetWorkflow(ctx, hypothesis.ID(), critic.ID(), parents)
	if err != nil {
		return nil, nil, err
	}
	if !bytes.Equal(loadedHypothesis.CanonicalBytes(), hypothesis.CanonicalBytes()) || !bytes.Equal(loadedCritic.CanonicalBytes(), critic.CanonicalBytes()) {
		return nil, nil, fmt.Errorf("postgres: research workflow conflict: %w", repository.ErrIdempotencyConflict)
	}
	return loadedHypothesis, loadedCritic, nil
}

func (r *ResearchWorkflowRepo) GetWorkflow(ctx context.Context, hypothesisID, criticID uuid.UUID, parents researchworkflow.Parents) (*researchworkflow.Hypothesis, *researchworkflow.Critic, error) {
	if r == nil || r.pool == nil || hypothesisID == uuid.Nil || criticID == uuid.Nil {
		return nil, nil, fmt.Errorf("postgres: research workflow identity is required")
	}
	var hDigest string
	var hRaw []byte
	if err := r.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM research_hypotheses WHERE id=$1`, hypothesisID).Scan(&hDigest, &hRaw); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, repository.ErrNotFound
	} else if err != nil {
		return nil, nil, err
	}
	hypothesis, err := researchworkflow.HypothesisFromCanonical(hypothesisID, hDigest, hRaw, parents)
	if err != nil {
		return nil, nil, err
	}
	if err = r.verifyHypothesisRows(ctx, hypothesisID, hRaw); err != nil {
		return nil, nil, err
	}
	var cDigest string
	var cRaw []byte
	if err = r.pool.QueryRow(ctx, `SELECT id,sha256,canonical_bytes FROM research_critics WHERE hypothesis_id=$1 AND id=$2`, hypothesisID, criticID).Scan(&criticID, &cDigest, &cRaw); err != nil {
		return nil, nil, err
	}
	critic, err := researchworkflow.CriticFromCanonical(criticID, cDigest, cRaw, hypothesis)
	if err != nil {
		return nil, nil, err
	}
	if err = r.verifyCriticRows(ctx, criticID, cRaw); err != nil {
		return nil, nil, err
	}
	return hypothesis, critic, nil
}

func (r *ResearchWorkflowRepo) verifyHypothesisRows(ctx context.Context, id uuid.UUID, raw []byte) error {
	var envelope struct{ Sources, Searches, Tests []json.RawMessage }
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	for table, expected := range map[string][]json.RawMessage{"research_hypothesis_sources": envelope.Sources, "research_hypothesis_searches": envelope.Searches, "research_hypothesis_tests": envelope.Tests} {
		rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT sequence,canonical_row FROM %s WHERE hypothesis_id=$1 ORDER BY sequence`, table), id)
		if err != nil {
			return err
		}
		stored := []json.RawMessage{}
		for rows.Next() {
			var sequence int
			var row []byte
			if err = rows.Scan(&sequence, &row); err != nil {
				rows.Close()
				return err
			}
			if sequence != len(stored) {
				rows.Close()
				return fmt.Errorf("postgres: research workflow rows do not reconstruct")
			}
			stored = append(stored, row)
		}
		rows.Close()
		if len(stored) != len(expected) {
			return fmt.Errorf("postgres: research workflow rows do not reconstruct")
		}
		for index := range expected {
			if !jsonEqual(stored[index], expected[index]) {
				return fmt.Errorf("postgres: research workflow row does not reconstruct")
			}
		}
	}
	return nil
}

func (r *ResearchWorkflowRepo) verifyCriticRows(ctx context.Context, id uuid.UUID, raw []byte) error {
	var envelope struct{ Findings, Checks []json.RawMessage }
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	for table, expected := range map[string][]json.RawMessage{"research_critic_findings": envelope.Findings, "research_critic_checks": envelope.Checks} {
		rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT sequence,canonical_row FROM %s WHERE critic_id=$1 ORDER BY sequence`, table), id)
		if err != nil {
			return err
		}
		stored := []json.RawMessage{}
		for rows.Next() {
			var sequence int
			var row []byte
			if err = rows.Scan(&sequence, &row); err != nil {
				rows.Close()
				return err
			}
			if sequence != len(stored) {
				rows.Close()
				return fmt.Errorf("postgres: research critic rows do not reconstruct")
			}
			stored = append(stored, row)
		}
		rows.Close()
		if len(stored) != len(expected) {
			return fmt.Errorf("postgres: research critic rows do not reconstruct")
		}
		for index := range expected {
			if !jsonEqual(stored[index], expected[index]) {
				return fmt.Errorf("postgres: research critic row does not reconstruct")
			}
		}
	}
	return nil
}

func (r *ResearchWorkflowRepo) stage(value string) error {
	if r.afterStage != nil {
		return r.afterStage(value)
	}
	return nil
}
func mustJSON(value any) json.RawMessage { raw, _ := json.Marshal(value); return raw }
func researchWorkflowWriteError(action string, err error) error {
	if err != nil && (strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "does not reconstruct") || strings.Contains(err.Error(), "foreign key")) {
		return fmt.Errorf("postgres: research workflow %s conflict: %w", action, repository.ErrIdempotencyConflict)
	}
	return fmt.Errorf("postgres: research workflow %s: %w", action, err)
}
