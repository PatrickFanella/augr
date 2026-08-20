package experimentrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/capital"
	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

const RunnerContractV1 = "experiment-runner-v1"

// ObservationMaterial binds one manifest row to the exact bytes and normalized
// quote used at the decision, route, and simulation boundaries.
type ObservationMaterial struct {
	PartitionContentSHA256   string
	ObservationSourceKey     string
	ObservationContentSHA256 string
	AvailableAt              time.Time
	CanonicalContent         []byte
	Snapshot                 marketdata.QuoteSnapshot
}

// EvidenceGraph is a fully loaded immutable declaration graph. The runner
// revalidates every edge before asking a program to plan or writing evidence.
type EvidenceGraph struct {
	Experiment       *strategycatalog.Experiment
	Version          *strategycatalog.Version
	Manifest         *dataset.Manifest
	Quality          *dataset.QualityResult
	Account          *domain.Account
	CapitalBinding   *capital.Binding
	CapitalPolicy    *capital.Policy
	CapitalState     *capital.State
	SimulationPolicy *simulation.Policy
	Instruments      map[uuid.UUID]*instrument.Instrument
	VenueContracts   map[uuid.UUID]*instrument.VenueContract
	Observations     []ObservationMaterial
}

// EvidenceLoader loads one exact declaration and all of its immutable parents.
type EvidenceLoader interface {
	LoadExperimentEvidence(context.Context, uuid.UUID) (*EvidenceGraph, error)
}

// Store is the narrow append-only boundary used by the runner. PostgreSQL and
// in-memory qualification implementations share this contract.
type Store interface {
	RecordProgram(context.Context, *ProgramIdentity) (*ProgramIdentity, error)
	RecordPlan(context.Context, *Plan) (*Plan, error)
	RecordAttemptEvent(context.Context, uuid.UUID, uuid.UUID, *AttemptEvent) (*AttemptEvent, error)
	RecordCompletedResult(context.Context, uuid.UUID, *Result, *AttemptEvent) (*Result, *AttemptEvent, error)
	ProposeExecutionIntent(context.Context, *lifecycle.Aggregate) (*lifecycle.Aggregate, error)
	RecordEconomicSourceEvent(context.Context, *ledger.EconomicSourceEvent) (*ledger.EconomicSourceEvent, error)
	ApplyExecutionFill(context.Context, uuid.UUID, *lifecycle.Transition) (*lifecycle.Aggregate, error)
	ApplyExecutionTransition(context.Context, uuid.UUID, *lifecycle.Transition) (*lifecycle.Aggregate, error)
	GetExecutionLifecycle(context.Context, uuid.UUID, uuid.UUID) (*lifecycle.Aggregate, error)
}

type RunRequest struct {
	ExperimentID uuid.UUID
	AttemptID    uuid.UUID
	StartedAt    time.Time
	FinishedAt   time.Time
	Program      Program
}

type Runner struct {
	loader EvidenceLoader
	store  Store
}

func NewRunner(loader EvidenceLoader, store Store) (*Runner, error) {
	if loader == nil || store == nil {
		return nil, fmt.Errorf("experiment runner loader and store are required")
	}
	return &Runner{loader: loader, store: store}, nil
}

// Run appends exactly one started and one terminal attempt event. A completed
// result is recorded only after all execution lifecycles have been reloaded.
func (runner *Runner) Run(ctx context.Context, request RunRequest) (*Result, error) {
	if runner == nil || runner.loader == nil || runner.store == nil || request.ExperimentID == uuid.Nil ||
		request.AttemptID == uuid.Nil || request.Program == nil || !canonicalTime(request.StartedAt) ||
		!canonicalTime(request.FinishedAt) || request.FinishedAt.Before(request.StartedAt) {
		return nil, fmt.Errorf("experiment run request is invalid")
	}
	started, err := NewAttemptEvent(AttemptEventInput{AttemptID: request.AttemptID, Sequence: 0, Type: AttemptStarted, OccurredAt: request.StartedAt})
	if err != nil {
		return nil, err
	}
	persistedStarted, err := runner.store.RecordAttemptEvent(ctx, request.ExperimentID, uuid.Nil, started)
	if err != nil {
		return nil, fmt.Errorf("record experiment attempt start: %w", err)
	}
	if !sameAttemptEvent(persistedStarted, started) {
		return nil, fmt.Errorf("record experiment attempt start: store returned mismatched event")
	}

	result, runErr := runner.run(ctx, request)
	if runErr != nil {
		return nil, runner.recordFailure(ctx, request, runErr)
	}

	completed, err := NewAttemptEvent(AttemptEventInput{AttemptID: request.AttemptID, Sequence: 1, Type: AttemptCompleted, OccurredAt: request.FinishedAt, ResultID: result.ID()})
	if err != nil {
		return nil, err
	}
	persistedResult, persistedCompleted, err := runner.store.RecordCompletedResult(ctx, request.ExperimentID, result, completed)
	if err != nil {
		completionErr := fmt.Errorf("record completed experiment result: %w", err)
		return nil, runner.recordFailure(ctx, request, completionErr)
	}
	if !sameResult(persistedResult, result) || !sameAttemptEvent(persistedCompleted, completed) {
		return nil, fmt.Errorf("record completed experiment result: store returned mismatched graph")
	}
	return persistedResult, nil
}

func (runner *Runner) recordFailure(ctx context.Context, request RunRequest, runErr error) error {
	code := "run_failed"
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		code = "context_cancelled"
	}
	failed, eventErr := NewAttemptEvent(AttemptEventInput{
		AttemptID: request.AttemptID, Sequence: 1, Type: AttemptFailed, OccurredAt: request.FinishedAt,
		ErrorCode: code, ErrorSHA256: hashBytes([]byte(runErr.Error())),
	})
	if eventErr != nil {
		return fmt.Errorf("experiment run failed (%v) and failure event is invalid: %w", runErr, eventErr)
	}
	persisted, persistErr := runner.store.RecordAttemptEvent(context.WithoutCancel(ctx), request.ExperimentID, uuid.Nil, failed)
	if persistErr != nil {
		return fmt.Errorf("experiment run failed (%v) and terminal evidence failed: %w", runErr, persistErr)
	}
	if !sameAttemptEvent(persisted, failed) {
		return fmt.Errorf("experiment run failed (%v) and store returned mismatched failure event", runErr)
	}
	return runErr
}

func (runner *Runner) run(ctx context.Context, request RunRequest) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	graph, err := runner.loader.LoadExperimentEvidence(ctx, request.ExperimentID)
	if err != nil {
		return nil, fmt.Errorf("load experiment evidence: %w", err)
	}
	program := request.Program.Identity()
	input, materials, err := validateEvidenceGraph(request.ExperimentID, graph, program)
	if err != nil {
		return nil, err
	}
	persistedProgram, err := runner.store.RecordProgram(ctx, program)
	if err != nil {
		return nil, fmt.Errorf("record experiment program: %w", err)
	}
	if !sameProgram(persistedProgram, program) {
		return nil, fmt.Errorf("record experiment program: store returned mismatched identity")
	}
	plan, err := request.Program.Plan(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("build experiment replay plan: %w", err)
	}
	steps, err := validatePlan(graph, program, plan, materials)
	if err != nil {
		return nil, err
	}
	assessments, err := preflightCapital(graph, plan, steps, materials)
	if err != nil {
		return nil, err
	}
	_ = assessments // The immutable assessments are re-created before any write; migration 78 records run evidence, not a new capital authority.
	persistedPlan, err := runner.store.RecordPlan(ctx, plan)
	if err != nil {
		return nil, fmt.Errorf("record experiment replay plan: %w", err)
	}
	if !samePlan(persistedPlan, plan) {
		return nil, fmt.Errorf("record experiment replay plan: store returned mismatched plan")
	}

	venue, err := simulation.NewVenue(graph.SimulationPolicy)
	if err != nil {
		return nil, err
	}
	outcomes := make([]StepOutcomeInput, len(steps))
	for sequence, step := range steps {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		outcomes[sequence] = StepOutcomeInput{Action: step.Action, DecisionSHA256: plan.DecisionSHA256(sequence), FilledQuantity: "0", FeeTotal: "0"}
		if step.Action != ActionExecute {
			continue
		}
		outcome, executeErr := runner.executeStep(ctx, graph, venue, plan, sequence, step, materials[observationKey(step)])
		if executeErr != nil {
			return nil, fmt.Errorf("execute experiment step %d: %w", sequence, executeErr)
		}
		outcomes[sequence] = outcome
	}
	result, err := NewResult(ResultInput{
		Plan: plan, AccountID: graph.Account.ID, QualityResultID: graph.Quality.ID(),
		SimulationPolicyVersion: graph.SimulationPolicy.Version(), CapitalPolicyVersion: graph.CapitalPolicy.Version(), Outcomes: outcomes,
	})
	if err != nil {
		return nil, fmt.Errorf("build experiment result: %w", err)
	}
	return result, nil
}

func validateEvidenceGraph(experimentID uuid.UUID, graph *EvidenceGraph, program *ProgramIdentity) (ProgramInput, map[string]ObservationMaterial, error) {
	if graph == nil || graph.Experiment == nil || graph.Version == nil || graph.Manifest == nil || graph.Quality == nil ||
		graph.Account == nil || graph.CapitalBinding == nil || graph.CapitalPolicy == nil || graph.CapitalState == nil ||
		graph.SimulationPolicy == nil || program == nil {
		return ProgramInput{}, nil, fmt.Errorf("experiment evidence graph is incomplete")
	}
	experiment := graph.Experiment
	version := graph.Version
	if experiment.ID() != experimentID || experiment.State() != strategycatalog.ExperimentDeclared || experiment.VersionID() != version.ID() ||
		experiment.AccountID() != graph.Account.ID || experiment.CapitalBindingID() != graph.CapitalBinding.ID ||
		experiment.ManifestID() != graph.Manifest.ID() || experiment.QualityResultID() != graph.Quality.ID() ||
		graph.Quality.ManifestID() != graph.Manifest.ID() || experiment.DatasetQuarantined() != graph.Quality.Quarantined() ||
		experiment.SimulationPolicyVersion() != graph.SimulationPolicy.Version() || experiment.CapitalPolicyVersion() != graph.CapitalPolicy.Version() ||
		program.VersionID() != version.ID() || program.VersionSHA256() != version.Digest() || program.CompilerKind() != version.CompilerKind() ||
		program.CompilerVersion() != version.CompilerVersion() || program.SourceCommit() != version.SourceCommit() ||
		program.SourceTreeSHA256() != version.SourceTreeSHA256() || program.DecisionContract() != version.DecisionContract() ||
		program.RunnerContract() != RunnerContractV1 {
		return ProgramInput{}, nil, fmt.Errorf("experiment evidence identity does not match declaration")
	}
	if err := graph.Account.Validate(); err != nil {
		return ProgramInput{}, nil, fmt.Errorf("experiment account: %w", err)
	}
	if err := graph.CapitalBinding.Validate(*graph.Account, graph.CapitalPolicy); err != nil {
		return ProgramInput{}, nil, fmt.Errorf("experiment capital binding: %w", err)
	}
	wantEnvironment := domain.AccountEnvironmentPaperScored
	if experiment.Mode() == strategycatalog.ExperimentPaperStress {
		wantEnvironment = domain.AccountEnvironmentPaperStress
	}
	if graph.Account.Environment != wantEnvironment || experiment.Mode() == strategycatalog.ExperimentPaperScored && graph.Quality.Quarantined() {
		return ProgramInput{}, nil, fmt.Errorf("experiment mode, account, or quality boundary does not match")
	}

	input := ProgramInput{
		ExperimentID: experiment.ID(), AccountID: graph.Account.ID, CapitalStateID: graph.CapitalState.ID(), CapitalStateSHA256: graph.CapitalState.Hash(),
		CapitalProjectionCheckpointID: graph.CapitalState.ProjectionCheckpointID(),
		CapitalStateBytes:             graph.CapitalState.CanonicalBytes(),
		ManifestID:                    graph.Manifest.ID(), ManifestSHA256: graph.Manifest.Digest(),
		EvaluationStart: formatTime(experiment.EvaluationStart()), EvaluationEnd: formatTime(experiment.EvaluationEnd()),
		Seed: experiment.Seed(), Mode: experiment.Mode(),
	}
	manifestRows := make(map[string]ObservationEvidence)
	manifestKinds := make(map[dataset.Kind]struct{})
	for _, partition := range graph.Manifest.Partitions() {
		manifestKinds[partition.Kind] = struct{}{}
		for _, observation := range partition.Observations {
			available := parseTime(observation.AvailableAt)
			if available.Before(experiment.EvaluationStart()) || available.After(experiment.EvaluationEnd()) {
				continue
			}
			value := ObservationEvidence{PartitionContentSHA256: partition.ContentSHA256, SourceKey: observation.SourceKey, ContentSHA256: observation.ContentSHA256, AvailableAt: observation.AvailableAt}
			key := evidenceKey(value.PartitionContentSHA256, value.SourceKey, value.ContentSHA256)
			if _, duplicate := manifestRows[key]; duplicate {
				return ProgramInput{}, nil, fmt.Errorf("manifest contains duplicate in-window observation evidence")
			}
			manifestRows[key] = value
			input.Evidence = append(input.Evidence, value)
		}
	}
	if len(input.Evidence) == 0 {
		return ProgramInput{}, nil, fmt.Errorf("manifest contains no observation in the experiment window")
	}
	for _, kind := range version.RequiredDatasetKinds() {
		if _, present := manifestKinds[kind]; !present {
			return ProgramInput{}, nil, fmt.Errorf("manifest lacks strategy-required dataset kind %q", kind)
		}
	}
	materials := make(map[string]ObservationMaterial, len(graph.Observations))
	for _, material := range graph.Observations {
		key := evidenceKey(material.PartitionContentSHA256, material.ObservationSourceKey, material.ObservationContentSHA256)
		manifest, declared := manifestRows[key]
		if snapshotErr := material.Snapshot.Validate(); snapshotErr != nil {
			return ProgramInput{}, nil, fmt.Errorf("observation material %q quote: %w", material.ObservationSourceKey, snapshotErr)
		}
		if !declared || material.AvailableAt.Format(timeLayout) != manifest.AvailableAt || hashBytes(material.CanonicalContent) != material.ObservationContentSHA256 ||
			material.Snapshot.AvailableAt == nil || !material.Snapshot.AvailableAt.Equal(material.AvailableAt) || material.Snapshot.ObservationID != material.ObservationSourceKey {
			return ProgramInput{}, nil, fmt.Errorf("observation material %q does not reconstruct manifest evidence", material.ObservationSourceKey)
		}
		if _, duplicate := materials[key]; duplicate {
			return ProgramInput{}, nil, fmt.Errorf("observation material is duplicated")
		}
		materials[key] = material
	}
	return input, materials, nil
}

func validatePlan(graph *EvidenceGraph, program *ProgramIdentity, plan *Plan, materials map[string]ObservationMaterial) ([]StepInput, error) {
	if plan == nil || plan.ExperimentID() != graph.Experiment.ID() || plan.ProgramID() != program.ID() ||
		plan.AccountID() != graph.Account.ID || plan.CapitalStateID() != graph.CapitalState.ID() || plan.CapitalStateSHA256() != graph.CapitalState.Hash() ||
		plan.CapitalProjectionCheckpointID() != graph.CapitalState.ProjectionCheckpointID() ||
		string(plan.CapitalStateBytes()) != string(graph.CapitalState.CanonicalBytes()) ||
		plan.ManifestID() != graph.Manifest.ID() || plan.ManifestSHA256() != graph.Manifest.Digest() ||
		plan.EvaluationStart() != graph.Experiment.EvaluationStart() || plan.EvaluationEnd() != graph.Experiment.EvaluationEnd() ||
		plan.Seed() != graph.Experiment.Seed() || plan.Mode() != graph.Experiment.Mode() {
		return nil, fmt.Errorf("experiment replay plan does not match declaration")
	}
	steps := plan.Steps()
	last := time.Time{}
	for sequence, step := range steps {
		if !last.IsZero() && step.AvailableAt.Before(last) {
			return nil, fmt.Errorf("experiment replay plan observations are not time ordered")
		}
		last = step.AvailableAt
		if _, ok := materials[observationKey(step)]; !ok {
			return nil, fmt.Errorf("experiment replay step %d lacks exact observation material", sequence)
		}
		if step.Intent != nil {
			material := materials[observationKey(step)]
			instrumentValue := graph.Instruments[step.Intent.InstrumentID]
			contract := graph.VenueContracts[step.Intent.VenueContractID]
			if instrumentValue == nil || contract == nil || material.Snapshot.InstrumentID != instrumentValue.ID ||
				material.Snapshot.VenueContractID == nil || *material.Snapshot.VenueContractID != contract.ID || contract.InstrumentID != instrumentValue.ID ||
				step.Intent.RouteAt.Before(contract.ValidFrom) || contract.ValidTo != nil && !step.Intent.RouteAt.Before(*contract.ValidTo) ||
				material.AvailableAt.After(step.Intent.DecisionAt) || material.AvailableAt.After(step.Intent.RouteAt) {
				return nil, fmt.Errorf("experiment replay step %d reference or no-lookahead evidence is invalid", sequence)
			}
		}
	}
	return steps, nil
}

func preflightCapital(graph *EvidenceGraph, plan *Plan, steps []StepInput, materials map[string]ObservationMaterial) ([]*capital.Assessment, error) {
	assessments := make([]*capital.Assessment, len(steps))
	for sequence, step := range steps {
		if step.Action != ActionExecute {
			continue
		}
		quantity := decimal.RequireFromString(step.Intent.Quantity)
		if step.Intent.Side == "sell" {
			quantity = quantity.Neg()
		}
		material := materials[observationKey(step)]
		price, err := preflightPrice(step.Intent, material.Snapshot)
		if err != nil {
			return nil, fmt.Errorf("experiment step %d capital price: %w", sequence, err)
		}
		inst := graph.Instruments[step.Intent.InstrumentID]
		contract := graph.VenueContracts[step.Intent.VenueContractID]
		direction := capital.ExposureIncreaseLong
		if quantity.IsNegative() {
			direction = capital.ExposureIncreaseShort
		}
		proposedNotional, err := capitalAssessmentNotional(step, *inst, *contract, price)
		if err != nil {
			return nil, fmt.Errorf("experiment step %d capital notional: %w", sequence, err)
		}
		assessment, err := capital.Assess(capital.AssessmentInput{
			Account: *graph.Account, Binding: *graph.CapitalBinding, Policy: graph.CapitalPolicy, State: graph.CapitalState,
			Instrument: *inst, Currency: graph.Account.BaseCurrency,
			ScenarioID: plan.ID().String() + "/step/" + fmt.Sprint(sequence), Direction: direction,
			ProposedNotional: proposedNotional,
		})
		if err != nil {
			return nil, fmt.Errorf("experiment step %d capital assessment: %w", sequence, err)
		}
		if assessment.Decision != capital.DecisionAdmitted {
			return nil, fmt.Errorf("experiment step %d capital rejected: %s", sequence, assessment.Reason)
		}
		assessments[sequence] = assessment
	}
	return assessments, nil
}

func capitalAssessmentNotional(step StepInput, inst instrument.Instrument, contract instrument.VenueContract, price decimal.Decimal) (decimal.Decimal, error) {
	executionNotional := decimal.RequireFromString(step.Intent.Quantity).Mul(price).Mul(contract.Multiplier)
	if inst.AssetClass != instrument.AssetClassOption {
		return executionNotional, nil
	}
	var decision struct {
		CapitalNotional string `json:"capital_notional"`
	}
	if err := json.Unmarshal(step.Decision, &decision); err != nil || decision.CapitalNotional == "" {
		return decimal.Zero, fmt.Errorf("option decision lacks declared capital notional")
	}
	value, err := decimal.NewFromString(decision.CapitalNotional)
	if err != nil || !value.IsPositive() || value.String() != decision.CapitalNotional || value.LessThan(executionNotional) {
		return decimal.Zero, fmt.Errorf("option decision capital notional is invalid")
	}
	return value, nil
}

func (runner *Runner) executeStep(ctx context.Context, graph *EvidenceGraph, venue *simulation.Venue, plan *Plan, sequence int, step StepInput, material ObservationMaterial) (StepOutcomeInput, error) {
	spec := step.Intent
	inst := graph.Instruments[spec.InstrumentID]
	contract := graph.VenueContracts[spec.VenueContractID]
	quantity := decimal.RequireFromString(spec.Quantity)
	if spec.Side == "sell" {
		quantity = quantity.Neg()
	}
	metadata := append(json.RawMessage(nil), step.Decision...)
	proposed, err := lifecycle.Propose(lifecycle.ProposeInput{
		Account: *graph.Account, Instrument: *inst, DecisionSnapshot: material.Snapshot,
		IdempotencyKey: plan.IntentIdempotencyKey(sequence), DesiredQuantityDelta: quantity, DecisionAt: spec.DecisionAt,
		OriginType: ledger.ExecutionOriginStrategyVersion, OriginID: graph.Version.ID().String(), StrategyVersionID: graph.Version.ID().String(),
		Metadata: metadata, Event: runnerEvent(plan, sequence, "proposal", spec.DecisionAt, material), CreatedAt: spec.DecisionAt,
	})
	if err != nil {
		return StepOutcomeInput{}, err
	}
	current, err := runner.store.ProposeExecutionIntent(ctx, proposed)
	if err != nil {
		return StepOutcomeInput{}, err
	}
	if current.Intent.ID != plan.IntentID(sequence) {
		return StepOutcomeInput{}, fmt.Errorf("persisted intent identity does not match plan")
	}
	if current.State == lifecycle.StateProposed {
		allocation, allocationErr := lifecycle.Allocate(current, quantity, runnerEvent(plan, sequence, "allocation", spec.DecisionAt, material), spec.DecisionAt)
		if allocationErr != nil {
			return StepOutcomeInput{}, allocationErr
		}
		current, err = runner.store.ApplyExecutionTransition(ctx, graph.Account.ID, allocation)
		if err != nil {
			return StepOutcomeInput{}, err
		}
	}
	if current.State == lifecycle.StateAllocated {
		approval, approvalErr := lifecycle.ApproveRisk(current, runnerEvent(plan, sequence, "capital_admitted", spec.RouteAt, material), spec.RouteAt)
		if approvalErr != nil {
			return StepOutcomeInput{}, approvalErr
		}
		current, err = runner.store.ApplyExecutionTransition(ctx, graph.Account.ID, approval)
		if err != nil {
			return StepOutcomeInput{}, err
		}
	}
	asset, ok := graph.SimulationPolicy.AssetPolicy(inst.AssetClass)
	if !ok {
		return StepOutcomeInput{}, fmt.Errorf("simulation policy lacks instrument asset class")
	}
	var limit *decimal.Decimal
	if spec.LimitPrice != nil {
		value := decimal.RequireFromString(*spec.LimitPrice)
		limit = &value
	}
	if current.State == lifecycle.StateRiskApproved {
		route, routeErr := lifecycle.Route(current, lifecycle.RouteInput{
			OrderIdempotencyKey: plan.OrderIdempotencyKey(sequence), Instrument: *inst, VenueContract: *contract,
			RouteSnapshot: material.Snapshot, QuoteRequirements: asset.QuoteRequirements,
			OrderType: lifecycle.OrderType(spec.OrderType), TimeInForce: lifecycle.TimeInForce(spec.TimeInForce), LimitPrice: limit,
			PolicyKind: lifecycle.PolicySimulation, PolicyVersion: graph.SimulationPolicy.Version(),
			Event: runnerEvent(plan, sequence, "route", spec.RouteAt, material), RoutedAt: spec.RouteAt, CreatedAt: spec.RouteAt,
		})
		if routeErr != nil {
			return StepOutcomeInput{}, routeErr
		}
		current, err = runner.store.ApplyExecutionTransition(ctx, graph.Account.ID, route)
		if err != nil {
			return StepOutcomeInput{}, err
		}
	}
	if current.Order == nil || current.Order.ID != plan.OrderID(sequence) {
		return StepOutcomeInput{}, fmt.Errorf("persisted order identity does not match plan")
	}
	evaluated, err := venue.Evaluate(simulation.EvaluationRequest{
		Account: *graph.Account, Instrument: *inst, VenueContract: *contract, Aggregate: current,
		Snapshot: material.Snapshot, EvaluatedAt: spec.RouteAt,
	})
	if err != nil {
		return StepOutcomeInput{}, err
	}
	if _, err = simulation.PersistResult(ctx, runner.store, graph.Account.ID, evaluated); err != nil {
		return StepOutcomeInput{}, err
	}
	reloaded, err := runner.store.GetExecutionLifecycle(ctx, graph.Account.ID, plan.IntentID(sequence))
	if err != nil {
		return StepOutcomeInput{}, fmt.Errorf("reload completed execution lifecycle: %w", err)
	}
	if reloaded.Order == nil || reloaded.Order.ID != plan.OrderID(sequence) {
		return StepOutcomeInput{}, fmt.Errorf("reloaded execution lifecycle identity does not match plan")
	}
	effects, feeTotal, err := replayEffects(graph.SimulationPolicy, *inst, reloaded)
	if err != nil {
		return StepOutcomeInput{}, err
	}
	economicOutcome, err := simulation.NewOutcome(simulation.OutcomeInput{Account: *graph.Account, VenueContract: *contract, Aggregate: reloaded, Fills: effects})
	if err != nil {
		return StepOutcomeInput{}, err
	}
	transitionIDs := make([]uuid.UUID, len(reloaded.Events))
	for i := range reloaded.Events {
		transitionIDs[i] = reloaded.Events[i].ID
	}
	fillIDs := make([]uuid.UUID, len(reloaded.Fills))
	filled := decimal.Zero
	for i := range reloaded.Fills {
		fillIDs[i] = reloaded.Fills[i].ID
		filled = filled.Add(reloaded.Fills[i].Quantity)
	}
	return StepOutcomeInput{
		Action: ActionExecute, DecisionSHA256: plan.DecisionSHA256(sequence), IntentID: reloaded.Intent.ID, OrderID: reloaded.Order.ID,
		TransitionIDs: transitionIDs, FillIDs: fillIDs, FilledQuantity: filled.String(), FeeTotal: feeTotal.String(),
		AggregateSHA256: aggregateHash(reloaded), OutcomeSHA256: economicOutcome.Hash(),
	}, nil
}

func replayEffects(policy *simulation.Policy, inst instrument.Instrument, aggregate *lifecycle.Aggregate) ([]simulation.FillEffect, decimal.Decimal, error) {
	effects := make([]simulation.FillEffect, len(aggregate.Fills))
	fees := decimal.Zero
	for i, fill := range aggregate.Fills {
		fee, err := policy.FillFee(inst.AssetClass, fill.Quantity, fill.Price, inst.Multiplier, i == 0)
		if err != nil {
			return nil, decimal.Zero, err
		}
		if fee != nil {
			fees = fees.Add(*fee)
		}
		effects[i] = simulation.FillEffect{Fill: fill, Quantity: fill.Quantity, Price: fill.Price, Fee: fee, PolicyVersion: aggregate.Order.PolicyVersion}
	}
	return effects, fees, nil
}

func aggregateHash(aggregate *lifecycle.Aggregate) string {
	clone := *aggregate
	clone.Events = append([]lifecycle.Event(nil), aggregate.Events...)
	for i := range clone.Events {
		clone.Events[i].IngestSequence = 0
	}
	encoded, _ := json.Marshal(clone)
	return hashBytes(encoded)
}

func runnerEvent(plan *Plan, sequence int, stage string, at time.Time, material ObservationMaterial) lifecycle.EventInput {
	evidence, _ := json.Marshal(struct {
		PlanID                   string `json:"plan_id"`
		Sequence                 int    `json:"sequence"`
		Stage                    string `json:"stage"`
		ObservationContentSHA256 string `json:"observation_content_sha256"`
	}{plan.ID().String(), sequence, stage, material.ObservationContentSHA256})
	return lifecycle.EventInput{
		Source: "experiment-runner", SourceNamespace: "experiment/" + plan.ExperimentID().String(),
		SourceEventID:  plan.ID().String() + "/step/" + fmt.Sprint(sequence) + "/" + stage,
		SourceRevision: material.Snapshot.SourceRevision, SourceAt: at, ReceivedAt: at,
		Actor: "experiment-runner", ReasonCode: stage, Evidence: evidence,
	}
}

func preflightPrice(spec *IntentSpecInput, snapshot marketdata.QuoteSnapshot) (decimal.Decimal, error) {
	if spec.LimitPrice != nil {
		return decimal.RequireFromString(*spec.LimitPrice), nil
	}
	if spec.Side == "buy" && snapshot.Ask != nil && snapshot.Ask.IsPositive() {
		return *snapshot.Ask, nil
	}
	if spec.Side == "sell" && snapshot.Bid != nil && snapshot.Bid.IsPositive() {
		return *snapshot.Bid, nil
	}
	return decimal.Zero, fmt.Errorf("executable top-of-book price is required")
}

func observationKey(step StepInput) string {
	return evidenceKey(step.PartitionContentSHA256, step.ObservationSourceKey, step.ObservationContentSHA256)
}

func evidenceKey(partition, source, content string) string {
	return partition + "\x00" + source + "\x00" + content
}

func sameProgram(left, right *ProgramIdentity) bool {
	return left != nil && right != nil && left.ID() == right.ID() && left.Digest() == right.Digest() && string(left.CanonicalBytes()) == string(right.CanonicalBytes())
}

func samePlan(left, right *Plan) bool {
	return left != nil && right != nil && left.ID() == right.ID() && left.Digest() == right.Digest() && string(left.CanonicalBytes()) == string(right.CanonicalBytes())
}

func sameResult(left, right *Result) bool {
	return left != nil && right != nil && left.ID() == right.ID() && left.Digest() == right.Digest() && string(left.CanonicalBytes()) == string(right.CanonicalBytes())
}

func sameAttemptEvent(left, right *AttemptEvent) bool {
	return left != nil && right != nil && left.ID() == right.ID() && left.Digest() == right.Digest() && string(left.CanonicalBytes()) == string(right.CanonicalBytes())
}
