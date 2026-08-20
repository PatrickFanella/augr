package strategycatalog

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestLegacyMappingRemainsUnvalidatedAndStablePerLegacyStrategy(t *testing.T) {
	input := LegacyMappingInput{
		LegacyStrategyID:     uuid.MustParse("30200000-0000-4000-8000-000000000030"),
		FamilyID:             uuid.MustParse("30200000-0000-4000-8000-000000000031"),
		LegacySnapshotSHA256: strings.Repeat("a", 64),
	}
	mapping, err := NewLegacyMapping(input)
	if err != nil {
		t.Fatal(err)
	}
	if mapping.State() != LegacyUnvalidated {
		t.Fatal("legacy mapping claimed validation")
	}
	changed := input
	changed.FamilyID = uuid.MustParse("30200000-0000-4000-8000-000000000032")
	remapped, err := NewLegacyMapping(changed)
	if err != nil {
		t.Fatal(err)
	}
	if remapped.ID() != mapping.ID() || remapped.Digest() == mapping.Digest() {
		t.Fatal("stable legacy mapping key did not expose changed-payload conflict")
	}
	restored, err := LegacyMappingFromCanonical(mapping.ID(), mapping.Digest(), mapping.CanonicalBytes())
	if err != nil || restored.LegacyStrategyID() != input.LegacyStrategyID || restored.FamilyID() != input.FamilyID {
		t.Fatalf("restore legacy mapping = %+v, %v", restored, err)
	}
}

func TestInitialLifecycleEvidenceHasNoTransitionSurface(t *testing.T) {
	for kind, want := range map[EntityKind]string{
		EntityFamily: "registered", EntityVersion: "registered", EntityExperiment: ExperimentDeclared,
		EntityDeployment: DeploymentProposed, EntityLegacyMapping: LegacyUnvalidated,
	} {
		evidence, err := NewInitialLifecycleEvidence(kind, uuid.New(), strings.Repeat("b", 64))
		if err != nil {
			t.Fatal(err)
		}
		if evidence.NextState() != want || evidence.ID() == uuid.Nil {
			t.Fatalf("%s initial state=%q want=%q", kind, evidence.NextState(), want)
		}
		restored, err := LifecycleEvidenceFromCanonical(evidence.ID(), evidence.Digest(), evidence.CanonicalBytes())
		if err != nil || !bytes.Equal(restored.CanonicalBytes(), evidence.CanonicalBytes()) {
			t.Fatalf("restore %s lifecycle = %+v, %v", kind, restored, err)
		}
	}
	if _, err := NewInitialLifecycleEvidence("activation", uuid.New(), strings.Repeat("b", 64)); err == nil {
		t.Fatal("unknown transition authority succeeded")
	}
}
