package strategycatalog

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
)

func TestVersionConfigEditCreatesNewImmutableIdentity(t *testing.T) {
	input := validVersionInput()
	first, err := NewVersion(input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := NewVersion(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() == uuid.Nil || retry.ID() != first.ID() || retry.Digest() != first.Digest() {
		t.Fatal("identical version did not converge")
	}
	input.Config = json.RawMessage(`{"lookback":253,"rebalance":"monthly"}`)
	edited, err := NewVersion(input)
	if err != nil {
		t.Fatal(err)
	}
	if edited.ID() == first.ID() || edited.Digest() == first.Digest() || bytes.Equal(edited.CanonicalBytes(), first.CanonicalBytes()) {
		t.Fatal("config edit reused immutable version identity")
	}
	restored, err := VersionFromCanonical(first.ID(), first.Digest(), first.CanonicalBytes())
	if err != nil || restored.ID() != first.ID() || restored.FamilyID() != input.FamilyID {
		t.Fatalf("restore version = %+v, %v", restored, err)
	}
	config := restored.Config()
	config[0] = '['
	if bytes.Equal(config, restored.Config()) {
		t.Fatal("version config leaked mutable state")
	}
}

func TestVersionDatasetKindsReorderWithoutIdentityChange(t *testing.T) {
	input := validVersionInput()
	first, err := NewVersion(input)
	if err != nil {
		t.Fatal(err)
	}
	input.RequiredDatasetKinds[0], input.RequiredDatasetKinds[1] = input.RequiredDatasetKinds[1], input.RequiredDatasetKinds[0]
	reordered, err := NewVersion(input)
	if err != nil {
		t.Fatal(err)
	}
	if reordered.ID() != first.ID() || !bytes.Equal(reordered.CanonicalBytes(), first.CanonicalBytes()) {
		t.Fatal("dataset kind input ordering changed version identity")
	}
}

func TestVersionRejectsNoncanonicalOrInvalidIdentity(t *testing.T) {
	valid := validVersionInput()
	for name, mutate := range map[string]func(*VersionInput){
		"family":               func(value *VersionInput) { value.FamilyID = uuid.Nil },
		"compiler":             func(value *VersionInput) { value.CompilerKind = " compiler" },
		"source commit":        func(value *VersionInput) { value.SourceCommit = "main" },
		"tree digest":          func(value *VersionInput) { value.SourceTreeSHA256 = strings.Repeat("z", 64) },
		"config array":         func(value *VersionInput) { value.Config = json.RawMessage(`[]`) },
		"config spacing":       func(value *VersionInput) { value.Config = json.RawMessage(`{ "lookback":252,"rebalance":"monthly"}`) },
		"config ordering":      func(value *VersionInput) { value.Config = json.RawMessage(`{"rebalance":"monthly","lookback":252}`) },
		"config exponent":      func(value *VersionInput) { value.Config = json.RawMessage(`{"lookback":1e3}`) },
		"config negative zero": func(value *VersionInput) { value.Config = json.RawMessage(`{"lookback":-0}`) },
		"nested negative zero": func(value *VersionInput) { value.Config = json.RawMessage(`{"thresholds":[{"value":-0.0}]}`) },
		"no kinds":             func(value *VersionInput) { value.RequiredDatasetKinds = nil },
		"duplicate kind": func(value *VersionInput) {
			value.RequiredDatasetKinds = []dataset.Kind{dataset.KindBars, dataset.KindBars}
		},
		"unknown kind": func(value *VersionInput) { value.RequiredDatasetKinds = []dataset.Kind{"invented"} },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			input.RequiredDatasetKinds = append([]dataset.Kind(nil), valid.RequiredDatasetKinds...)
			mutate(&input)
			if _, err := NewVersion(input); err == nil {
				t.Fatal("invalid version succeeded")
			}
		})
	}
}

func TestVersionRestoreRejectsTampering(t *testing.T) {
	version, err := NewVersion(validVersionInput())
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(version.CanonicalBytes(), []byte(`"lookback":252`), []byte(`"lookback":253`), 1)
	if _, err := VersionFromCanonical(version.ID(), hashBytes(tampered), tampered); err == nil {
		t.Fatal("tampered version restored under original ID")
	}
}

func validVersionInput() VersionInput {
	return VersionInput{
		FamilyID:     uuid.MustParse("30200000-0000-4000-8000-000000000001"),
		CompilerKind: "go-native", CompilerVersion: "strategy-compiler-v1",
		SourceCommit: strings.Repeat("a", 40), SourceTreeSHA256: strings.Repeat("b", 64),
		ConfigSchema: "momentum-config-v1", Config: json.RawMessage(`{"lookback":252,"rebalance":"monthly"}`),
		DecisionContract: "trade-intent-v1", RequiredDatasetKinds: []dataset.Kind{dataset.KindQuotes, dataset.KindBars},
	}
}
