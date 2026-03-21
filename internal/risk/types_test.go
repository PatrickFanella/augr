package risk

import "testing"

func TestCircuitBreakerStates(t *testing.T) {
	t.Parallel()

	if CircuitBreakerPhaseOpen != "open" {
		t.Fatalf("expected open state value, got %q", CircuitBreakerPhaseOpen)
	}
	if CircuitBreakerPhaseTripped != "tripped" {
		t.Fatalf("expected tripped state value, got %q", CircuitBreakerPhaseTripped)
	}
	if CircuitBreakerPhaseCooldown != "cooldown" {
		t.Fatalf("expected cooldown state value, got %q", CircuitBreakerPhaseCooldown)
	}

	if CircuitBreakerPhaseOpen.String() != "open" {
		t.Fatalf("expected open String() value, got %q", CircuitBreakerPhaseOpen.String())
	}
	if CircuitBreakerPhaseTripped.String() != "tripped" {
		t.Fatalf("expected tripped String() value, got %q", CircuitBreakerPhaseTripped.String())
	}
	if CircuitBreakerPhaseCooldown.String() != "cooldown" {
		t.Fatalf("expected cooldown String() value, got %q", CircuitBreakerPhaseCooldown.String())
	}

	states := map[CircuitBreakerPhase]struct{}{
		CircuitBreakerPhaseOpen:     {},
		CircuitBreakerPhaseTripped:  {},
		CircuitBreakerPhaseCooldown: {},
	}
	if len(states) != 3 {
		t.Fatalf("expected exactly 3 circuit breaker states, got %d", len(states))
	}
}

func TestKillSwitchMechanisms(t *testing.T) {
	t.Parallel()

	if KillSwitchMechanismAPI != "api_toggle" {
		t.Fatalf("expected api mechanism value, got %q", KillSwitchMechanismAPI)
	}
	if KillSwitchMechanismFile != "file_flag" {
		t.Fatalf("expected file mechanism value, got %q", KillSwitchMechanismFile)
	}
	if KillSwitchMechanismEnvVar != "env_var" {
		t.Fatalf("expected env mechanism value, got %q", KillSwitchMechanismEnvVar)
	}
	if KillSwitchMechanismUnknown != "unknown" {
		t.Fatalf("expected unknown mechanism value, got %q", KillSwitchMechanismUnknown)
	}
	if KillSwitchMechanismAPI.String() != "api_toggle" {
		t.Fatalf("expected api String() value, got %q", KillSwitchMechanismAPI.String())
	}
	if KillSwitchMechanismFile.String() != "file_flag" {
		t.Fatalf("expected file String() value, got %q", KillSwitchMechanismFile.String())
	}
	if KillSwitchMechanismEnvVar.String() != "env_var" {
		t.Fatalf("expected env String() value, got %q", KillSwitchMechanismEnvVar.String())
	}
	if KillSwitchMechanismUnknown.String() != "unknown" {
		t.Fatalf("expected unknown String() value, got %q", KillSwitchMechanismUnknown.String())
	}

	mechanisms := map[KillSwitchMechanism]struct{}{
		KillSwitchMechanismAPI:     {},
		KillSwitchMechanismFile:    {},
		KillSwitchMechanismEnvVar:  {},
		KillSwitchMechanismUnknown: {},
	}
	if len(mechanisms) != 4 {
		t.Fatalf("expected exactly 4 kill switch mechanisms, got %d", len(mechanisms))
	}
}
