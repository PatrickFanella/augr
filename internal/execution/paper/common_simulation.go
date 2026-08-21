package paper

import (
	"errors"
	"fmt"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
)

// ErrCommonSimulationPaperBoundary identifies an ADR-018 account-mode or
// account/aggregate mismatch rejected before canonical venue evaluation.
var ErrCommonSimulationPaperBoundary = errors.New("paper common simulation boundary violation")

// CommonSimulation is the internal-paper path into the immutable common
// simulation venue. Its only additional behavior is fail-closed ADR-018
// account isolation.
type CommonSimulation struct {
	venue *simulation.Venue
}

// NewCommonSimulation validates the exact policy through the common venue.
func NewCommonSimulation(policy *simulation.Policy) (*CommonSimulation, error) {
	venue, err := simulation.NewVenue(policy)
	if err != nil {
		return nil, fmt.Errorf("construct paper common simulation: %w", err)
	}
	return &CommonSimulation{venue: venue}, nil
}

// PolicyVersion returns the content-addressed policy version used by Evaluate.
func (adapter *CommonSimulation) PolicyVersion() string {
	if adapter == nil || adapter.venue == nil {
		return ""
	}
	return adapter.venue.PolicyVersion()
}

// PolicyDigest returns the exact policy digest used by Evaluate.
func (adapter *CommonSimulation) PolicyDigest() string {
	if adapter == nil || adapter.venue == nil {
		return ""
	}
	return adapter.venue.PolicyDigest()
}

// Evaluate preserves the canonical request and result while enforcing that
// internal paper uses one matching scored or stress account boundary.
func (adapter *CommonSimulation) Evaluate(request simulation.EvaluationRequest) (*simulation.Result, error) {
	if adapter == nil || adapter.venue == nil {
		return nil, fmt.Errorf("paper common simulation is not initialized")
	}
	if err := validateCommonSimulationPaperRequest(request); err != nil {
		return nil, err
	}
	return adapter.venue.Evaluate(request)
}

func validateCommonSimulationPaperRequest(request simulation.EvaluationRequest) error {
	if err := request.Account.Validate(); err != nil {
		return fmt.Errorf("%w: invalid account: %v", ErrCommonSimulationPaperBoundary, err)
	}
	if request.Account.Environment != domain.AccountEnvironmentPaperScored &&
		request.Account.Environment != domain.AccountEnvironmentPaperStress {
		return fmt.Errorf("%w: environment %q is not internal paper", ErrCommonSimulationPaperBoundary, request.Account.Environment)
	}
	if request.Aggregate == nil || request.Aggregate.Order == nil ||
		request.Aggregate.Intent.AccountID != request.Account.ID ||
		request.Aggregate.Intent.Environment != request.Account.Environment ||
		request.Aggregate.Order.AccountID != request.Account.ID {
		return fmt.Errorf("%w: account and aggregate do not match", ErrCommonSimulationPaperBoundary)
	}
	if request.Account.Environment == domain.AccountEnvironmentPaperStress &&
		(request.Account.EvidenceClass != domain.PaperEvidenceClassSynthetic || request.Account.PromotionEligible()) {
		return fmt.Errorf("%w: stress evidence classification is invalid", ErrCommonSimulationPaperBoundary)
	}
	return nil
}
