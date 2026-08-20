package dataset

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const (
	QualityResultSchemaV1 = "dataset-quality-result-v1"
	qualityResultDomain   = "dataset-quality-result"
)

type CheckStatus string

const (
	CheckPassed      CheckStatus = "passed"
	CheckFailed      CheckStatus = "failed"
	CheckNotAssessed CheckStatus = "not_assessed"
)

type InstrumentWindow struct {
	InstrumentID   uuid.UUID
	ValidFrom      time.Time
	ValidTo        *time.Time
	EvidenceSHA256 string
}

type SessionEvidence struct {
	PartitionContentSHA256 string
	ExpectedEffectiveAt    []time.Time
	EvidenceSHA256         string
}

type ExternalAssessment struct {
	PartitionContentSHA256 string
	Check                  CheckCode
	Status                 CheckStatus
	EvidenceSHA256         string
}

type QualityInput struct {
	Policy              *Policy
	Manifest            *Manifest
	InstrumentWindows   []InstrumentWindow
	Sessions            []SessionEvidence
	ExternalAssessments []ExternalAssessment
}

type CheckResult struct {
	Key                    string      `json:"key"`
	PartitionContentSHA256 string      `json:"partition_content_sha256"`
	Kind                   Kind        `json:"kind"`
	Check                  CheckCode   `json:"check"`
	Required               bool        `json:"required"`
	Status                 CheckStatus `json:"status"`
	Severity               Severity    `json:"severity"`
	EvidenceSHA256         string      `json:"evidence_sha256"`
}

type Finding struct {
	Key                    string    `json:"key"`
	PartitionContentSHA256 string    `json:"partition_content_sha256"`
	Check                  CheckCode `json:"check"`
	Code                   string    `json:"code"`
	Severity               Severity  `json:"severity"`
	Evidence               []string  `json:"evidence"`
}

type qualityCanonical struct {
	Schema        string        `json:"schema"`
	PolicyVersion string        `json:"policy_version"`
	ManifestID    string        `json:"manifest_id"`
	Quarantined   bool          `json:"quarantined"`
	CheckCount    int           `json:"check_count"`
	FindingCount  int           `json:"finding_count"`
	Checks        []CheckResult `json:"checks"`
	Findings      []Finding     `json:"findings"`
}

type QualityResult struct {
	canonical qualityCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func Evaluate(input QualityInput) (*QualityResult, error) {
	if input.Policy == nil || input.Manifest == nil {
		return nil, fmt.Errorf("dataset quality policy and manifest are required")
	}
	policy, err := NewPolicy(PolicyInput{Schema: input.Policy.schema, Kinds: input.Policy.Kinds(), Rules: input.Policy.Rules()})
	if err != nil || policy.ID() != input.Policy.ID() || !bytes.Equal(policy.CanonicalBytes(), input.Policy.CanonicalBytes()) {
		return nil, fmt.Errorf("dataset quality policy is invalid")
	}
	manifest, err := ManifestFromCanonical(input.Manifest.ID(), input.Manifest.Digest(), input.Manifest.CanonicalBytes())
	if err != nil || manifest.ID() != input.Manifest.ID() {
		return nil, fmt.Errorf("dataset quality manifest is invalid")
	}
	windows, err := normalizeInstrumentWindows(input.InstrumentWindows)
	if err != nil {
		return nil, err
	}
	sessions, err := normalizeSessions(input.Sessions)
	if err != nil {
		return nil, err
	}
	assessments, err := normalizeAssessments(input.ExternalAssessments)
	if err != nil {
		return nil, err
	}

	checks := make([]CheckResult, 0)
	findings := make([]Finding, 0)
	for _, partition := range manifest.Partitions() {
		for _, rule := range policy.Rules() {
			if !kindIncluded(rule.Kinds, partition.Kind) {
				continue
			}
			check, generated := evaluateCheck(partition, rule, windows, sessions, assessments)
			checks = append(checks, check)
			findings = append(findings, generated...)
		}
	}
	sort.Slice(checks, func(i, j int) bool { return checks[i].Key < checks[j].Key })
	sort.Slice(findings, func(i, j int) bool { return findings[i].Key < findings[j].Key })
	quarantined := false
	for _, check := range checks {
		if check.Status == CheckFailed || check.Required && check.Status == CheckNotAssessed {
			quarantined = true
			break
		}
	}
	canonical := qualityCanonical{
		Schema: QualityResultSchemaV1, PolicyVersion: policy.Version(), ManifestID: manifest.ID().String(),
		Quarantined: quarantined, CheckCount: len(checks), FindingCount: len(findings), Checks: checks, Findings: findings,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal dataset quality result: %w", err)
	}
	digest := hashBytes(encoded)
	return &QualityResult{
		canonical: canonical, bytes: encoded, digest: digest,
		id: economicid.DeterministicUUID(qualityResultDomain, QualityResultSchemaV1+"@sha256:"+digest),
	}, nil
}

func evaluateCheck(partition Partition, rule CheckRule, windows map[uuid.UUID][]InstrumentWindow, sessions map[string]SessionEvidence, assessments map[string]ExternalAssessment) (CheckResult, []Finding) {
	scopeKey := partition.ContentSHA256 + "\x00" + string(rule.Code)
	key := hashBytes([]byte(scopeKey))
	result := CheckResult{Key: key, PartitionContentSHA256: partition.ContentSHA256, Kind: partition.Kind, Check: rule.Code, Required: rule.Required, Status: CheckPassed, Severity: rule.Severity}
	switch rule.Code {
	case CheckSessionCoverage:
		evidence, ok := sessions[partition.ContentSHA256]
		if !ok {
			return notAssessed(result)
		}
		result.EvidenceSHA256 = evidence.EvidenceSHA256
		actual := make(map[string]struct{}, len(partition.Observations))
		for _, observation := range partition.Observations {
			actual[observation.EffectiveAt] = struct{}{}
		}
		missing := make([]string, 0)
		for _, expected := range evidence.ExpectedEffectiveAt {
			formatted := formatTime(expected)
			if _, ok := actual[formatted]; !ok {
				missing = append(missing, formatted)
			}
		}
		if len(missing) > 0 {
			result.Status = CheckFailed
			return result, []Finding{newFinding(result, "missing_session", missing)}
		}
	case CheckInstrumentValidity:
		missingEvidence, invalid := make([]string, 0), make([]string, 0)
		for _, observation := range partition.Observations {
			if observation.InstrumentID == "" {
				invalid = append(invalid, observation.SourceKey+":missing_instrument")
				continue
			}
			instrumentID, _ := uuid.Parse(observation.InstrumentID)
			candidates := windows[instrumentID]
			if len(candidates) == 0 {
				missingEvidence = append(missingEvidence, observation.SourceKey+":"+observation.InstrumentID)
				continue
			}
			effectiveAt := parseTime(observation.EffectiveAt)
			valid := false
			for _, window := range candidates {
				if !effectiveAt.Before(window.ValidFrom) && (window.ValidTo == nil || effectiveAt.Before(*window.ValidTo)) {
					valid = true
					result.EvidenceSHA256 = window.EvidenceSHA256
					break
				}
			}
			if !valid {
				invalid = append(invalid, observation.SourceKey+":"+observation.InstrumentID)
			}
		}
		if len(invalid) > 0 {
			result.Status = CheckFailed
			return result, []Finding{newFinding(result, "invalid_instrument_window", invalid)}
		}
		if len(missingEvidence) > 0 {
			result.Status = CheckNotAssessed
			return result, []Finding{newFinding(result, "instrument_window_not_assessed", missingEvidence)}
		}
	case CheckCorporateActions, CheckProviderSpotCompare:
		assessment, ok := assessments[scopeKey]
		if !ok {
			return notAssessed(result)
		}
		result.Status, result.EvidenceSHA256 = assessment.Status, assessment.EvidenceSHA256
		if assessment.Status == CheckFailed {
			return result, []Finding{newFinding(result, "external_check_failed", []string{assessment.EvidenceSHA256})}
		}
	default:
		result.EvidenceSHA256 = partition.ContentSHA256
	}
	return result, nil
}

func notAssessed(result CheckResult) (CheckResult, []Finding) {
	result.Status = CheckNotAssessed
	code := "check_not_assessed"
	if result.Required {
		code = "required_check_not_assessed"
	}
	return result, []Finding{newFinding(result, code, nil)}
}

func newFinding(result CheckResult, code string, evidence []string) Finding {
	cloned := append([]string(nil), evidence...)
	sort.Strings(cloned)
	return Finding{
		Key:                    hashBytes([]byte(result.Key + "\x00" + code + "\x00" + strings.Join(cloned, "\x00"))),
		PartitionContentSHA256: result.PartitionContentSHA256, Check: result.Check, Code: code,
		Severity: result.Severity, Evidence: cloned,
	}
}

func QualityResultFromCanonical(id uuid.UUID, digest string, raw []byte) (*QualityResult, error) {
	if id == uuid.Nil || !sha256Pattern.MatchString(digest) || hashBytes(raw) != digest {
		return nil, fmt.Errorf("dataset quality result envelope is invalid")
	}
	var canonical qualityCanonical
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	if canonical.Schema != QualityResultSchemaV1 || canonical.CheckCount != len(canonical.Checks) || canonical.FindingCount != len(canonical.Findings) ||
		id != economicid.DeterministicUUID(qualityResultDomain, QualityResultSchemaV1+"@sha256:"+digest) {
		return nil, fmt.Errorf("dataset quality result identity is invalid")
	}
	if !strings.HasPrefix(canonical.PolicyVersion, PolicySchemaV1+"@sha256:") {
		return nil, fmt.Errorf("dataset quality policy version is invalid")
	}
	if _, err := uuid.Parse(canonical.ManifestID); err != nil {
		return nil, fmt.Errorf("dataset quality manifest identity is invalid")
	}
	policy, _ := NewPolicy(ReviewedPolicyV1Input())
	quarantined := false
	seenChecks, seenFindings := map[string]struct{}{}, map[string]struct{}{}
	checkByKey := make(map[string]CheckResult, len(canonical.Checks))
	for index, check := range canonical.Checks {
		if index > 0 && canonical.Checks[index-1].Key >= check.Key {
			return nil, fmt.Errorf("dataset quality checks are not canonically ordered")
		}
		rule, ok := policyRule(policy, check.Kind, check.Check)
		if _, duplicate := seenChecks[check.Key]; duplicate || !ok || !validCheckResult(check) ||
			check.Required != rule.Required || check.Severity != rule.Severity {
			return nil, fmt.Errorf("dataset quality check graph is invalid")
		}
		seenChecks[check.Key] = struct{}{}
		checkByKey[check.Key] = check
		if check.Status == CheckFailed || check.Required && check.Status == CheckNotAssessed {
			quarantined = true
		}
	}
	for index, finding := range canonical.Findings {
		if index > 0 && canonical.Findings[index-1].Key >= finding.Key {
			return nil, fmt.Errorf("dataset quality findings are not canonically ordered")
		}
		checkKey := hashBytes([]byte(finding.PartitionContentSHA256 + "\x00" + string(finding.Check)))
		check, checkOK := checkByKey[checkKey]
		expectedKey := hashBytes([]byte(checkKey + "\x00" + finding.Code + "\x00" + strings.Join(finding.Evidence, "\x00")))
		if _, duplicate := seenFindings[finding.Key]; duplicate || !checkOK || check.Status == CheckPassed ||
			finding.Key != expectedKey || finding.Severity != check.Severity || finding.Code == "" || !sort.StringsAreSorted(finding.Evidence) {
			return nil, fmt.Errorf("dataset quality finding graph is invalid")
		}
		seenFindings[finding.Key] = struct{}{}
	}
	if len(canonical.Findings) != countNonpassing(canonical.Checks) {
		return nil, fmt.Errorf("dataset quality finding count does not cover nonpassing checks")
	}
	if quarantined != canonical.Quarantined {
		return nil, fmt.Errorf("dataset quality quarantine state is invalid")
	}
	encoded, _ := json.Marshal(canonical)
	if !bytes.Equal(encoded, raw) {
		return nil, fmt.Errorf("dataset quality canonical bytes do not reconstruct")
	}
	return &QualityResult{canonical: canonical, bytes: append(json.RawMessage(nil), raw...), digest: digest, id: id}, nil
}

func (result *QualityResult) ID() uuid.UUID {
	if result == nil {
		return uuid.Nil
	}
	return result.id
}
func (result *QualityResult) Digest() string {
	if result == nil {
		return ""
	}
	return result.digest
}
func (result *QualityResult) CanonicalBytes() json.RawMessage {
	if result == nil {
		return nil
	}
	return append(json.RawMessage(nil), result.bytes...)
}
func (result *QualityResult) ManifestID() uuid.UUID {
	if result == nil {
		return uuid.Nil
	}
	value, _ := uuid.Parse(result.canonical.ManifestID)
	return value
}
func (result *QualityResult) PolicyVersion() string {
	if result == nil {
		return ""
	}
	return result.canonical.PolicyVersion
}
func (result *QualityResult) Quarantined() bool { return result != nil && result.canonical.Quarantined }
func (result *QualityResult) Checks() []CheckResult {
	if result == nil {
		return nil
	}
	return append([]CheckResult(nil), result.canonical.Checks...)
}
func (result *QualityResult) Findings() []Finding {
	if result == nil {
		return nil
	}
	values := append([]Finding(nil), result.canonical.Findings...)
	for index := range values {
		values[index].Evidence = append([]string(nil), values[index].Evidence...)
	}
	return values
}

func normalizeInstrumentWindows(values []InstrumentWindow) (map[uuid.UUID][]InstrumentWindow, error) {
	result := make(map[uuid.UUID][]InstrumentWindow)
	seen := make(map[string]struct{})
	for _, value := range values {
		if value.InstrumentID == uuid.Nil || !canonicalTimeValue(value.ValidFrom) || !sha256Pattern.MatchString(value.EvidenceSHA256) ||
			value.ValidTo != nil && (!canonicalTimeValue(*value.ValidTo) || !value.ValidFrom.Before(*value.ValidTo)) {
			return nil, fmt.Errorf("dataset instrument window is invalid")
		}
		key := value.InstrumentID.String() + "\x00" + formatTime(value.ValidFrom)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("dataset instrument window is duplicated")
		}
		seen[key] = struct{}{}
		if value.ValidTo != nil {
			copy := *value.ValidTo
			value.ValidTo = &copy
		}
		result[value.InstrumentID] = append(result[value.InstrumentID], value)
	}
	return result, nil
}

func normalizeSessions(values []SessionEvidence) (map[string]SessionEvidence, error) {
	result := make(map[string]SessionEvidence, len(values))
	for _, value := range values {
		if !sha256Pattern.MatchString(value.PartitionContentSHA256) || !sha256Pattern.MatchString(value.EvidenceSHA256) || len(value.ExpectedEffectiveAt) == 0 {
			return nil, fmt.Errorf("dataset session evidence is invalid")
		}
		if _, ok := result[value.PartitionContentSHA256]; ok {
			return nil, fmt.Errorf("dataset session evidence is duplicated")
		}
		seen := make(map[string]struct{}, len(value.ExpectedEffectiveAt))
		for _, timestamp := range value.ExpectedEffectiveAt {
			if !canonicalTimeValue(timestamp) {
				return nil, fmt.Errorf("dataset expected session is invalid")
			}
			key := formatTime(timestamp)
			if _, ok := seen[key]; ok {
				return nil, fmt.Errorf("dataset expected session is duplicated")
			}
			seen[key] = struct{}{}
		}
		value.ExpectedEffectiveAt = append([]time.Time(nil), value.ExpectedEffectiveAt...)
		sort.Slice(value.ExpectedEffectiveAt, func(i, j int) bool { return value.ExpectedEffectiveAt[i].Before(value.ExpectedEffectiveAt[j]) })
		result[value.PartitionContentSHA256] = value
	}
	return result, nil
}

func normalizeAssessments(values []ExternalAssessment) (map[string]ExternalAssessment, error) {
	result := make(map[string]ExternalAssessment, len(values))
	for _, value := range values {
		if !sha256Pattern.MatchString(value.PartitionContentSHA256) || !sha256Pattern.MatchString(value.EvidenceSHA256) ||
			(value.Check != CheckCorporateActions && value.Check != CheckProviderSpotCompare) || (value.Status != CheckPassed && value.Status != CheckFailed) {
			return nil, fmt.Errorf("dataset external assessment is invalid")
		}
		key := value.PartitionContentSHA256 + "\x00" + string(value.Check)
		if _, ok := result[key]; ok {
			return nil, fmt.Errorf("dataset external assessment is duplicated")
		}
		result[key] = value
	}
	return result, nil
}

func kindIncluded(values []Kind, want Kind) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func validCheckResult(value CheckResult) bool {
	return value.Key == hashBytes([]byte(value.PartitionContentSHA256+"\x00"+string(value.Check))) && sha256Pattern.MatchString(value.Key) && sha256Pattern.MatchString(value.PartitionContentSHA256) && validKind(value.Kind) &&
		(value.Status == CheckPassed || value.Status == CheckFailed || value.Status == CheckNotAssessed) && (value.Severity == SeverityHigh || value.Severity == SeverityCritical) &&
		(value.EvidenceSHA256 == "" || sha256Pattern.MatchString(value.EvidenceSHA256))
}

func policyRule(policy *Policy, kind Kind, check CheckCode) (CheckRule, bool) {
	for _, rule := range policy.Rules() {
		if rule.Code == check && kindIncluded(rule.Kinds, kind) {
			return rule, true
		}
	}
	return CheckRule{}, false
}

func countNonpassing(values []CheckResult) int {
	count := 0
	for _, value := range values {
		if value.Status != CheckPassed {
			count++
		}
	}
	return count
}
