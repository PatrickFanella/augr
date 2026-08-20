package capital

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

const (
	matrixSchema        = "capital-replay-matrix-v1"
	matrixScoredSchema  = "capital-replay-matrix-scored-v1"
	matrixAccountDomain = "capital-matrix-account"
)

// MatrixSpec contains shared replay facts that may not drift across tiers.
type MatrixSpec struct {
	ScenarioID              string
	MarketInputDigest       string
	SimulationPolicyVersion string
	Seed                    int64
	AsOf                    time.Time
}

// ScenarioContext is the immutable per-tier identity supplied to a scenario
// factory. The factory may size a proposal from Tier but cannot replace shared
// replay facts.
type ScenarioContext struct {
	Spec     MatrixSpec
	Account  domain.Account
	Binding  Binding
	Policy   *Policy
	Tier     decimal.Decimal
	Ordinal  int
	IsStress bool
}

// Scenario contains tier-specific projection/proposal material plus echoed
// shared facts that RunMatrix verifies before assessment.
type Scenario struct {
	Projection              *ledger.PortfolioProjection
	Instruments             []instrument.Instrument
	ProposedInstrument      instrument.Instrument
	Direction               ExposureDirection
	ProposedNotional        decimal.Decimal
	MarketInputDigest       string
	SimulationPolicyVersion string
	Seed                    int64
}

type ScenarioFactory func(ScenarioContext) (Scenario, error)

// MatrixOutcome retains a rejection as a valid per-tier result.
type MatrixOutcome struct {
	Ordinal     int
	Tier        decimal.Decimal
	Profile     domain.MarginProfile
	Environment domain.AccountEnvironment
	Account     domain.Account
	Binding     Binding
	State       *State
	Assessment  *Assessment
}

// MatrixResult contains six scored outcomes and one separately classified
// stress outcome in fixed order.
type MatrixResult struct {
	Spec           MatrixSpec
	PolicyVersion  string
	ScoredProfile  domain.MarginProfile
	Outcomes       []*MatrixOutcome
	canonicalBytes json.RawMessage
	hash           string
	scoredHash     string
}

type canonicalMatrix struct {
	Schema                  string                   `json:"schema"`
	ScenarioID              string                   `json:"scenario_id"`
	MarketInputDigest       string                   `json:"market_input_digest"`
	SimulationPolicyVersion string                   `json:"simulation_policy_version"`
	CapitalPolicyVersion    string                   `json:"capital_policy_version"`
	ScoredProfile           string                   `json:"scored_profile"`
	Seed                    int64                    `json:"seed"`
	AsOf                    string                   `json:"as_of"`
	Outcomes                []canonicalMatrixOutcome `json:"outcomes"`
}

type canonicalMatrixOutcome struct {
	Ordinal          int    `json:"ordinal"`
	Tier             string `json:"tier"`
	Profile          string `json:"profile"`
	Environment      string `json:"environment"`
	EvidenceClass    string `json:"evidence_class"`
	StorageNamespace string `json:"storage_namespace"`
	AccountID        string `json:"account_id"`
	BindingID        string `json:"binding_id"`
	StateHash        string `json:"state_hash"`
	AssessmentHash   string `json:"assessment_hash"`
	Decision         string `json:"decision"`
	Reason           string `json:"reason"`
}

// RunMatrix evaluates the same shared replay facts at every reviewed scored
// tier and once under isolated stress/unlimited semantics.
func RunMatrix(
	spec MatrixSpec,
	policy *Policy,
	scoredProfile domain.MarginProfile,
	factory ScenarioFactory,
) (*MatrixResult, error) {
	if err := validateMatrixSpec(spec); err != nil {
		return nil, fmt.Errorf("run capital matrix: %w", err)
	}
	if policy == nil || factory == nil {
		return nil, fmt.Errorf("run capital matrix: policy and scenario factory are required")
	}
	profile, ok := policy.Profile(scoredProfile)
	if !ok || profile.Unlimited || scoredProfile == domain.MarginProfileStressUnlimited {
		return nil, fmt.Errorf("run capital matrix: scored profile must be finite and reviewed")
	}

	result := &MatrixResult{Spec: spec, PolicyVersion: policy.Version(), ScoredProfile: scoredProfile}
	tiers := policy.Tiers()
	var proposedInstrumentID uuid.UUID
	for ordinal := 0; ordinal <= len(tiers); ordinal++ {
		isStress := ordinal == len(tiers)
		tier := tiers[len(tiers)-1]
		selectedProfile := scoredProfile
		environment := domain.AccountEnvironmentPaperScored
		multiplier := profile.MaximumGross
		if isStress {
			selectedProfile = domain.MarginProfileStressUnlimited
			environment = domain.AccountEnvironmentPaperStress
			multiplier = decimal.Zero
		} else {
			tier = tiers[ordinal]
		}
		account, err := matrixAccount(spec, policy, tier, selectedProfile, environment, multiplier)
		if err != nil {
			return nil, fmt.Errorf("run capital matrix ordinal %d account: %w", ordinal, err)
		}
		binding, err := NewBinding(*account, policy, tier, selectedProfile, spec.AsOf)
		if err != nil {
			return nil, fmt.Errorf("run capital matrix ordinal %d binding: %w", ordinal, err)
		}
		context := ScenarioContext{
			Spec: spec, Account: *account, Binding: *binding, Policy: policy,
			Tier: tier, Ordinal: ordinal, IsStress: isStress,
		}
		scenario, err := factory(context)
		if err != nil {
			return nil, fmt.Errorf("run capital matrix ordinal %d scenario: %w", ordinal, err)
		}
		if scenario.Projection == nil || !scenario.Projection.AsOf.Equal(spec.AsOf) ||
			scenario.MarketInputDigest != spec.MarketInputDigest ||
			scenario.SimulationPolicyVersion != spec.SimulationPolicyVersion || scenario.Seed != spec.Seed {
			return nil, fmt.Errorf("run capital matrix ordinal %d changed shared replay facts", ordinal)
		}
		if ordinal == 0 {
			proposedInstrumentID = scenario.ProposedInstrument.ID
		} else if proposedInstrumentID == uuid.Nil || scenario.ProposedInstrument.ID != proposedInstrumentID {
			return nil, fmt.Errorf("run capital matrix ordinal %d changed proposed instrument identity", ordinal)
		}
		state, err := StateFromProjection(*account, *binding, policy, scenario.Projection, scenario.Instruments)
		if err != nil {
			return nil, fmt.Errorf("run capital matrix ordinal %d state: %w", ordinal, err)
		}
		assessment, err := Assess(AssessmentInput{
			Account: *account, Binding: *binding, Policy: policy, State: state,
			Instrument: scenario.ProposedInstrument, Currency: account.BaseCurrency,
			ScenarioID: spec.ScenarioID, Direction: scenario.Direction, ProposedNotional: scenario.ProposedNotional,
		})
		if err != nil {
			return nil, fmt.Errorf("run capital matrix ordinal %d assessment: %w", ordinal, err)
		}
		result.Outcomes = append(result.Outcomes, &MatrixOutcome{
			Ordinal: ordinal, Tier: tier, Profile: selectedProfile, Environment: environment,
			Account: *account, Binding: *binding, State: state, Assessment: assessment,
		})
	}
	if err := result.seal(); err != nil {
		return nil, fmt.Errorf("run capital matrix: %w", err)
	}
	return result, nil
}

func matrixAccount(
	spec MatrixSpec,
	policy *Policy,
	tier decimal.Decimal,
	profile domain.MarginProfile,
	environment domain.AccountEnvironment,
	multiplier decimal.Decimal,
) (*domain.Account, error) {
	namespaceDigest := sha256.Sum256([]byte(strings.Join([]string{
		spec.ScenarioID, policy.Version(), string(profile), tier.String(), string(environment),
	}, "\x00")))
	namespaceSuffix := hex.EncodeToString(namespaceDigest[:])[:24]
	account, err := domain.NewAccount(domain.AccountInput{
		Name:        "capital matrix " + tier.String() + " " + string(profile),
		Environment: environment, Venue: "internal", BaseCurrency: policy.Currency(),
		StorageNamespace: string(environment) + "/capital-matrix/" + namespaceSuffix,
		StartingCapital:  tier, BuyingPowerMultiplier: multiplier, MarginProfile: profile,
		CreatedBy: "capital-matrix-v1", CreationMetadata: json.RawMessage(`{"source":"capital-matrix-v1"}`),
		CreatedAt: spec.AsOf,
	})
	if err != nil {
		return nil, err
	}
	account.ID = economicid.DeterministicUUID(
		matrixAccountDomain, spec.ScenarioID, policy.Version(), string(environment), string(profile), tier.String(),
	)
	if err := account.Validate(); err != nil {
		return nil, err
	}
	return account, nil
}

func validateMatrixSpec(spec MatrixSpec) error {
	if spec.ScenarioID == "" || spec.ScenarioID != strings.TrimSpace(spec.ScenarioID) || len(spec.ScenarioID) > 256 {
		return fmt.Errorf("canonical scenario ID is required")
	}
	if len(spec.MarketInputDigest) != 64 || strings.ToLower(spec.MarketInputDigest) != spec.MarketInputDigest ||
		!isLowerHex(spec.MarketInputDigest) {
		return fmt.Errorf("market input digest must be lowercase SHA-256")
	}
	if spec.SimulationPolicyVersion == "" || spec.SimulationPolicyVersion != strings.TrimSpace(spec.SimulationPolicyVersion) ||
		len(spec.SimulationPolicyVersion) > 256 {
		return fmt.Errorf("simulation policy version is required")
	}
	if spec.Seed < 0 {
		return fmt.Errorf("matrix seed cannot be negative")
	}
	if spec.AsOf.IsZero() || spec.AsOf.Location() != time.UTC || !spec.AsOf.Equal(spec.AsOf.Truncate(time.Microsecond)) {
		return fmt.Errorf("matrix as-of must use UTC microsecond precision")
	}
	return nil
}

func isLowerHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func (result *MatrixResult) seal() error {
	if result == nil || len(result.Outcomes) != 7 {
		return fmt.Errorf("capital matrix requires seven outcomes")
	}
	all := result.canonical(matrixSchema, result.Outcomes)
	encoded, err := json.Marshal(all)
	if err != nil {
		return err
	}
	digestBytes := sha256.Sum256(encoded)
	result.canonicalBytes = encoded
	result.hash = hex.EncodeToString(digestBytes[:])
	scoredEncoded, err := json.Marshal(result.canonical(matrixScoredSchema, result.Outcomes[:6]))
	if err != nil {
		return err
	}
	scoredDigest := sha256.Sum256(scoredEncoded)
	result.scoredHash = hex.EncodeToString(scoredDigest[:])
	return nil
}

func (result *MatrixResult) canonical(schema string, outcomes []*MatrixOutcome) canonicalMatrix {
	value := canonicalMatrix{
		Schema: schema, ScenarioID: result.Spec.ScenarioID, MarketInputDigest: result.Spec.MarketInputDigest,
		SimulationPolicyVersion: result.Spec.SimulationPolicyVersion, CapitalPolicyVersion: result.PolicyVersion,
		ScoredProfile: string(result.ScoredProfile), Seed: result.Spec.Seed, AsOf: formatCapitalTime(result.Spec.AsOf),
		Outcomes: make([]canonicalMatrixOutcome, 0, len(outcomes)),
	}
	for _, outcome := range outcomes {
		value.Outcomes = append(value.Outcomes, canonicalMatrixOutcome{
			Ordinal: outcome.Ordinal, Tier: outcome.Tier.String(), Profile: string(outcome.Profile),
			Environment: string(outcome.Environment), EvidenceClass: outcome.Account.EvidenceClass,
			StorageNamespace: outcome.Account.StorageNamespace, AccountID: outcome.Account.ID.String(),
			BindingID: outcome.Binding.ID.String(), StateHash: outcome.State.Hash(),
			AssessmentHash: outcome.Assessment.Hash(), Decision: string(outcome.Assessment.Decision),
			Reason: string(outcome.Assessment.Reason),
		})
	}
	return value
}

func (result *MatrixResult) ScoredOutcomes() []*MatrixOutcome {
	if result == nil || len(result.Outcomes) < 6 {
		return nil
	}
	return append([]*MatrixOutcome(nil), result.Outcomes[:6]...)
}

func (result *MatrixResult) StressOutcome() *MatrixOutcome {
	if result == nil || len(result.Outcomes) != 7 {
		return nil
	}
	return result.Outcomes[6]
}

func (result *MatrixResult) CanonicalBytes() json.RawMessage {
	if result == nil {
		return nil
	}
	return append(json.RawMessage(nil), result.canonicalBytes...)
}

func (result *MatrixResult) Hash() string {
	if result == nil {
		return ""
	}
	return result.hash
}

func (result *MatrixResult) ScoredHash() string {
	if result == nil {
		return ""
	}
	return result.scoredHash
}
