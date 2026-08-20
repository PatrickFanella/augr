package venuerecon

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
)

const (
	runSchemaV1      = "venue-reconciliation-run-v1"
	resultIDDomain   = "venue-reconciliation-result"
	incidentIDDomain = "venue-reconciliation-incident"
	runIDDomain      = "venue-reconciliation-run"
)

// Result is one exact comparison fact. Nil values mean absent, never zero.
type Result struct {
	ID            uuid.UUID      `json:"id"`
	Key           string         `json:"key"`
	Kind          ComparisonKind `json:"kind"`
	Status        ResultStatus   `json:"status"`
	Reason        ReasonCode     `json:"reason"`
	Severity      Severity       `json:"severity"`
	ProviderValue *string        `json:"provider_value"`
	LocalValue    *string        `json:"local_value"`
	Delta         *string        `json:"delta"`
}

// Incident is the immutable non-clean consequence of one result.
type Incident struct {
	ID       uuid.UUID  `json:"id"`
	ResultID uuid.UUID  `json:"result_id"`
	Key      string     `json:"key"`
	Reason   ReasonCode `json:"reason"`
	Severity Severity   `json:"severity"`
}

// Run is a deterministic comparison graph. It contains no process time.
type Run struct {
	ID                 uuid.UUID       `json:"id"`
	Schema             string          `json:"schema"`
	PolicyVersion      string          `json:"policy_version"`
	ProviderSnapshotID uuid.UUID       `json:"provider_snapshot_id"`
	LocalSnapshotID    uuid.UUID       `json:"local_snapshot_id"`
	Clean              bool            `json:"clean"`
	Results            []Result        `json:"results"`
	Incidents          []Incident      `json:"incidents"`
	SHA256             string          `json:"sha256"`
	CanonicalBytes     json.RawMessage `json:"-"`
	generationSeal     string
}

type CompareInput struct {
	Policy                *Policy
	Provider              *SnapshotAdmission
	Local                 *LocalSnapshot
	EquityBasisEquivalent bool
}

// Compare produces exact results and one incident per non-clean result.
func Compare(input CompareInput) (*Run, error) {
	if input.Policy == nil || input.Local == nil || input.Provider == nil {
		return nil, fmt.Errorf("policy, provider admission, and local snapshot are required")
	}
	if input.Policy.Schema() != PolicySchemaV1 || input.Policy.Version() == "" || input.Local.ID() == uuid.Nil {
		return nil, fmt.Errorf("comparison evidence is invalid")
	}
	if err := validateLocalSnapshot(input.Local); err != nil {
		return nil, err
	}
	results := make([]Result, 0)
	providerSnapshotID := uuid.Nil
	if input.Provider.Snapshot == nil {
		reason := input.Provider.Reason
		if reason != ReasonProviderUnavailable && reason != ReasonSnapshotUnstable && reason != ReasonSnapshotIncomplete && reason != ReasonSnapshotMappingFailure {
			return nil, fmt.Errorf("provider admission failure reason is invalid")
		}
		result, err := makeResult(input.Policy, "snapshot", reason, nil, nil, nil)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
		return finishRun(input.Policy, providerSnapshotID, input.Local.ID(), results)
	}
	providerSnapshotID = input.Provider.Snapshot.ID()
	if err := validateStableProviderSnapshot(input.Provider.Snapshot); err != nil {
		return nil, err
	}
	provider := input.Provider.Snapshot.Capture()
	if provider == nil {
		return nil, fmt.Errorf("stable provider snapshot has no capture")
	}
	if provider.canonical.AccountID != input.Local.canonical.AccountID || provider.canonical.Provider != input.Local.canonical.Provider ||
		provider.canonical.Namespace != input.Local.canonical.Namespace || provider.canonical.Currency != input.Local.canonical.Currency ||
		provider.canonical.HorizonStart != input.Local.canonical.HorizonStart || provider.canonical.HorizonEnd != input.Local.canonical.HorizonEnd {
		result, err := makeResult(input.Policy, "snapshot:scope", ReasonUnsupportedFact, stringPointer(scopeKey(provider)), stringPointer(localScopeKey(input.Local)), nil)
		if err != nil {
			return nil, err
		}
		return finishRun(input.Policy, providerSnapshotID, input.Local.ID(), []Result{result})
	}
	results = append(results, compareCash(input.Policy, provider.canonical.Cash, input.Local.canonical.Cash))
	if input.EquityBasisEquivalent {
		equity := compareCash(input.Policy, provider.canonical.Equity, input.Local.canonical.Equity)
		equity.Key = "equity"
		results = append(results, equity)
	} else {
		equity, err := makeResult(input.Policy, "equity", ReasonEquityBasisNotComparable,
			stringPointer(provider.canonical.Equity), stringPointer(input.Local.canonical.Equity), nil)
		if err != nil {
			return nil, err
		}
		results = append(results, equity)
	}
	positionResults, err := comparePositions(input.Policy, provider.canonical.Positions, input.Local.canonical.Positions)
	if err != nil {
		return nil, err
	}
	results = append(results, positionResults...)
	providerRule, ok := input.Policy.ProviderRule(provider.canonical.Provider)
	if !ok || providerRule.AuthoritativeFillNamespace != provider.canonical.Namespace {
		return nil, fmt.Errorf("provider fill namespace is not authorized by policy")
	}
	fillResults, err := compareFills(input.Policy, string(provider.canonical.Provider), providerRule.AuthoritativeFillNamespace,
		provider.canonical.Fills, input.Local.canonical.Fills)
	if err != nil {
		return nil, err
	}
	results = append(results, fillResults...)
	for _, issue := range input.Local.canonical.Issues {
		result, resultErr := makeResult(input.Policy, "snapshot:"+string(issue.Reason)+":"+issue.SourceID, issue.Reason, nil, stringPointer(issue.EvidenceID), nil)
		if resultErr != nil {
			return nil, resultErr
		}
		results = append(results, result)
	}
	if allMatched(results) {
		matched, err := makeResult(input.Policy, "snapshot", ReasonSnapshotMatched, stringPointer(provider.digest), stringPointer(input.Local.digest), nil)
		if err != nil {
			return nil, err
		}
		results = append(results, matched)
	}
	return finishRun(input.Policy, providerSnapshotID, input.Local.ID(), results)
}

func compareCash(policy *Policy, providerValue, localValue string) Result {
	if providerValue == localValue {
		result, _ := makeResult(policy, "cash", ReasonCashMatched, &providerValue, &localValue, stringPointer("0"))
		return result
	}
	providerDecimal, _ := decimal.NewFromString(providerValue)
	localDecimal, _ := decimal.NewFromString(localValue)
	delta := providerDecimal.Sub(localDecimal).String()
	result, _ := makeResult(policy, "cash", ReasonCashMismatch, &providerValue, &localValue, &delta)
	return result
}

func comparePositions(policy *Policy, provider []ProviderPosition, local []LocalPosition) ([]Result, error) {
	providerByID := make(map[uuid.UUID]ProviderPosition, len(provider))
	localByID := make(map[uuid.UUID]LocalPosition, len(local))
	keys := make(map[uuid.UUID]struct{}, len(provider)+len(local))
	for _, row := range provider {
		providerByID[row.InstrumentID] = row
		keys[row.InstrumentID] = struct{}{}
	}
	for _, row := range local {
		localByID[row.InstrumentID] = row
		keys[row.InstrumentID] = struct{}{}
	}
	ids := sortedUUIDKeys(keys)
	results := make([]Result, 0, len(ids))
	for _, id := range ids {
		providerRow, providerOK := providerByID[id]
		localRow, localOK := localByID[id]
		key := "position:" + id.String()
		var reason ReasonCode
		var providerValue, localValue *string
		switch {
		case !providerOK:
			reason, localValue = ReasonPositionProviderMissing, &localRow.Quantity
		case !localOK:
			reason, providerValue = ReasonPositionLocalMissing, &providerRow.Quantity
		case providerRow.Quantity != localRow.Quantity:
			reason, providerValue, localValue = ReasonPositionQuantityMismatch, &providerRow.Quantity, &localRow.Quantity
		default:
			reason, providerValue, localValue = ReasonPositionMatched, &providerRow.Quantity, &localRow.Quantity
		}
		result, err := makeResult(policy, key, reason, providerValue, localValue, decimalDelta(providerValue, localValue))
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func compareFills(policy *Policy, providerName, namespace string, provider []ProviderFill, local []LocalFillEvidence) ([]Result, error) {
	providerByKey := make(map[string]ProviderFill, len(provider))
	localByKey := make(map[string]LocalFillEvidence, len(local))
	keys := make(map[string]struct{}, len(provider)+len(local))
	for _, row := range provider {
		key := providerName + "\x00" + namespace + "\x00" + providerComparisonKey(row)
		providerByKey[key] = row
		keys[key] = struct{}{}
	}
	for _, row := range local {
		key := providerName + "\x00" + namespace + "\x00" + localComparisonKey(row)
		localByKey[key] = row
		keys[key] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	results := make([]Result, 0, len(ordered))
	for _, key := range ordered {
		providerRow, providerOK := providerByKey[key]
		localRow, localOK := localByKey[key]
		resultKey := "fill:" + strings.ReplaceAll(key, "\x00", ":")
		if !providerOK || !localOK {
			reason := ReasonFillProviderMissing
			var providerValue, localValue *string
			if providerOK {
				reason, providerValue = ReasonFillLocalMissing, stringPointer(providerRow.SourceID)
			} else {
				localValue = stringPointer(localRow.SourceID)
			}
			result, err := makeResult(policy, resultKey, reason, providerValue, localValue, nil)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
			continue
		}
		if providerRow.ObservationClass != lifecycle.ObservationOrdinary || localRow.ObservationClass != lifecycle.ObservationOrdinary {
			reason := ReasonCorrectionPending
			if providerRow.ObservationClass == lifecycle.ObservationBust || localRow.ObservationClass == lifecycle.ObservationBust {
				reason = ReasonBustPending
			}
			result, err := makeResult(policy, resultKey, reason, stringPointer(providerRow.SourceID), stringPointer(localRow.SourceID), nil)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
			continue
		}
		mismatches := []struct {
			suffix          string
			reason          ReasonCode
			provider, local string
		}{
			{"quantity", ReasonFillQuantityMismatch, providerRow.Quantity, localRow.Quantity},
			{"price", ReasonFillPriceMismatch, providerRow.Price, localRow.Price},
			{"fee", ReasonFillFeeMismatch, providerRow.Fee, localRow.Fee},
			{"instrument", ReasonFillInstrumentMismatch, providerRow.InstrumentID.String() + "/" + providerRow.VenueContract.String(), localRow.InstrumentID + "/" + localRow.VenueContractID},
			{"side", ReasonFillSideMismatch, string(providerRow.Side), string(localRow.Side)},
			{"order", ReasonFillOrderMismatch, providerRow.ExternalOrderID + "/" + providerRow.ClientOrderID, localRow.ExternalOrderID + "/" + localRow.ClientOrderID},
		}
		matched := true
		for _, mismatch := range mismatches {
			if mismatch.provider == mismatch.local {
				continue
			}
			matched = false
			result, err := makeResult(policy, resultKey+":"+mismatch.suffix, mismatch.reason, &mismatch.provider, &mismatch.local, decimalDeltaForReason(mismatch.reason, mismatch.provider, mismatch.local))
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		}
		if matched {
			result, err := makeResult(policy, resultKey, ReasonFillMatched, stringPointer(providerRow.SourceID), stringPointer(localRow.SourceID), nil)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		}
	}
	return results, nil
}

func makeResult(policy *Policy, key string, reason ReasonCode, providerValue, localValue, delta *string) (Result, error) {
	rule, ok := policy.Reason(reason)
	if !ok || key == "" {
		return Result{}, fmt.Errorf("comparison reason %q is not in policy", reason)
	}
	canonical := struct {
		Key           string         `json:"key"`
		Kind          ComparisonKind `json:"kind"`
		Status        ResultStatus   `json:"status"`
		Reason        ReasonCode     `json:"reason"`
		Severity      Severity       `json:"severity"`
		ProviderValue *string        `json:"provider_value"`
		LocalValue    *string        `json:"local_value"`
		Delta         *string        `json:"delta"`
	}{
		key, rule.Kind, rule.Status, reason, rule.Severity, providerValue, localValue, delta,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return Result{}, err
	}
	return Result{
		ID: economicid.DeterministicUUID(resultIDDomain, string(encoded)), Key: key, Kind: rule.Kind,
		Status: rule.Status, Reason: reason, Severity: rule.Severity, ProviderValue: cloneString(providerValue),
		LocalValue: cloneString(localValue), Delta: cloneString(delta),
	}, nil
}

func finishRun(policy *Policy, providerID, localID uuid.UUID, results []Result) (*Run, error) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Key == results[j].Key {
			return results[i].Reason < results[j].Reason
		}
		return results[i].Key < results[j].Key
	})
	for index := range results {
		id, err := deterministicResultID(policy.Version(), providerID, localID, results[index])
		if err != nil {
			return nil, err
		}
		results[index].ID = id
	}
	incidents := make([]Incident, 0)
	for _, result := range results {
		if result.Status == StatusMatched {
			continue
		}
		incidents = append(incidents, Incident{
			ID:       economicid.DeterministicUUID(incidentIDDomain, result.ID.String()),
			ResultID: result.ID, Key: result.Key, Reason: result.Reason, Severity: result.Severity,
		})
	}
	payload := struct {
		Schema             string     `json:"schema"`
		PolicyVersion      string     `json:"policy_version"`
		ProviderSnapshotID string     `json:"provider_snapshot_id"`
		LocalSnapshotID    string     `json:"local_snapshot_id"`
		Clean              bool       `json:"clean"`
		Results            []Result   `json:"results"`
		Incidents          []Incident `json:"incidents"`
	}{
		runSchemaV1, policy.Version(), projectionUUIDString(providerID), localID.String(), len(incidents) == 0, results, incidents,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	digest := sha256Hex(encoded)
	return &Run{
		ID: economicid.DeterministicUUID(runIDDomain, runSchemaV1+"@sha256:"+digest), Schema: runSchemaV1,
		PolicyVersion: policy.Version(), ProviderSnapshotID: providerID, LocalSnapshotID: localID, Clean: len(incidents) == 0,
		Results: append(make([]Result, 0, len(results)), results...), Incidents: append(make([]Incident, 0, len(incidents)), incidents...), SHA256: digest,
		CanonicalBytes: append(json.RawMessage(nil), encoded...), generationSeal: digest,
	}, nil
}

func validateStableProviderSnapshot(snapshot *StableProviderSnapshot) error {
	if snapshot == nil || !sameCapturePayload(snapshot.first, snapshot.second) {
		return fmt.Errorf("stable provider snapshot captures are invalid")
	}
	for _, capture := range []*ProviderCapture{snapshot.first, snapshot.second} {
		encoded, err := json.Marshal(capture.canonical)
		if err != nil || string(encoded) != string(capture.bytes) || sha256Hex(encoded) != capture.digest ||
			capture.id != economicid.DeterministicUUID(providerCaptureDomain, providerCaptureSchemaV1+"@sha256:"+capture.digest) {
			return fmt.Errorf("provider capture mutable fields differ from canonical evidence")
		}
	}
	if sha256Hex(snapshot.bytes) != snapshot.digest || snapshot.id != economicid.DeterministicUUID(stableSnapshotDomain, stableSnapshotSchemaV1+"@sha256:"+snapshot.digest) {
		return fmt.Errorf("stable provider snapshot identity is invalid")
	}
	return nil
}

func ValidateStableProviderSnapshot(snapshot *StableProviderSnapshot) error {
	return validateStableProviderSnapshot(snapshot)
}

func validateLocalSnapshot(snapshot *LocalSnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("local snapshot is required")
	}
	encoded, err := json.Marshal(snapshot.canonical)
	if err != nil || string(encoded) != string(snapshot.bytes) || sha256Hex(encoded) != snapshot.digest ||
		snapshot.id != economicid.DeterministicUUID(localSnapshotDomain, localSnapshotSchemaV1+"@sha256:"+snapshot.digest) {
		return fmt.Errorf("local snapshot mutable fields differ from canonical evidence")
	}
	return nil
}

func ValidateLocalSnapshot(snapshot *LocalSnapshot) error { return validateLocalSnapshot(snapshot) }

func ValidateRun(run *Run) error {
	if run == nil || run.Schema != runSchemaV1 || run.ID == uuid.Nil || run.PolicyVersion == "" || run.LocalSnapshotID == uuid.Nil || len(run.Results) == 0 {
		return fmt.Errorf("venue reconciliation run envelope is invalid")
	}
	digest := sha256Hex(run.CanonicalBytes)
	if digest != run.SHA256 || run.ID != economicid.DeterministicUUID(runIDDomain, runSchemaV1+"@sha256:"+digest) ||
		run.Clean != (len(run.Incidents) == 0) {
		return fmt.Errorf("venue reconciliation run identity is invalid")
	}
	var payload struct {
		Schema             string     `json:"schema"`
		PolicyVersion      string     `json:"policy_version"`
		ProviderSnapshotID string     `json:"provider_snapshot_id"`
		LocalSnapshotID    string     `json:"local_snapshot_id"`
		Clean              bool       `json:"clean"`
		Results            []Result   `json:"results"`
		Incidents          []Incident `json:"incidents"`
	}
	if err := json.Unmarshal(run.CanonicalBytes, &payload); err != nil || payload.Schema != run.Schema || payload.PolicyVersion != run.PolicyVersion ||
		payload.ProviderSnapshotID != projectionUUIDString(run.ProviderSnapshotID) || payload.LocalSnapshotID != run.LocalSnapshotID.String() ||
		payload.Clean != run.Clean || !reflect.DeepEqual(payload.Results, run.Results) || !reflect.DeepEqual(payload.Incidents, run.Incidents) {
		return fmt.Errorf("venue reconciliation run bytes do not match envelope")
	}
	policy, err := NewPolicy(ReviewedPolicyV1Input())
	if err != nil || run.PolicyVersion != policy.Version() {
		return fmt.Errorf("venue reconciliation run policy is not the reviewed policy")
	}
	expectedIncidents := make([]Incident, 0, len(run.Incidents))
	for index, result := range run.Results {
		if result.Key == "" || (index > 0 && (run.Results[index-1].Key > result.Key ||
			(run.Results[index-1].Key == result.Key && run.Results[index-1].Reason >= result.Reason))) {
			return fmt.Errorf("venue reconciliation results are not uniquely ordered")
		}
		rule, ok := policy.Reason(result.Reason)
		if !ok || result.Kind != rule.Kind || result.Status != rule.Status || result.Severity != rule.Severity {
			return fmt.Errorf("venue reconciliation result policy fields are invalid")
		}
		expectedID, idErr := deterministicResultID(run.PolicyVersion, run.ProviderSnapshotID, run.LocalSnapshotID, result)
		if idErr != nil || result.ID != expectedID {
			return fmt.Errorf("venue reconciliation result identity is invalid")
		}
		if result.Status != StatusMatched {
			expectedIncidents = append(expectedIncidents, Incident{
				ID: economicid.DeterministicUUID(incidentIDDomain, result.ID.String()), ResultID: result.ID,
				Key: result.Key, Reason: result.Reason, Severity: result.Severity,
			})
		}
	}
	if !reflect.DeepEqual(expectedIncidents, run.Incidents) {
		return fmt.Errorf("venue reconciliation incidents do not match non-clean results")
	}
	return nil
}

// ValidatePersistableRun accepts only an unmodified run emitted by Compare.
// Reconstructed database evidence is deliberately readable but not writable.
func ValidatePersistableRun(run *Run) error {
	if err := ValidateRun(run); err != nil {
		return err
	}
	if run.generationSeal == "" || run.generationSeal != run.SHA256 {
		return fmt.Errorf("venue reconciliation run was not emitted by the comparer or was modified")
	}
	return nil
}

func deterministicResultID(policyVersion string, providerID, localID uuid.UUID, result Result) (uuid.UUID, error) {
	identity := struct {
		PolicyVersion      string         `json:"policy_version"`
		ProviderSnapshotID string         `json:"provider_snapshot_id"`
		LocalSnapshotID    string         `json:"local_snapshot_id"`
		Key                string         `json:"key"`
		Kind               ComparisonKind `json:"kind"`
		Status             ResultStatus   `json:"status"`
		Reason             ReasonCode     `json:"reason"`
		Severity           Severity       `json:"severity"`
		ProviderValue      *string        `json:"provider_value"`
		LocalValue         *string        `json:"local_value"`
		Delta              *string        `json:"delta"`
	}{
		policyVersion, projectionUUIDString(providerID), localID.String(), result.Key, result.Kind,
		result.Status, result.Reason, result.Severity, result.ProviderValue, result.LocalValue, result.Delta,
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return uuid.Nil, err
	}
	return economicid.DeterministicUUID(resultIDDomain, string(encoded)), nil
}

func RunFromCanonical(id uuid.UUID, digest string, canonical json.RawMessage) (*Run, error) {
	var payload struct {
		Schema             string     `json:"schema"`
		PolicyVersion      string     `json:"policy_version"`
		ProviderSnapshotID string     `json:"provider_snapshot_id"`
		LocalSnapshotID    string     `json:"local_snapshot_id"`
		Clean              bool       `json:"clean"`
		Results            []Result   `json:"results"`
		Incidents          []Incident `json:"incidents"`
	}
	if err := json.Unmarshal(canonical, &payload); err != nil {
		return nil, fmt.Errorf("decode venue reconciliation run: %w", err)
	}
	providerID := uuid.Nil
	var err error
	if payload.ProviderSnapshotID != "" {
		providerID, err = uuid.Parse(payload.ProviderSnapshotID)
		if err != nil {
			return nil, fmt.Errorf("decode provider snapshot ID: %w", err)
		}
	}
	localID, err := uuid.Parse(payload.LocalSnapshotID)
	if err != nil {
		return nil, fmt.Errorf("decode local snapshot ID: %w", err)
	}
	run := &Run{
		ID: id, Schema: payload.Schema, PolicyVersion: payload.PolicyVersion, ProviderSnapshotID: providerID,
		LocalSnapshotID: localID, Clean: payload.Clean, Results: payload.Results, Incidents: payload.Incidents,
		SHA256: digest, CanonicalBytes: append(json.RawMessage(nil), canonical...),
	}
	if err := ValidateRun(run); err != nil {
		return nil, err
	}
	return run, nil
}

func allMatched(results []Result) bool {
	for _, result := range results {
		if result.Status != StatusMatched {
			return false
		}
	}
	return true
}
func stringPointer(value string) *string { return &value }
func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func decimalDelta(provider, local *string) *string {
	if provider == nil || local == nil {
		return nil
	}
	return decimalDeltaForReason(ReasonPositionQuantityMismatch, *provider, *local)
}

func decimalDeltaForReason(reason ReasonCode, provider, local string) *string {
	if reason != ReasonFillQuantityMismatch && reason != ReasonFillPriceMismatch && reason != ReasonFillFeeMismatch && reason != ReasonPositionQuantityMismatch {
		return nil
	}
	providerValue, providerErr := decimal.NewFromString(provider)
	localValue, localErr := decimal.NewFromString(local)
	if providerErr != nil || localErr != nil {
		return nil
	}
	return stringPointer(providerValue.Sub(localValue).String())
}

func sortedUUIDKeys(values map[uuid.UUID]struct{}) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].String() < result[j].String() })
	return result
}

func providerComparisonKey(value ProviderFill) string {
	if value.ObservationClass == lifecycle.ObservationOrdinary {
		return "ordinary\x00" + value.SourceID
	}
	return "revision\x00" + value.OriginalSourceID + "\x00" + string(value.ObservationClass) + "\x00" + value.ObservationDiscriminator
}

func localComparisonKey(value LocalFillEvidence) string {
	if value.ObservationClass == lifecycle.ObservationOrdinary {
		return "ordinary\x00" + value.SourceID
	}
	return "revision\x00" + value.OriginalSourceID + "\x00" + string(value.ObservationClass) + "\x00" + value.ObservationDiscriminator
}

func scopeKey(capture *ProviderCapture) string {
	return strings.Join([]string{capture.canonical.AccountID, string(capture.canonical.Provider), capture.canonical.Namespace, capture.canonical.Currency, capture.canonical.HorizonStart, capture.canonical.HorizonEnd}, "/")
}

func localScopeKey(snapshot *LocalSnapshot) string {
	return strings.Join([]string{snapshot.canonical.AccountID, string(snapshot.canonical.Provider), snapshot.canonical.Namespace, snapshot.canonical.Currency, snapshot.canonical.HorizonStart, snapshot.canonical.HorizonEnd}, "/")
}

// Runner composes only read-only evidence sources and the pure comparer.
type Runner struct {
	provider CaptureReader
	local    *LocalSource
	policy   *Policy
}

func NewRunner(policy *Policy, provider CaptureReader, local *LocalSource) *Runner {
	return &Runner{policy: policy, provider: provider, local: local}
}

func (runner *Runner) Run(ctx context.Context, request LocalSnapshotRequest) (*Run, error) {
	if runner == nil || runner.policy == nil || runner.provider == nil || runner.local == nil {
		return nil, fmt.Errorf("reconciliation runner dependencies are required")
	}
	provider, err := CaptureTwice(ctx, runner.provider)
	if err != nil {
		return nil, err
	}
	local, err := runner.local.Capture(ctx, request)
	if err != nil {
		return nil, err
	}
	return Compare(CompareInput{Policy: runner.policy, Provider: provider, Local: local})
}
