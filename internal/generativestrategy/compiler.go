package generativestrategy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

const (
	CompilerKindV1     = "typed-generative"
	CompilerVersionV1  = "typed-generative-compiler-v1"
	ConfigSchemaV1     = "typed-generative-strategy-config-v1"
	DecisionContractV1 = "typed-generative-decision-v1"
	ReceiptSchemaV1    = "typed-generative-compilation-receipt-v1"
)

var sourceCommitPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

type compiledConfig struct {
	Schema                string              `json:"schema"`
	SpecID                string              `json:"spec_id"`
	SpecSHA256            string              `json:"spec_sha256"`
	Inputs                []inputCanonical    `json:"inputs"`
	Universe              universeCanonical   `json:"universe"`
	Entry                 exprCanonical       `json:"entry"`
	Exit                  exprCanonical       `json:"exit"`
	Sizing                sizingCanonical     `json:"sizing"`
	MaximumHoldingSeconds int64               `json:"maximum_holding_seconds"`
	Costs                 costsCanonical      `json:"costs"`
	Capacity              capacityCanonical   `json:"capacity"`
	ProhibitedBehaviors   []string            `json:"prohibited_behaviors"`
	PropertyTests         []string            `json:"property_tests"`
	ExampleTests          []exampleCanonical  `json:"example_tests"`
	Retirement            retirementCanonical `json:"retirement"`
	Authoring             authoringCanonical  `json:"authoring"`
}

type receiptCanonical struct {
	Schema           string `json:"schema"`
	State            string `json:"state"`
	FamilyID         string `json:"family_id"`
	FamilySHA256     string `json:"family_sha256"`
	SpecID           string `json:"spec_id"`
	SpecSHA256       string `json:"spec_sha256"`
	VersionID        string `json:"version_id"`
	VersionSHA256    string `json:"version_sha256"`
	CompilerKind     string `json:"compiler_kind"`
	CompilerVersion  string `json:"compiler_version"`
	SourceCommit     string `json:"source_commit"`
	SourceTreeSHA256 string `json:"source_tree_sha256"`
	ConfigSchema     string `json:"config_schema"`
	DecisionContract string `json:"decision_contract"`
	ConfigSHA256     string `json:"config_sha256"`
}

type Receipt struct {
	canonical receiptCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func Compile(spec *Spec, sourceCommit, sourceTreeSHA256 string) (*strategycatalog.Version, *Receipt, error) {
	if spec == nil || !sourceCommitPattern.MatchString(sourceCommit) || !digestPattern.MatchString(sourceTreeSHA256) {
		return nil, nil, fmt.Errorf("generated strategy compiler identity is invalid")
	}
	config := compiledConfig{
		Schema: ConfigSchemaV1, SpecID: spec.ID().String(), SpecSHA256: spec.Digest(),
		Inputs: append([]inputCanonical(nil), spec.canonical.Inputs...), Universe: spec.canonical.Universe,
		Entry: spec.canonical.Entry, Exit: spec.canonical.Exit, Sizing: spec.canonical.Sizing,
		MaximumHoldingSeconds: spec.canonical.MaximumHoldingSeconds, Costs: spec.canonical.Costs,
		Capacity: spec.canonical.Capacity, ProhibitedBehaviors: append([]string(nil), spec.canonical.ProhibitedBehaviors...),
		PropertyTests: append([]string(nil), spec.canonical.PropertyTests...),
		ExampleTests:  append([]exampleCanonical(nil), spec.canonical.ExampleTests...),
		Retirement:    spec.canonical.Retirement, Authoring: spec.canonical.Authoring,
	}
	configBytes, err := canonicalConfigBytes(config)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal generated strategy config: %w", err)
	}
	version, err := strategycatalog.NewVersion(strategycatalog.VersionInput{
		FamilyID: spec.FamilyID(), CompilerKind: CompilerKindV1, CompilerVersion: CompilerVersionV1,
		SourceCommit: sourceCommit, SourceTreeSHA256: sourceTreeSHA256, ConfigSchema: ConfigSchemaV1,
		Config: configBytes, DecisionContract: DecisionContractV1, RequiredDatasetKinds: spec.RequiredDatasetKinds(),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("compile generated strategy version: %w", err)
	}
	canonical := receiptCanonical{
		Schema: ReceiptSchemaV1, State: "compiled", FamilyID: spec.canonical.FamilyID,
		FamilySHA256: spec.canonical.FamilySHA256, SpecID: spec.ID().String(), SpecSHA256: spec.Digest(),
		VersionID: version.ID().String(), VersionSHA256: version.Digest(), CompilerKind: CompilerKindV1,
		CompilerVersion: CompilerVersionV1, SourceCommit: sourceCommit, SourceTreeSHA256: sourceTreeSHA256,
		ConfigSchema: ConfigSchemaV1, DecisionContract: DecisionContractV1, ConfigSHA256: hash(configBytes),
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal generated strategy receipt: %w", err)
	}
	digest := hash(encoded)
	receipt := &Receipt{canonical, encoded, digest, economicid.DeterministicUUID("typed-generative-compilation-receipt", ReceiptSchemaV1+"@sha256:"+digest)}
	return version, receipt, nil
}

func canonicalConfigBytes(value compiledConfig) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(&object); err != nil {
		return nil, err
	}
	return json.Marshal(object)
}

func ReceiptFromCanonical(id uuid.UUID, digest string, raw []byte, spec *Spec, version *strategycatalog.Version) (*Receipt, error) {
	var canonical receiptCanonical
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if id == uuid.Nil || spec == nil || version == nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decoder.Decode(&canonical) != nil {
		return nil, fmt.Errorf("generated strategy receipt envelope is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("generated strategy receipt has extra JSON")
	}
	rebuiltVersion, rebuilt, err := Compile(spec, canonical.SourceCommit, canonical.SourceTreeSHA256)
	if err != nil || rebuiltVersion.ID() != version.ID() || rebuiltVersion.Digest() != version.Digest() || rebuilt.ID() != id || rebuilt.Digest() != digest || !bytes.Equal(rebuilt.bytes, raw) {
		return nil, fmt.Errorf("generated strategy receipt does not reconstruct")
	}
	return rebuilt, nil
}

func (r *Receipt) ID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return r.id
}

func (r *Receipt) Digest() string {
	if r == nil {
		return ""
	}
	return r.digest
}

func (r *Receipt) CanonicalBytes() json.RawMessage {
	if r == nil {
		return nil
	}
	return append(json.RawMessage(nil), r.bytes...)
}

func (r *Receipt) SpecID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return uuid.MustParse(r.canonical.SpecID)
}

func (r *Receipt) VersionID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return uuid.MustParse(r.canonical.VersionID)
}
