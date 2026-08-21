package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/evidenceprogram"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type stubMilestoneEvidenceSource struct {
	assessment *evidenceprogram.Assessment
	err        error
	gotID      uuid.UUID
}

func (s *stubMilestoneEvidenceSource) GetAssessment(_ context.Context, id uuid.UUID) (*evidenceprogram.Assessment, error) {
	s.gotID = id
	return s.assessment, s.err
}

func testMilestoneAssessment(t *testing.T) *evidenceprogram.Assessment {
	t.Helper()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	value, err := evidenceprogram.AssessShadow(evidenceprogram.ShadowInput{
		StartedAt: start, EndedAt: start.Add(30 * 24 * time.Hour), DailyComplete: true,
		Parents: []evidenceprogram.EvidenceRef{{Kind: "shadow_campaign", ID: uuid.New(), SHA256: strings.Repeat("0", 64)}},
		Candidates: []evidenceprogram.CandidateShadow{
			{Key: "alpha", ObservedDays: 30, ExecutableSamples: 10, SimulatedFills: 8, SlippageKnown: true, SlippageDivergence: "0.01"},
			{Key: "beta", ObservedDays: 30, ExecutableSamples: 9, SimulatedFills: 7, SlippageKnown: true, SlippageDivergence: "-0.01"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestMilestoneEvidenceRouteIsReadOnlyAndReconstructsAssessment(t *testing.T) {
	assessment := testMilestoneAssessment(t)
	source := &stubMilestoneEvidenceSource{assessment: assessment}
	deps := testDeps()
	deps.MilestoneEvidence = source
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/evidence/assessments/"+assessment.ID().String(), nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	got := decodeJSON[milestoneAssessmentResponse](t, rr)
	if got.ID != assessment.ID() || got.SHA256 != assessment.Digest() || got.Campaign != assessment.Campaign() || got.Outcome != assessment.Outcome() {
		t.Fatalf("unexpected response: %+v", got)
	}
	if source.gotID != assessment.ID() || string(got.Canonical) != string(assessment.CanonicalBytes()) {
		t.Fatalf("route did not retain exact assessment identity/canonical bytes")
	}
}

func TestMilestoneEvidenceRouteFailsClosed(t *testing.T) {
	t.Run("not configured", func(t *testing.T) {
		rr := doRequest(t, newTestServer(t), http.MethodGet, "/api/v1/evidence/assessments/"+uuid.NewString(), nil)
		if rr.Code != http.StatusNotImplemented {
			t.Fatalf("status = %d; body: %s", rr.Code, rr.Body.String())
		}
	})
	t.Run("invalid id", func(t *testing.T) {
		deps := testDeps()
		deps.MilestoneEvidence = &stubMilestoneEvidenceSource{}
		rr := doRequest(t, newTestServerWithDeps(t, deps), http.MethodGet, "/api/v1/evidence/assessments/not-a-uuid", nil)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d; body: %s", rr.Code, rr.Body.String())
		}
	})
	for name, want := range map[string]struct {
		err  error
		code int
	}{
		"missing": {repository.ErrNotFound, http.StatusNotFound},
		"corrupt": {errors.New("canonical evidence does not reconstruct"), http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			deps := testDeps()
			deps.MilestoneEvidence = &stubMilestoneEvidenceSource{err: want.err}
			rr := doRequest(t, newTestServerWithDeps(t, deps), http.MethodGet, "/api/v1/evidence/assessments/"+uuid.NewString(), nil)
			if rr.Code != want.code {
				t.Fatalf("status = %d, want %d; body: %s", rr.Code, want.code, rr.Body.String())
			}
		})
	}
}
