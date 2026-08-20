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

	"github.com/PatrickFanella/get-rich-quick/internal/evidencereview"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type EvidenceReviewRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ evidencereview.Store = (*EvidenceReviewRepo)(nil)

func NewEvidenceReviewRepo(pool *pgxpool.Pool) *EvidenceReviewRepo {
	return &EvidenceReviewRepo{pool: pool}
}

type evidenceCaseEnvelope struct {
	Schema                  string   `json:"schema"`
	State                   string   `json:"state"`
	HypothesisID            string   `json:"hypothesis_id"`
	HypothesisSHA256        string   `json:"hypothesis_sha256"`
	CriticID                string   `json:"critic_id"`
	CriticSHA256            string   `json:"critic_sha256"`
	CriticRecommendation    string   `json:"critic_recommendation"`
	VersionID               string   `json:"version_id"`
	VersionSHA256           string   `json:"version_sha256"`
	PromotionPolicyID       string   `json:"promotion_policy_id"`
	PromotionPolicySHA256   string   `json:"promotion_policy_sha256"`
	PromotionDecisionID     string   `json:"promotion_decision_id"`
	PromotionDecisionSHA256 string   `json:"promotion_decision_sha256"`
	DeploymentID            string   `json:"deployment_id"`
	DeploymentSHA256        string   `json:"deployment_sha256"`
	AssessmentID            string   `json:"assessment_id"`
	AssessmentSHA256        string   `json:"assessment_sha256"`
	EvidenceReferences      []string `json:"evidence_references"`
	AuthoritativeOutcome    string   `json:"authoritative_outcome"`
	AuthoritativeNextState  string   `json:"authoritative_next_state"`
}
type evidenceReviewEnvelope struct {
	Schema     string `json:"schema"`
	State      string `json:"state"`
	CaseID     string `json:"case_id"`
	CaseSHA256 string `json:"case_sha256"`
	Reviewer   struct {
		Key            string `json:"key"`
		Kind           string `json:"kind"`
		IdentitySHA256 string `json:"identity_sha256"`
	} `json:"reviewer"`
	ReviewedAt        string `json:"reviewed_at"`
	PriorReviewID     string `json:"prior_review_id"`
	PriorReviewSHA256 string `json:"prior_review_sha256"`
	Checks            []struct {
		Sequence    int      `json:"sequence"`
		Name        string   `json:"name"`
		Severity    string   `json:"severity"`
		State       string   `json:"state"`
		References  []string `json:"references"`
		Explanation string   `json:"explanation"`
	} `json:"checks"`
	Disposition            string `json:"disposition"`
	AuthoritativeOutcome   string `json:"authoritative_outcome"`
	AuthoritativeNextState string `json:"authoritative_next_state"`
}
type evidenceSummaryEnvelope struct {
	Schema      string `json:"schema"`
	State       string `json:"state"`
	CaseID      string `json:"case_id"`
	CaseSHA256  string `json:"case_sha256"`
	ReviewHeads []struct {
		Sequence     int    `json:"sequence"`
		ReviewID     string `json:"review_id"`
		ReviewSHA256 string `json:"review_sha256"`
		ReviewerKey  string `json:"reviewer_key"`
		ReviewedAt   string `json:"reviewed_at"`
		Disposition  string `json:"disposition"`
	} `json:"review_heads"`
	Checks []struct {
		Sequence     int    `json:"sequence"`
		Name         string `json:"name"`
		PassCount    int    `json:"pass_count"`
		FailCount    int    `json:"fail_count"`
		UnknownCount int    `json:"unknown_count"`
	} `json:"checks"`
	Consensus              string `json:"consensus"`
	EscalationRequired     bool   `json:"escalation_required"`
	AuthoritativeOutcome   string `json:"authoritative_outcome"`
	AuthoritativeNextState string `json:"authoritative_next_state"`
}

func (r *EvidenceReviewRepo) RegisterBundle(ctx context.Context, reviewCase *evidencereview.Case, reviews []*evidencereview.Review, summary *evidencereview.Summary, parents evidencereview.CaseInput) (*evidencereview.Case, []*evidencereview.Review, *evidencereview.Summary, error) {
	if r == nil || r.pool == nil || reviewCase == nil || len(reviews) < 2 || summary == nil {
		return nil, nil, nil, fmt.Errorf("postgres: evidence review bundle is required")
	}
	var c evidenceCaseEnvelope
	var s evidenceSummaryEnvelope
	if err := json.Unmarshal(reviewCase.CanonicalBytes(), &c); err != nil {
		return nil, nil, nil, err
	}
	if err := json.Unmarshal(summary.CanonicalBytes(), &s); err != nil {
		return nil, nil, nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO evidence_review_cases(id,schema_name,state,hypothesis_id,hypothesis_sha256,critic_id,critic_sha256,critic_recommendation,version_id,version_sha256,promotion_policy_id,promotion_policy_sha256,promotion_decision_id,promotion_decision_sha256,deployment_id,deployment_sha256,assessment_id,assessment_sha256,authoritative_outcome,authoritative_next_state,reference_count,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,convert_from($23,'UTF8')::jsonb) ON CONFLICT(id) DO NOTHING`, reviewCase.ID(), c.Schema, c.State, c.HypothesisID, c.HypothesisSHA256, c.CriticID, c.CriticSHA256, c.CriticRecommendation, c.VersionID, c.VersionSHA256, c.PromotionPolicyID, c.PromotionPolicySHA256, c.PromotionDecisionID, c.PromotionDecisionSHA256, c.DeploymentID, c.DeploymentSHA256, c.AssessmentID, c.AssessmentSHA256, c.AuthoritativeOutcome, c.AuthoritativeNextState, len(c.EvidenceReferences), reviewCase.Digest(), reviewCase.CanonicalBytes())
	if err != nil {
		return nil, nil, nil, evidenceWriteError("insert case", err)
	}
	if err = r.stage("case"); err != nil {
		return nil, nil, nil, err
	}
	for sequence, reference := range c.EvidenceReferences {
		if _, err = tx.Exec(ctx, `INSERT INTO evidence_review_case_references(case_id,sequence,reference) VALUES($1,$2,$3) ON CONFLICT(case_id,sequence) DO NOTHING`, reviewCase.ID(), sequence, reference); err != nil {
			return nil, nil, nil, evidenceWriteError("insert case reference", err)
		}
	}
	if err = r.stage("case_references"); err != nil {
		return nil, nil, nil, err
	}
	for _, review := range reviews {
		var v evidenceReviewEnvelope
		if review == nil || json.Unmarshal(review.CanonicalBytes(), &v) != nil {
			return nil, nil, nil, fmt.Errorf("postgres: evidence review is invalid")
		}
		var prior any
		if v.PriorReviewID != "" {
			prior = v.PriorReviewID
		}
		_, err = tx.Exec(ctx, `INSERT INTO evidence_reviews(id,schema_name,state,case_id,case_sha256,reviewer_key,reviewer_kind,reviewer_identity_sha256,reviewed_at,prior_review_id,prior_review_sha256,check_count,disposition,authoritative_outcome,authoritative_next_state,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,convert_from($17,'UTF8')::jsonb) ON CONFLICT(id) DO NOTHING`, review.ID(), v.Schema, v.State, v.CaseID, v.CaseSHA256, v.Reviewer.Key, v.Reviewer.Kind, v.Reviewer.IdentitySHA256, v.ReviewedAt, prior, v.PriorReviewSHA256, len(v.Checks), v.Disposition, v.AuthoritativeOutcome, v.AuthoritativeNextState, review.Digest(), review.CanonicalBytes())
		if err != nil {
			return nil, nil, nil, evidenceWriteError("insert review", err)
		}
		if err = r.stage("review"); err != nil {
			return nil, nil, nil, err
		}
		for _, check := range v.Checks {
			raw, _ := json.Marshal(check)
			if _, err = tx.Exec(ctx, `INSERT INTO evidence_review_checks(review_id,sequence,check_name,severity,check_state,canonical_row) VALUES($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT(review_id,sequence) DO NOTHING`, review.ID(), check.Sequence, check.Name, check.Severity, check.State, string(raw)); err != nil {
				return nil, nil, nil, evidenceWriteError("insert check", err)
			}
			if err = r.stage("check"); err != nil {
				return nil, nil, nil, err
			}
			for sequence, reference := range check.References {
				if _, err = tx.Exec(ctx, `INSERT INTO evidence_review_check_references(review_id,check_sequence,sequence,reference) VALUES($1,$2,$3,$4) ON CONFLICT(review_id,check_sequence,sequence) DO NOTHING`, review.ID(), check.Sequence, sequence, reference); err != nil {
					return nil, nil, nil, evidenceWriteError("insert check reference", err)
				}
				if err = r.stage("check_reference"); err != nil {
					return nil, nil, nil, err
				}
			}
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO evidence_review_summaries(id,schema_name,state,case_id,case_sha256,review_head_count,check_count,consensus,escalation_required,authoritative_outcome,authoritative_next_state,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,convert_from($13,'UTF8')::jsonb) ON CONFLICT(id) DO NOTHING`, summary.ID(), s.Schema, s.State, s.CaseID, s.CaseSHA256, len(s.ReviewHeads), len(s.Checks), s.Consensus, s.EscalationRequired, s.AuthoritativeOutcome, s.AuthoritativeNextState, summary.Digest(), summary.CanonicalBytes())
	if err != nil {
		return nil, nil, nil, evidenceWriteError("insert summary", err)
	}
	if err = r.stage("summary"); err != nil {
		return nil, nil, nil, err
	}
	for _, head := range s.ReviewHeads {
		raw, _ := json.Marshal(head)
		if _, err = tx.Exec(ctx, `INSERT INTO evidence_review_summary_heads(summary_id,sequence,review_id,review_sha256,reviewer_key,canonical_row) VALUES($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT(summary_id,sequence) DO NOTHING`, summary.ID(), head.Sequence, head.ReviewID, head.ReviewSHA256, head.ReviewerKey, string(raw)); err != nil {
			return nil, nil, nil, evidenceWriteError("insert summary head", err)
		}
		if err = r.stage("summary_head"); err != nil {
			return nil, nil, nil, err
		}
	}
	for _, check := range s.Checks {
		raw, _ := json.Marshal(check)
		if _, err = tx.Exec(ctx, `INSERT INTO evidence_review_summary_checks(summary_id,sequence,check_name,canonical_row) VALUES($1,$2,$3,$4::jsonb) ON CONFLICT(summary_id,sequence) DO NOTHING`, summary.ID(), check.Sequence, check.Name, string(raw)); err != nil {
			return nil, nil, nil, evidenceWriteError("insert summary check", err)
		}
		if err = r.stage("summary_check"); err != nil {
			return nil, nil, nil, err
		}
	}
	if err = r.stage("summary_rows"); err != nil {
		return nil, nil, nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, nil, nil, evidenceWriteError("commit", err)
	}
	loadedCase, loadedReviews, loadedSummary, err := r.GetBundle(ctx, reviewCase.ID(), summary.ID(), parents)
	if err != nil {
		return nil, nil, nil, err
	}
	if !bytes.Equal(loadedCase.CanonicalBytes(), reviewCase.CanonicalBytes()) || !bytes.Equal(loadedSummary.CanonicalBytes(), summary.CanonicalBytes()) {
		return nil, nil, nil, fmt.Errorf("postgres: evidence review conflict: %w", repository.ErrIdempotencyConflict)
	}
	return loadedCase, loadedReviews, loadedSummary, nil
}

func (r *EvidenceReviewRepo) GetBundle(ctx context.Context, caseID, summaryID uuid.UUID, parents evidencereview.CaseInput) (*evidencereview.Case, []*evidencereview.Review, *evidencereview.Summary, error) {
	if r == nil || r.pool == nil || caseID == uuid.Nil || summaryID == uuid.Nil {
		return nil, nil, nil, fmt.Errorf("postgres: evidence review identity is required")
	}
	var caseDigest string
	var caseRaw []byte
	if err := r.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM evidence_review_cases WHERE id=$1`, caseID).Scan(&caseDigest, &caseRaw); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, repository.ErrNotFound
	} else if err != nil {
		return nil, nil, nil, err
	}
	reviewCase, err := evidencereview.CaseFromCanonical(caseID, caseDigest, caseRaw, parents)
	if err != nil {
		return nil, nil, nil, err
	}
	if err = r.verifyRows(ctx, "evidence_review_case_references", "case_id", caseID, caseRaw, "evidence_references", false); err != nil {
		return nil, nil, nil, err
	}
	var summaryDigest string
	var summaryRaw []byte
	if err = r.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM evidence_review_summaries WHERE id=$1 AND case_id=$2`, summaryID, caseID).Scan(&summaryDigest, &summaryRaw); err != nil {
		return nil, nil, nil, err
	}
	var envelope evidenceSummaryEnvelope
	if err = json.Unmarshal(summaryRaw, &envelope); err != nil {
		return nil, nil, nil, err
	}
	reviews := make([]*evidencereview.Review, 0, len(envelope.ReviewHeads))
	cache := map[uuid.UUID]*evidencereview.Review{}
	for _, head := range envelope.ReviewHeads {
		id, parseErr := uuid.Parse(head.ReviewID)
		if parseErr != nil {
			return nil, nil, nil, parseErr
		}
		review, loadErr := r.loadReview(ctx, id, reviewCase, cache)
		if loadErr != nil {
			return nil, nil, nil, loadErr
		}
		reviews = append(reviews, review)
	}
	summary, err := evidencereview.SummaryFromCanonical(summaryID, summaryDigest, summaryRaw, evidencereview.SummaryInput{Case: reviewCase, ReviewHeads: reviews})
	if err != nil {
		return nil, nil, nil, err
	}
	if err = r.verifyCanonicalRows(ctx, "evidence_review_summary_heads", "summary_id", summaryID, envelope.ReviewHeads); err != nil {
		return nil, nil, nil, err
	}
	if err = r.verifyCanonicalRows(ctx, "evidence_review_summary_checks", "summary_id", summaryID, envelope.Checks); err != nil {
		return nil, nil, nil, err
	}
	return reviewCase, reviews, summary, nil
}

func (r *EvidenceReviewRepo) loadReview(ctx context.Context, id uuid.UUID, reviewCase *evidencereview.Case, cache map[uuid.UUID]*evidencereview.Review) (*evidencereview.Review, error) {
	if value := cache[id]; value != nil {
		return value, nil
	}
	var digest string
	var raw []byte
	var priorID *uuid.UUID
	if err := r.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes,prior_review_id FROM evidence_reviews WHERE id=$1`, id).Scan(&digest, &raw, &priorID); err != nil {
		return nil, err
	}
	var prior *evidencereview.Review
	var err error
	if priorID != nil {
		prior, err = r.loadReview(ctx, *priorID, reviewCase, cache)
		if err != nil {
			return nil, err
		}
	}
	review, err := evidencereview.ReviewFromCanonical(id, digest, raw, reviewCase, prior)
	if err != nil {
		return nil, err
	}
	var envelope evidenceReviewEnvelope
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if err = r.verifyCanonicalRows(ctx, "evidence_review_checks", "review_id", id, envelope.Checks); err != nil {
		return nil, err
	}
	for _, check := range envelope.Checks {
		if err = r.verifyReferenceRows(ctx, id, check.Sequence, check.References); err != nil {
			return nil, err
		}
	}
	cache[id] = review
	return review, nil
}

func (r *EvidenceReviewRepo) verifyCanonicalRows(ctx context.Context, table, column string, id uuid.UUID, expected any) error {
	raw, _ := json.Marshal(expected)
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return err
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT sequence,canonical_row FROM %s WHERE %s=$1 ORDER BY sequence`, table, column), id)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var sequence int
		var stored []byte
		if err = rows.Scan(&sequence, &stored); err != nil {
			return err
		}
		if sequence != index || index >= len(list) || !jsonEqual(stored, list[index]) {
			return fmt.Errorf("postgres: evidence review rows do not reconstruct")
		}
		index++
	}
	if index != len(list) {
		return fmt.Errorf("postgres: evidence review rows do not reconstruct")
	}
	return nil
}

func (r *EvidenceReviewRepo) verifyReferenceRows(ctx context.Context, reviewID uuid.UUID, checkSequence int, expected []string) error {
	rows, err := r.pool.Query(ctx, `SELECT sequence,reference FROM evidence_review_check_references WHERE review_id=$1 AND check_sequence=$2 ORDER BY sequence`, reviewID, checkSequence)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var sequence int
		var value string
		if err = rows.Scan(&sequence, &value); err != nil {
			return err
		}
		if sequence != index || index >= len(expected) || value != expected[index] {
			return fmt.Errorf("postgres: evidence review references do not reconstruct")
		}
		index++
	}
	if index != len(expected) {
		return fmt.Errorf("postgres: evidence review references do not reconstruct")
	}
	return nil
}

func (r *EvidenceReviewRepo) verifyRows(ctx context.Context, table, column string, id uuid.UUID, parent []byte, arrayKey string, _ bool) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(parent, &object); err != nil {
		return err
	}
	var expected []string
	if err := json.Unmarshal(object[arrayKey], &expected); err != nil {
		return err
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT sequence,reference FROM %s WHERE %s=$1 ORDER BY sequence`, table, column), id)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var sequence int
		var value string
		if err = rows.Scan(&sequence, &value); err != nil {
			return err
		}
		if sequence != index || index >= len(expected) || value != expected[index] {
			return fmt.Errorf("postgres: evidence review case references do not reconstruct")
		}
		index++
	}
	if index != len(expected) {
		return fmt.Errorf("postgres: evidence review case references do not reconstruct")
	}
	return nil
}

func (r *EvidenceReviewRepo) stage(v string) error {
	if r.afterStage != nil {
		return r.afterStage(v)
	}
	return nil
}

func evidenceWriteError(action string, err error) error {
	if err != nil && (strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "does not reconstruct") || strings.Contains(err.Error(), "foreign key")) {
		return fmt.Errorf("postgres: evidence review %s conflict: %w", action, repository.ErrIdempotencyConflict)
	}
	return fmt.Errorf("postgres: evidence review %s: %w", action, err)
}
