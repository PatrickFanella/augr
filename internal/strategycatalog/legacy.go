package strategycatalog

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const (
	LegacyMappingSchemaV1 = "legacy-strategy-family-mapping-v1"
	LegacyUnvalidated     = "legacy_unvalidated"
	legacyMappingDomain   = "legacy-strategy-family-mapping"
)

type LegacyMappingInput struct {
	LegacyStrategyID     uuid.UUID
	FamilyID             uuid.UUID
	LegacySnapshotSHA256 string
}

type legacyMappingCanonical struct {
	Schema               string `json:"schema"`
	State                string `json:"state"`
	LegacyStrategyID     string `json:"legacy_strategy_id"`
	FamilyID             string `json:"family_id"`
	LegacySnapshotSHA256 string `json:"legacy_snapshot_sha256"`
}

type LegacyMapping struct {
	canonical legacyMappingCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewLegacyMapping(input LegacyMappingInput) (*LegacyMapping, error) {
	if input.LegacyStrategyID == uuid.Nil || input.FamilyID == uuid.Nil || !sha256Pattern.MatchString(input.LegacySnapshotSHA256) {
		return nil, fmt.Errorf("legacy strategy mapping is invalid")
	}
	canonical := legacyMappingCanonical{
		Schema: LegacyMappingSchemaV1, State: LegacyUnvalidated,
		LegacyStrategyID: input.LegacyStrategyID.String(), FamilyID: input.FamilyID.String(),
		LegacySnapshotSHA256: input.LegacySnapshotSHA256,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal legacy strategy mapping: %w", err)
	}
	return &LegacyMapping{
		canonical: canonical, bytes: encoded, digest: hashBytes(encoded),
		id: economicid.DeterministicUUID(legacyMappingDomain, input.LegacyStrategyID.String()),
	}, nil
}

func LegacyMappingFromCanonical(id uuid.UUID, digest string, raw []byte) (*LegacyMapping, error) {
	if id == uuid.Nil || !sha256Pattern.MatchString(digest) || hashBytes(raw) != digest {
		return nil, fmt.Errorf("legacy strategy mapping envelope is invalid")
	}
	var canonical legacyMappingCanonical
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	legacyID, err := uuid.Parse(canonical.LegacyStrategyID)
	if err != nil {
		return nil, err
	}
	familyID, err := uuid.Parse(canonical.FamilyID)
	if err != nil {
		return nil, err
	}
	mapping, err := NewLegacyMapping(LegacyMappingInput{LegacyStrategyID: legacyID, FamilyID: familyID, LegacySnapshotSHA256: canonical.LegacySnapshotSHA256})
	if err != nil {
		return nil, err
	}
	if canonical.Schema != LegacyMappingSchemaV1 || canonical.State != LegacyUnvalidated || mapping.ID() != id ||
		mapping.Digest() != digest || !bytes.Equal(mapping.bytes, raw) {
		return nil, fmt.Errorf("legacy strategy mapping canonical identity does not reconstruct")
	}
	return mapping, nil
}

func (mapping *LegacyMapping) ID() uuid.UUID {
	if mapping == nil {
		return uuid.Nil
	}
	return mapping.id
}

func (mapping *LegacyMapping) Digest() string {
	if mapping == nil {
		return ""
	}
	return mapping.digest
}

func (mapping *LegacyMapping) CanonicalBytes() json.RawMessage {
	if mapping == nil {
		return nil
	}
	return append(json.RawMessage(nil), mapping.bytes...)
}

func (mapping *LegacyMapping) LegacyStrategyID() uuid.UUID {
	if mapping == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(mapping.canonical.LegacyStrategyID)
	return id
}

func (mapping *LegacyMapping) FamilyID() uuid.UUID {
	if mapping == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(mapping.canonical.FamilyID)
	return id
}

func (mapping *LegacyMapping) LegacySnapshotSHA256() string {
	if mapping == nil {
		return ""
	}
	return mapping.canonical.LegacySnapshotSHA256
}

func (mapping *LegacyMapping) State() string {
	if mapping == nil {
		return ""
	}
	return mapping.canonical.State
}
