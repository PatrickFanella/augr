package strategycatalog

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const (
	LifecycleEvidenceSchemaV1 = "strategy-catalog-lifecycle-event-v1"
	lifecycleEvidenceDomain   = "strategy-catalog-lifecycle-event"
)

type EntityKind string

const (
	EntityFamily        EntityKind = "family"
	EntityVersion       EntityKind = "version"
	EntityExperiment    EntityKind = "experiment"
	EntityDeployment    EntityKind = "deployment"
	EntityLegacyMapping EntityKind = "legacy_mapping"
)

type lifecycleCanonical struct {
	Schema         string     `json:"schema"`
	EntityKind     EntityKind `json:"entity_kind"`
	EntityID       string     `json:"entity_id"`
	EventKind      string     `json:"event_kind"`
	PriorState     string     `json:"prior_state"`
	NextState      string     `json:"next_state"`
	EvidenceSHA256 string     `json:"evidence_sha256"`
}

type LifecycleEvidence struct {
	canonical lifecycleCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewInitialLifecycleEvidence(kind EntityKind, entityID uuid.UUID, evidenceSHA256 string) (*LifecycleEvidence, error) {
	eventKind, nextState, ok := initialLifecycle(kind)
	if !ok || entityID == uuid.Nil || !sha256Pattern.MatchString(evidenceSHA256) {
		return nil, fmt.Errorf("strategy catalog lifecycle evidence is invalid")
	}
	canonical := lifecycleCanonical{
		Schema: LifecycleEvidenceSchemaV1, EntityKind: kind, EntityID: entityID.String(),
		EventKind: eventKind, PriorState: "", NextState: nextState, EvidenceSHA256: evidenceSHA256,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, err
	}
	digest := hashBytes(encoded)
	return &LifecycleEvidence{
		canonical: canonical, bytes: encoded, digest: digest,
		id: economicid.DeterministicUUID(lifecycleEvidenceDomain, kind.String(), entityID.String(), eventKind, evidenceSHA256),
	}, nil
}

func LifecycleEvidenceFromCanonical(id uuid.UUID, digest string, raw []byte) (*LifecycleEvidence, error) {
	if id == uuid.Nil || !sha256Pattern.MatchString(digest) || hashBytes(raw) != digest {
		return nil, fmt.Errorf("strategy catalog lifecycle envelope is invalid")
	}
	var canonical lifecycleCanonical
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	entityID, err := uuid.Parse(canonical.EntityID)
	if err != nil {
		return nil, err
	}
	evidence, err := NewInitialLifecycleEvidence(canonical.EntityKind, entityID, canonical.EvidenceSHA256)
	if err != nil {
		return nil, err
	}
	if canonical.Schema != LifecycleEvidenceSchemaV1 || canonical.PriorState != "" || evidence.ID() != id ||
		evidence.Digest() != digest || !bytes.Equal(evidence.bytes, raw) {
		return nil, fmt.Errorf("strategy catalog lifecycle identity does not reconstruct")
	}
	return evidence, nil
}

func initialLifecycle(kind EntityKind) (string, string, bool) {
	switch kind {
	case EntityFamily, EntityVersion:
		return "registered", "registered", true
	case EntityExperiment:
		return "declared", ExperimentDeclared, true
	case EntityDeployment:
		return "proposed", DeploymentProposed, true
	case EntityLegacyMapping:
		return "mapped", LegacyUnvalidated, true
	default:
		return "", "", false
	}
}

func (kind EntityKind) String() string { return string(kind) }
func (evidence *LifecycleEvidence) ID() uuid.UUID {
	if evidence == nil {
		return uuid.Nil
	}
	return evidence.id
}

func (evidence *LifecycleEvidence) Digest() string {
	if evidence == nil {
		return ""
	}
	return evidence.digest
}

func (evidence *LifecycleEvidence) CanonicalBytes() json.RawMessage {
	if evidence == nil {
		return nil
	}
	return append(json.RawMessage(nil), evidence.bytes...)
}

func (evidence *LifecycleEvidence) EntityKind() EntityKind {
	if evidence == nil {
		return ""
	}
	return evidence.canonical.EntityKind
}

func (evidence *LifecycleEvidence) EntityID() uuid.UUID {
	if evidence == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(evidence.canonical.EntityID)
	return id
}

func (evidence *LifecycleEvidence) EventKind() string {
	if evidence == nil {
		return ""
	}
	return evidence.canonical.EventKind
}

func (evidence *LifecycleEvidence) NextState() string {
	if evidence == nil {
		return ""
	}
	return evidence.canonical.NextState
}

func (evidence *LifecycleEvidence) EvidenceSHA256() string {
	if evidence == nil {
		return ""
	}
	return evidence.canonical.EvidenceSHA256
}
