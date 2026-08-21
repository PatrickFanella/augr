package strategycatalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/capital"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
)

const (
	ExperimentSchemaV1  = "research-experiment-v1"
	experimentDomain    = "research-experiment"
	ExperimentDeclared  = "declared"
	canonicalTimeLayout = "2006-01-02T15:04:05.000000Z"
)

type ExperimentMode string

const (
	ExperimentPaperScored ExperimentMode = "paper_scored"
	ExperimentPaperStress ExperimentMode = "paper_stress"
)

type ExperimentInput struct {
	VersionID               uuid.UUID
	AccountID               uuid.UUID
	CapitalBindingID        uuid.UUID
	ManifestID              uuid.UUID
	QualityResultID         uuid.UUID
	SimulationPolicyVersion string
	CapitalPolicyVersion    string
	Mode                    ExperimentMode
	EvaluationStart         time.Time
	EvaluationEnd           time.Time
	Seed                    int64
	DatasetQuarantined      bool
}

type experimentCanonical struct {
	Schema                  string         `json:"schema"`
	State                   string         `json:"state"`
	VersionID               string         `json:"version_id"`
	AccountID               string         `json:"account_id"`
	CapitalBindingID        string         `json:"capital_binding_id"`
	ManifestID              string         `json:"manifest_id"`
	QualityResultID         string         `json:"quality_result_id"`
	SimulationPolicyVersion string         `json:"simulation_policy_version"`
	CapitalPolicyVersion    string         `json:"capital_policy_version"`
	Mode                    ExperimentMode `json:"mode"`
	EvaluationStart         string         `json:"evaluation_start"`
	EvaluationEnd           string         `json:"evaluation_end"`
	Seed                    int64          `json:"seed"`
	DatasetQuarantined      bool           `json:"dataset_quarantined"`
}

type Experiment struct {
	canonical experimentCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewExperiment(input ExperimentInput) (*Experiment, error) {
	if input.VersionID == uuid.Nil || input.AccountID == uuid.Nil || input.CapitalBindingID == uuid.Nil ||
		input.ManifestID == uuid.Nil || input.QualityResultID == uuid.Nil ||
		!policyVersion(input.SimulationPolicyVersion, simulation.PolicySchemaV1) ||
		!policyVersion(input.CapitalPolicyVersion, capital.PolicySchemaV1) ||
		!canonicalTime(input.EvaluationStart) || !canonicalTime(input.EvaluationEnd) ||
		!input.EvaluationStart.Before(input.EvaluationEnd) {
		return nil, fmt.Errorf("research experiment identity is invalid")
	}
	if input.Mode != ExperimentPaperScored && input.Mode != ExperimentPaperStress {
		return nil, fmt.Errorf("research experiment mode is invalid")
	}
	if input.Mode == ExperimentPaperScored && input.DatasetQuarantined {
		return nil, fmt.Errorf("scored experiment cannot admit a quarantined dataset")
	}
	canonical := experimentCanonical{
		Schema: ExperimentSchemaV1, State: ExperimentDeclared,
		VersionID: input.VersionID.String(), AccountID: input.AccountID.String(), CapitalBindingID: input.CapitalBindingID.String(),
		ManifestID: input.ManifestID.String(), QualityResultID: input.QualityResultID.String(),
		SimulationPolicyVersion: input.SimulationPolicyVersion, CapitalPolicyVersion: input.CapitalPolicyVersion,
		Mode: input.Mode, EvaluationStart: formatCanonicalTime(input.EvaluationStart), EvaluationEnd: formatCanonicalTime(input.EvaluationEnd),
		Seed: input.Seed, DatasetQuarantined: input.DatasetQuarantined,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal research experiment: %w", err)
	}
	digest := hashBytes(encoded)
	return &Experiment{
		canonical: canonical, bytes: encoded, digest: digest,
		id: economicid.DeterministicUUID(experimentDomain, ExperimentSchemaV1+"@sha256:"+digest),
	}, nil
}

func ExperimentFromCanonical(id uuid.UUID, digest string, raw []byte) (*Experiment, error) {
	if id == uuid.Nil || !sha256Pattern.MatchString(digest) || hashBytes(raw) != digest {
		return nil, fmt.Errorf("research experiment envelope is invalid")
	}
	var canonical experimentCanonical
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	versionID, err := uuid.Parse(canonical.VersionID)
	if err != nil {
		return nil, err
	}
	accountID, err := uuid.Parse(canonical.AccountID)
	if err != nil {
		return nil, err
	}
	bindingID, err := uuid.Parse(canonical.CapitalBindingID)
	if err != nil {
		return nil, err
	}
	manifestID, err := uuid.Parse(canonical.ManifestID)
	if err != nil {
		return nil, err
	}
	qualityID, err := uuid.Parse(canonical.QualityResultID)
	if err != nil {
		return nil, err
	}
	experiment, err := NewExperiment(ExperimentInput{
		VersionID: versionID, AccountID: accountID, CapitalBindingID: bindingID,
		ManifestID: manifestID, QualityResultID: qualityID,
		SimulationPolicyVersion: canonical.SimulationPolicyVersion, CapitalPolicyVersion: canonical.CapitalPolicyVersion,
		Mode: canonical.Mode, EvaluationStart: parseCanonicalTime(canonical.EvaluationStart), EvaluationEnd: parseCanonicalTime(canonical.EvaluationEnd),
		Seed: canonical.Seed, DatasetQuarantined: canonical.DatasetQuarantined,
	})
	if err != nil {
		return nil, err
	}
	if canonical.Schema != ExperimentSchemaV1 || canonical.State != ExperimentDeclared || experiment.ID() != id ||
		experiment.Digest() != digest || !bytes.Equal(experiment.bytes, raw) {
		return nil, fmt.Errorf("research experiment canonical identity does not reconstruct")
	}
	return experiment, nil
}

func (experiment *Experiment) ID() uuid.UUID {
	if experiment == nil {
		return uuid.Nil
	}
	return experiment.id
}

func (experiment *Experiment) Digest() string {
	if experiment == nil {
		return ""
	}
	return experiment.digest
}

func (experiment *Experiment) CanonicalBytes() json.RawMessage {
	if experiment == nil {
		return nil
	}
	return append(json.RawMessage(nil), experiment.bytes...)
}

func (experiment *Experiment) VersionID() uuid.UUID {
	return experimentUUID(experiment, func(value experimentCanonical) string { return value.VersionID })
}

func (experiment *Experiment) AccountID() uuid.UUID {
	return experimentUUID(experiment, func(value experimentCanonical) string { return value.AccountID })
}

func (experiment *Experiment) CapitalBindingID() uuid.UUID {
	return experimentUUID(experiment, func(value experimentCanonical) string { return value.CapitalBindingID })
}

func (experiment *Experiment) ManifestID() uuid.UUID {
	return experimentUUID(experiment, func(value experimentCanonical) string { return value.ManifestID })
}

func (experiment *Experiment) QualityResultID() uuid.UUID {
	return experimentUUID(experiment, func(value experimentCanonical) string { return value.QualityResultID })
}

func (experiment *Experiment) SimulationPolicyVersion() string {
	if experiment == nil {
		return ""
	}
	return experiment.canonical.SimulationPolicyVersion
}

func (experiment *Experiment) CapitalPolicyVersion() string {
	if experiment == nil {
		return ""
	}
	return experiment.canonical.CapitalPolicyVersion
}

func (experiment *Experiment) Mode() ExperimentMode {
	if experiment == nil {
		return ""
	}
	return experiment.canonical.Mode
}

func (experiment *Experiment) State() string {
	if experiment == nil {
		return ""
	}
	return experiment.canonical.State
}

func (experiment *Experiment) EvaluationStart() time.Time {
	if experiment == nil {
		return time.Time{}
	}
	return parseCanonicalTime(experiment.canonical.EvaluationStart)
}

func (experiment *Experiment) EvaluationEnd() time.Time {
	if experiment == nil {
		return time.Time{}
	}
	return parseCanonicalTime(experiment.canonical.EvaluationEnd)
}

func (experiment *Experiment) Seed() int64 {
	if experiment == nil {
		return 0
	}
	return experiment.canonical.Seed
}

func (experiment *Experiment) DatasetQuarantined() bool {
	return experiment != nil && experiment.canonical.DatasetQuarantined
}

func experimentUUID(experiment *Experiment, selectValue func(experimentCanonical) string) uuid.UUID {
	if experiment == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(selectValue(experiment.canonical))
	return id
}

func policyVersion(value, schema string) bool {
	return strings.HasPrefix(value, schema+"@sha256:") && len(value) == len(schema)+len("@sha256:")+64 && sha256Pattern.MatchString(strings.TrimPrefix(value, schema+"@sha256:"))
}

func canonicalTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Nanosecond()%1000 == 0
}
func formatCanonicalTime(value time.Time) string { return value.Format(canonicalTimeLayout) }
func parseCanonicalTime(value string) time.Time {
	parsed, _ := time.Parse(canonicalTimeLayout, value)
	return parsed
}
