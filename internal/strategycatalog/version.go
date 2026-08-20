package strategycatalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const (
	VersionSchemaV1 = "strategy-version-v1"
	versionDomain   = "strategy-version"
)

var (
	sourceCommitPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	negativeZeroPattern = regexp.MustCompile(`^-0(?:\.0+)?$`)
)

type VersionInput struct {
	FamilyID             uuid.UUID
	CompilerKind         string
	CompilerVersion      string
	SourceCommit         string
	SourceTreeSHA256     string
	ConfigSchema         string
	Config               json.RawMessage
	DecisionContract     string
	RequiredDatasetKinds []dataset.Kind
}

type versionCanonical struct {
	Schema               string          `json:"schema"`
	FamilyID             string          `json:"family_id"`
	CompilerKind         string          `json:"compiler_kind"`
	CompilerVersion      string          `json:"compiler_version"`
	SourceCommit         string          `json:"source_commit"`
	SourceTreeSHA256     string          `json:"source_tree_sha256"`
	ConfigSchema         string          `json:"config_schema"`
	Config               json.RawMessage `json:"config"`
	DecisionContract     string          `json:"decision_contract"`
	RequiredDatasetKinds []dataset.Kind  `json:"required_dataset_kinds"`
}

type Version struct {
	canonical versionCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewVersion(input VersionInput) (*Version, error) {
	if input.FamilyID == uuid.Nil || !canonicalText(input.CompilerKind, 128) ||
		!canonicalText(input.CompilerVersion, 256) || !sourceCommitPattern.MatchString(input.SourceCommit) ||
		!sha256Pattern.MatchString(input.SourceTreeSHA256) || !canonicalText(input.ConfigSchema, 256) ||
		!canonicalText(input.DecisionContract, 256) {
		return nil, fmt.Errorf("strategy version identity is invalid")
	}
	config, err := canonicalConfig(input.Config)
	if err != nil {
		return nil, err
	}
	kinds := append([]dataset.Kind(nil), input.RequiredDatasetKinds...)
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	if len(kinds) == 0 {
		return nil, fmt.Errorf("strategy version requires dataset kinds")
	}
	allowedKinds := reviewedDatasetKinds()
	for index, kind := range kinds {
		if _, ok := allowedKinds[kind]; !ok || index > 0 && kinds[index-1] == kind {
			return nil, fmt.Errorf("strategy version dataset kinds are invalid")
		}
	}
	canonical := versionCanonical{
		Schema: VersionSchemaV1, FamilyID: input.FamilyID.String(), CompilerKind: input.CompilerKind,
		CompilerVersion: input.CompilerVersion, SourceCommit: input.SourceCommit,
		SourceTreeSHA256: input.SourceTreeSHA256, ConfigSchema: input.ConfigSchema, Config: config,
		DecisionContract: input.DecisionContract, RequiredDatasetKinds: kinds,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal strategy version: %w", err)
	}
	digest := hashBytes(encoded)
	return &Version{
		canonical: canonical, bytes: encoded, digest: digest,
		id: economicid.DeterministicUUID(versionDomain, VersionSchemaV1+"@sha256:"+digest),
	}, nil
}

func VersionFromCanonical(id uuid.UUID, digest string, raw []byte) (*Version, error) {
	if id == uuid.Nil || !sha256Pattern.MatchString(digest) || hashBytes(raw) != digest {
		return nil, fmt.Errorf("strategy version envelope is invalid")
	}
	var canonical versionCanonical
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	familyID, err := uuid.Parse(canonical.FamilyID)
	if err != nil {
		return nil, fmt.Errorf("strategy version family identity is invalid")
	}
	version, err := NewVersion(VersionInput{
		FamilyID: familyID, CompilerKind: canonical.CompilerKind, CompilerVersion: canonical.CompilerVersion,
		SourceCommit: canonical.SourceCommit, SourceTreeSHA256: canonical.SourceTreeSHA256,
		ConfigSchema: canonical.ConfigSchema, Config: canonical.Config, DecisionContract: canonical.DecisionContract,
		RequiredDatasetKinds: canonical.RequiredDatasetKinds,
	})
	if err != nil {
		return nil, err
	}
	if canonical.Schema != VersionSchemaV1 || version.ID() != id || version.Digest() != digest || !bytes.Equal(version.bytes, raw) {
		return nil, fmt.Errorf("strategy version canonical identity does not reconstruct")
	}
	return version, nil
}

func (version *Version) ID() uuid.UUID {
	if version == nil {
		return uuid.Nil
	}
	return version.id
}

func (version *Version) Digest() string {
	if version == nil {
		return ""
	}
	return version.digest
}

func (version *Version) CanonicalBytes() json.RawMessage {
	if version == nil {
		return nil
	}
	return append(json.RawMessage(nil), version.bytes...)
}

func (version *Version) FamilyID() uuid.UUID {
	if version == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(version.canonical.FamilyID)
	return id
}

func (version *Version) RequiredDatasetKinds() []dataset.Kind {
	if version == nil {
		return nil
	}
	return append([]dataset.Kind(nil), version.canonical.RequiredDatasetKinds...)
}

func (version *Version) Config() json.RawMessage {
	if version == nil {
		return nil
	}
	return append(json.RawMessage(nil), version.canonical.Config...)
}

func (version *Version) CompilerKind() string {
	if version == nil {
		return ""
	}
	return version.canonical.CompilerKind
}

func (version *Version) CompilerVersion() string {
	if version == nil {
		return ""
	}
	return version.canonical.CompilerVersion
}

func (version *Version) SourceCommit() string {
	if version == nil {
		return ""
	}
	return version.canonical.SourceCommit
}

func (version *Version) SourceTreeSHA256() string {
	if version == nil {
		return ""
	}
	return version.canonical.SourceTreeSHA256
}

func (version *Version) ConfigSchema() string {
	if version == nil {
		return ""
	}
	return version.canonical.ConfigSchema
}

func (version *Version) DecisionContract() string {
	if version == nil {
		return ""
	}
	return version.canonical.DecisionContract
}

func canonicalConfig(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, fmt.Errorf("strategy version config is required and bounded")
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, fmt.Errorf("strategy version config must be a JSON object")
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	if !canonicalConfigNumbers(value) {
		return nil, fmt.Errorf("strategy version config numbers must use canonical non-exponent notation")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(encoded, raw) {
		return nil, fmt.Errorf("strategy version config must use canonical JSON")
	}
	return append(json.RawMessage(nil), encoded...), nil
}

func canonicalConfigNumbers(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			if !canonicalConfigNumbers(child) {
				return false
			}
		}
	case []any:
		for _, child := range typed {
			if !canonicalConfigNumbers(child) {
				return false
			}
		}
	case json.Number:
		number := string(typed)
		if strings.ContainsAny(number, "eE") || negativeZeroPattern.MatchString(number) {
			return false
		}
	}
	return true
}

func reviewedDatasetKinds() map[dataset.Kind]struct{} {
	policy, err := dataset.NewPolicy(dataset.ReviewedPolicyV1Input())
	if err != nil {
		panic(err)
	}
	result := make(map[dataset.Kind]struct{})
	for _, kind := range policy.Kinds() {
		result[kind] = struct{}{}
	}
	return result
}
