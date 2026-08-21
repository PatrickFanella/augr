package generativestrategy

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestCompilerProducesExactInertStrategyVersionAndReceipt(t *testing.T) {
	_, input := specFixture(t)
	spec, err := NewSpec(input)
	if err != nil {
		t.Fatal(err)
	}
	version, receipt, err := Compile(spec, strings.Repeat("b", 40), strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	if version.FamilyID() != input.Family.ID() || version.CompilerKind() != CompilerKindV1 || version.CompilerVersion() != CompilerVersionV1 || version.ConfigSchema() != ConfigSchemaV1 || version.DecisionContract() != DecisionContractV1 {
		t.Fatalf("version identity is wrong: %s", version.CanonicalBytes())
	}
	if receipt.SpecID() != spec.ID() || receipt.VersionID() != version.ID() || bytes.Contains(version.Config(), []byte(`"state":"active"`)) || bytes.Contains(version.Config(), []byte("deployment")) {
		t.Fatalf("authority leaked: version=%s receipt=%s", version.Config(), receipt.CanonicalBytes())
	}
	reloaded, err := ReceiptFromCanonical(receipt.ID(), receipt.Digest(), receipt.CanonicalBytes(), spec, version)
	if err != nil || reloaded.Digest() != receipt.Digest() {
		t.Fatalf("reload=%v err=%v", reloaded, err)
	}
}

func TestCompilerIsByteIdenticalUnderConcurrentUse(t *testing.T) {
	_, input := specFixture(t)
	spec, err := NewSpec(input)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		version, receipt string
		err              error
	}
	values := make(chan result, 8)
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			version, receipt, compileErr := Compile(spec, strings.Repeat("b", 40), strings.Repeat("c", 64))
			if compileErr != nil {
				values <- result{err: compileErr}
				return
			}
			values <- result{version: version.Digest(), receipt: receipt.Digest()}
		}()
	}
	wait.Wait()
	close(values)
	var expected result
	for value := range values {
		if value.err != nil {
			t.Fatal(value.err)
		}
		if expected.version == "" {
			expected = value
		} else if value.version != expected.version || value.receipt != expected.receipt {
			t.Fatalf("nondeterministic=%+v/%+v", value, expected)
		}
	}
}

func TestCompilerSemanticEditsChangeVersionAndInvalidInputsEmitNothing(t *testing.T) {
	_, input := specFixture(t)
	firstSpec, err := NewSpec(input)
	if err != nil {
		t.Fatal(err)
	}
	firstVersion, firstReceipt, err := Compile(firstSpec, strings.Repeat("b", 40), strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	input.Costs.SlippageBPS = "3"
	secondSpec, err := NewSpec(input)
	if err != nil {
		t.Fatal(err)
	}
	secondVersion, secondReceipt, err := Compile(secondSpec, strings.Repeat("b", 40), strings.Repeat("c", 64))
	if err != nil || firstVersion.ID() == secondVersion.ID() || firstReceipt.ID() == secondReceipt.ID() {
		t.Fatalf("semantic edit did not change identity: %v", err)
	}
	version, receipt, err := Compile(firstSpec, "dirty", strings.Repeat("c", 64))
	if err == nil || version != nil || receipt != nil {
		t.Fatalf("invalid compiler emitted version=%v receipt=%v err=%v", version, receipt, err)
	}
}

func TestReceiptRejectsTampering(t *testing.T) {
	_, input := specFixture(t)
	spec, err := NewSpec(input)
	if err != nil {
		t.Fatal(err)
	}
	version, receipt, err := Compile(spec, strings.Repeat("b", 40), strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(receipt.CanonicalBytes(), []byte(`"state":"compiled"`), []byte(`"state":"deployed"`), 1)
	if _, err = ReceiptFromCanonical(receipt.ID(), receipt.Digest(), tampered, spec, version); err == nil {
		t.Fatal("tampered receipt accepted")
	}
}
