// Package experimentrun owns deterministic execution plans and run evidence
// for declared research experiments. It does not approve or activate strategy
// deployments.
package experimentrun

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

const (
	ProgramSchemaV1 = "experiment-program-v1"
	programDomain   = "experiment-program"
)

var (
	digestPattern       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	sourceCommitPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
)

type ProgramIdentityInput struct {
	VersionID        uuid.UUID
	VersionSHA256    string
	CompilerKind     string
	CompilerVersion  string
	SourceCommit     string
	SourceTreeSHA256 string
	DecisionContract string
	AdapterKind      string
	AdapterVersion   string
	AdapterSHA256    string
	RunnerContract   string
}

type programCanonical struct {
	Schema           string `json:"schema"`
	VersionID        string `json:"version_id"`
	VersionSHA256    string `json:"version_sha256"`
	CompilerKind     string `json:"compiler_kind"`
	CompilerVersion  string `json:"compiler_version"`
	SourceCommit     string `json:"source_commit"`
	SourceTreeSHA256 string `json:"source_tree_sha256"`
	DecisionContract string `json:"decision_contract"`
	AdapterKind      string `json:"adapter_kind"`
	AdapterVersion   string `json:"adapter_version"`
	AdapterSHA256    string `json:"adapter_sha256"`
	RunnerContract   string `json:"runner_contract"`
}

type ProgramIdentity struct {
	canonical programCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

// Program receives only a validated immutable input and returns a complete
// canonical replay plan. Implementations are registered explicitly by exact
// ProgramIdentity; the runner never discovers or compiles code dynamically.
type Program interface {
	Identity() *ProgramIdentity
	Plan(context.Context, ProgramInput) (*Plan, error)
}

type ProgramInput struct {
	ExperimentID    uuid.UUID
	ManifestID      uuid.UUID
	ManifestSHA256  string
	EvaluationStart string
	EvaluationEnd   string
	Seed            int64
	Mode            strategycatalog.ExperimentMode
	Evidence        []ObservationEvidence
}

type ObservationEvidence struct {
	PartitionContentSHA256 string
	SourceKey              string
	ContentSHA256          string
	AvailableAt            string
}

func NewProgramIdentity(input ProgramIdentityInput) (*ProgramIdentity, error) {
	if input.VersionID == uuid.Nil || !digestPattern.MatchString(input.VersionSHA256) ||
		!canonicalText(input.CompilerKind, 128) || !canonicalText(input.CompilerVersion, 256) ||
		!sourceCommitPattern.MatchString(input.SourceCommit) || !digestPattern.MatchString(input.SourceTreeSHA256) ||
		!canonicalText(input.DecisionContract, 256) || !canonicalText(input.AdapterKind, 128) ||
		!canonicalText(input.AdapterVersion, 256) || !digestPattern.MatchString(input.AdapterSHA256) ||
		!canonicalText(input.RunnerContract, 256) {
		return nil, fmt.Errorf("experiment program identity is invalid")
	}
	canonical := programCanonical{
		Schema: ProgramSchemaV1, VersionID: input.VersionID.String(), VersionSHA256: input.VersionSHA256,
		CompilerKind: input.CompilerKind, CompilerVersion: input.CompilerVersion, SourceCommit: input.SourceCommit,
		SourceTreeSHA256: input.SourceTreeSHA256, DecisionContract: input.DecisionContract,
		AdapterKind: input.AdapterKind, AdapterVersion: input.AdapterVersion, AdapterSHA256: input.AdapterSHA256,
		RunnerContract: input.RunnerContract,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, err
	}
	digest := hashBytes(encoded)
	return &ProgramIdentity{
		canonical: canonical, bytes: encoded, digest: digest,
		id: economicid.DeterministicUUID(programDomain, ProgramSchemaV1+"@sha256:"+digest),
	}, nil
}

func ProgramIdentityFromCanonical(id uuid.UUID, digest string, raw []byte) (*ProgramIdentity, error) {
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hashBytes(raw) != digest {
		return nil, fmt.Errorf("experiment program envelope is invalid")
	}
	var canonical programCanonical
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
	program, err := NewProgramIdentity(ProgramIdentityInput{
		VersionID: versionID, VersionSHA256: canonical.VersionSHA256, CompilerKind: canonical.CompilerKind,
		CompilerVersion: canonical.CompilerVersion, SourceCommit: canonical.SourceCommit,
		SourceTreeSHA256: canonical.SourceTreeSHA256, DecisionContract: canonical.DecisionContract,
		AdapterKind: canonical.AdapterKind, AdapterVersion: canonical.AdapterVersion,
		AdapterSHA256: canonical.AdapterSHA256, RunnerContract: canonical.RunnerContract,
	})
	if err != nil {
		return nil, err
	}
	if canonical.Schema != ProgramSchemaV1 || program.ID() != id || program.Digest() != digest || !bytes.Equal(program.bytes, raw) {
		return nil, fmt.Errorf("experiment program canonical identity does not reconstruct")
	}
	return program, nil
}

func (program *ProgramIdentity) ID() uuid.UUID {
	if program == nil {
		return uuid.Nil
	}
	return program.id
}

func (program *ProgramIdentity) Digest() string {
	if program == nil {
		return ""
	}
	return program.digest
}

func (program *ProgramIdentity) CanonicalBytes() json.RawMessage {
	if program == nil {
		return nil
	}
	return append(json.RawMessage(nil), program.bytes...)
}

func (program *ProgramIdentity) VersionID() uuid.UUID {
	if program == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(program.canonical.VersionID)
	return id
}

func (program *ProgramIdentity) VersionSHA256() string {
	if program == nil {
		return ""
	}
	return program.canonical.VersionSHA256
}

func (program *ProgramIdentity) CompilerKind() string {
	if program == nil {
		return ""
	}
	return program.canonical.CompilerKind
}

func (program *ProgramIdentity) CompilerVersion() string {
	if program == nil {
		return ""
	}
	return program.canonical.CompilerVersion
}

func (program *ProgramIdentity) SourceCommit() string {
	if program == nil {
		return ""
	}
	return program.canonical.SourceCommit
}

func (program *ProgramIdentity) SourceTreeSHA256() string {
	if program == nil {
		return ""
	}
	return program.canonical.SourceTreeSHA256
}

func (program *ProgramIdentity) DecisionContract() string {
	if program == nil {
		return ""
	}
	return program.canonical.DecisionContract
}

func (program *ProgramIdentity) AdapterKind() string {
	if program == nil {
		return ""
	}
	return program.canonical.AdapterKind
}

func (program *ProgramIdentity) AdapterVersion() string {
	if program == nil {
		return ""
	}
	return program.canonical.AdapterVersion
}

func (program *ProgramIdentity) AdapterSHA256() string {
	if program == nil {
		return ""
	}
	return program.canonical.AdapterSHA256
}

func (program *ProgramIdentity) RunnerContract() string {
	if program == nil {
		return ""
	}
	return program.canonical.RunnerContract
}

func canonicalText(value string, maximum int) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= maximum
}

func hashBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("canonical JSON contains multiple values")
		}
		return err
	}
	return nil
}
