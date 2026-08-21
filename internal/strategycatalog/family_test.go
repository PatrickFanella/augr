package strategycatalog

import (
	"bytes"
	"testing"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

func TestFamilyCanonicalIdentityAndRestore(t *testing.T) {
	input := FamilyInput{
		Slug: "quality-wheel", Name: "Quality-filtered wheel",
		Thesis:       "Harvest option premium only when quality and valuation gates admit the underlying.",
		AssetClasses: []instrument.AssetClass{instrument.AssetClassOption, instrument.AssetClassEquity},
	}
	first, err := NewFamily(input)
	if err != nil {
		t.Fatal(err)
	}
	input.AssetClasses[0], input.AssetClasses[1] = input.AssetClasses[1], input.AssetClasses[0]
	reordered, err := NewFamily(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() == uuid.Nil || first.ID() != reordered.ID() || first.Digest() != reordered.Digest() || !bytes.Equal(first.CanonicalBytes(), reordered.CanonicalBytes()) {
		t.Fatal("family identity changed with input ordering")
	}
	restored, err := FamilyFromCanonical(first.ID(), first.Digest(), first.CanonicalBytes())
	if err != nil || restored.ID() != first.ID() || restored.Slug() != input.Slug {
		t.Fatalf("restore family = %+v, %v", restored, err)
	}
	classes := restored.AssetClasses()
	classes[0] = instrument.AssetClassUnknown
	if restored.AssetClasses()[0] == instrument.AssetClassUnknown {
		t.Fatal("family asset classes leaked mutable state")
	}
}

func TestFamilyRejectsInvalidOrChangedStableIdentity(t *testing.T) {
	valid := FamilyInput{Slug: "momentum", Name: "Momentum", Thesis: "Rank point-in-time momentum.", AssetClasses: []instrument.AssetClass{instrument.AssetClassEquity}}
	for name, mutate := range map[string]func(*FamilyInput){
		"slug":       func(value *FamilyInput) { value.Slug = "Momentum" },
		"name":       func(value *FamilyInput) { value.Name = " Momentum" },
		"thesis":     func(value *FamilyInput) { value.Thesis = "" },
		"unknown":    func(value *FamilyInput) { value.AssetClasses = []instrument.AssetClass{instrument.AssetClassUnknown} },
		"duplicates": func(value *FamilyInput) { value.AssetClasses = append(value.AssetClasses, value.AssetClasses[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			input.AssetClasses = append([]instrument.AssetClass(nil), valid.AssetClasses...)
			mutate(&input)
			if _, err := NewFamily(input); err == nil {
				t.Fatal("invalid family succeeded")
			}
		})
	}
	family, err := NewFamily(valid)
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(family.CanonicalBytes(), []byte(`"Momentum"`), []byte(`"Momentum changed"`), 1)
	if _, err := FamilyFromCanonical(family.ID(), family.Digest(), tampered); err == nil {
		t.Fatal("tampered stable family envelope restored")
	}
}
