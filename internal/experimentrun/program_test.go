package experimentrun

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestProgramIdentityRestoresAndChangesWithExecutableEvidence(t *testing.T) {
	input := validProgramIdentityInput()
	first, err := NewProgramIdentity(input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := NewProgramIdentity(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() == uuid.Nil || first.ID() != retry.ID() || first.Digest() != retry.Digest() {
		t.Fatal("identical program identity did not converge")
	}
	restored, err := ProgramIdentityFromCanonical(first.ID(), first.Digest(), first.CanonicalBytes())
	if err != nil || restored.VersionID() != input.VersionID || restored.AdapterSHA256() != input.AdapterSHA256 {
		t.Fatalf("restore program=%+v err=%v", restored, err)
	}
	input.AdapterSHA256 = strings.Repeat("d", 64)
	changed, err := NewProgramIdentity(input)
	if err != nil {
		t.Fatal(err)
	}
	if changed.ID() == first.ID() || bytes.Equal(changed.CanonicalBytes(), first.CanonicalBytes()) {
		t.Fatal("adapter artifact change reused program identity")
	}
}

func TestProgramIdentityRejectsInvalidAndTamperedEvidence(t *testing.T) {
	input := validProgramIdentityInput()
	input.VersionID = uuid.Nil
	if _, err := NewProgramIdentity(input); err == nil {
		t.Fatal("nil version succeeded")
	}
	program, err := NewProgramIdentity(validProgramIdentityInput())
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(program.CanonicalBytes(), []byte(`"fixture-v1"`), []byte(`"fixture-v2"`), 1)
	if _, err := ProgramIdentityFromCanonical(program.ID(), hashBytes(tampered), tampered); err == nil {
		t.Fatal("tampered program restored under original identity")
	}
}

func validProgramIdentityInput() ProgramIdentityInput {
	return ProgramIdentityInput{
		VersionID: uuid.MustParse("30300000-0000-4000-8000-000000000001"), VersionSHA256: strings.Repeat("a", 64),
		CompilerKind: "go-native", CompilerVersion: "strategy-compiler-v1", SourceCommit: strings.Repeat("b", 40),
		SourceTreeSHA256: strings.Repeat("c", 64), DecisionContract: "trade-intent-v1",
		AdapterKind: "fixture", AdapterVersion: "fixture-v1", AdapterSHA256: strings.Repeat("e", 64),
		RunnerContract: "experiment-runner-v1",
	}
}
