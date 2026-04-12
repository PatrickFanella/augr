package api_test

import (
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/api"
)

// TestEventTypeVocabularyConsistency verifies that the API EventType constants
// match the WebSocket event type vocabulary defined in the TypeScript frontend
// (web/src/lib/api/types.ts  WebSocketEventType).
//
// If this test fails, at least one of the three layers (Go agent, Go API, or
// TypeScript) has drifted. Update all three to stay in sync.
func TestEventTypeVocabularyConsistency(t *testing.T) {
	// Expected set: the canonical WebSocket event types from the TypeScript
	// frontend (web/src/lib/api/types.ts WebSocketEventType).
	expectedWSTypes := []string{
		"pipeline_start",
		"agent_decision",
		"debate_round",
		"signal",
		"order_submitted",
		"order_filled",
		"position_update",
		"circuit_breaker",
		"error",
		"pipeline_health",
	}

	// API hub event types (internal/api/hub.go EventType constants).
	apiEventTypes := map[api.EventType]bool{
		api.EventPipelineStart:  true,
		api.EventAgentDecision:  true,
		api.EventDebateRound:    true,
		api.EventSignal:         true,
		api.EventOrderSubmitted: true,
		api.EventOrderFilled:    true,
		api.EventPositionUpdate: true,
		api.EventCircuitBreaker: true,
		api.EventError:          true,
		api.EventPipelineHealth: true,
	}

	// Every expected WebSocket type must have a matching API EventType constant.
	for _, ws := range expectedWSTypes {
		found := false
		for et := range apiEventTypes {
			if string(et) == ws {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("WebSocket event type %q has no matching API EventType constant", ws)
		}
	}

	// Every API EventType constant must appear in the expected WebSocket set.
	wsSet := make(map[string]bool, len(expectedWSTypes))
	for _, ws := range expectedWSTypes {
		wsSet[ws] = true
	}
	for et := range apiEventTypes {
		if !wsSet[string(et)] {
			t.Errorf("API EventType %q has no matching WebSocket event type in TypeScript", et)
		}
	}

	// Counts must agree.
	if len(expectedWSTypes) != len(apiEventTypes) {
		t.Errorf("count mismatch: %d WebSocket types vs %d API EventType constants",
			len(expectedWSTypes), len(apiEventTypes))
	}
}
