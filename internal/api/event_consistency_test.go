package api_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/api"
)

// TestEventTypeVocabularyConsistency verifies the Go and TypeScript WebSocket
// event vocabularies stay aligned.
func TestEventTypeVocabularyConsistency(t *testing.T) {
	t.Parallel()

	want := []string{
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
		"polymarket_whale_trade",
		"polymarket_price_move",
		"polymarket_account_tracked",
	}

	gotGo := eventTypeStrings(api.WebSocketEventTypes())
	if !slices.Equal(gotGo, want) {
		t.Fatalf("Go event vocabulary mismatch\n got: %v\nwant: %v", gotGo, want)
	}

	gotTS, err := readFrontendEventTypes()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(gotTS, want) {
		t.Fatalf("TypeScript event vocabulary mismatch\n got: %v\nwant: %v", gotTS, want)
	}

	if !slices.Equal(gotGo, gotTS) {
		t.Fatalf("Go and TypeScript event vocabularies diverged\n go: %v\n ts: %v", gotGo, gotTS)
	}
}

func eventTypeStrings(eventTypes []api.EventType) []string {
	out := make([]string, len(eventTypes))
	for i, eventType := range eventTypes {
		out[i] = string(eventType)
	}
	return out
}

func readFrontendEventTypes() ([]string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return nil, errors.New("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(filename), "..", "..", "web", "src", "shared", "types", "websocket.ts")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var events []string
	inList := false
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if !inList {
			if strings.HasPrefix(trimmed, "export const websocketEventTypes = [") {
				inList = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "] as const") {
			break
		}
		if trimmed == "" {
			continue
		}
		trimmed = strings.TrimSuffix(trimmed, ",")
		events = append(events, strings.Trim(trimmed, "'"))
	}

	if len(events) == 0 {
		return nil, errors.New("failed to parse frontend websocket event vocabulary")
	}
	return events, nil
}
