package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// Hub unit tests
// ---------------------------------------------------------------------------

func TestHubRegisterUnregister(t *testing.T) {
	t.Parallel()

	hub := NewHub(slog.Default())
	go hub.Run()
	defer hub.Stop()

	c := &Client{
		hub:  hub,
		send: make(chan []byte, sendBufferSize),
		subscriptions: Subscriptions{
			StrategyIDs: make(map[uuid.UUID]bool),
			RunIDs:      make(map[uuid.UUID]bool),
			AllEvents:   true,
		},
	}

	hub.register <- c
	waitFor(t, func() bool { return hub.ClientCount() == 1 })

	hub.unregister <- c
	waitFor(t, func() bool { return hub.ClientCount() == 0 })
}

func TestHubBroadcastToSubscribers(t *testing.T) {
	t.Parallel()

	hub := NewHub(slog.Default())
	go hub.Run()
	defer hub.Stop()

	stratID := uuid.New()
	runID := uuid.New()

	// Client subscribed to a specific strategy.
	cStrategy := &Client{
		hub:  hub,
		send: make(chan []byte, sendBufferSize),
		subscriptions: Subscriptions{
			StrategyIDs: map[uuid.UUID]bool{stratID: true},
			RunIDs:      make(map[uuid.UUID]bool),
		},
	}

	// Client subscribed to a specific run.
	cRun := &Client{
		hub:  hub,
		send: make(chan []byte, sendBufferSize),
		subscriptions: Subscriptions{
			StrategyIDs: make(map[uuid.UUID]bool),
			RunIDs:      map[uuid.UUID]bool{runID: true},
		},
	}

	// Client subscribed to all events.
	cAll := &Client{
		hub:  hub,
		send: make(chan []byte, sendBufferSize),
		subscriptions: Subscriptions{
			StrategyIDs: make(map[uuid.UUID]bool),
			RunIDs:      make(map[uuid.UUID]bool),
			AllEvents:   true,
		},
	}

	// Client with no matching subscriptions.
	cNone := &Client{
		hub:  hub,
		send: make(chan []byte, sendBufferSize),
		subscriptions: Subscriptions{
			StrategyIDs: map[uuid.UUID]bool{uuid.New(): true},
			RunIDs:      make(map[uuid.UUID]bool),
		},
	}

	hub.register <- cStrategy
	hub.register <- cRun
	hub.register <- cAll
	hub.register <- cNone
	waitFor(t, func() bool { return hub.ClientCount() == 4 })

	// Broadcast a message matching stratID.
	hub.Broadcast(WSMessage{
		Type:       EventPipelineStart,
		StrategyID: stratID,
		RunID:      runID,
		Timestamp:  time.Now(),
	})

	// cStrategy, cRun, and cAll should receive it.
	assertReceive(t, cStrategy.send, "cStrategy")
	assertReceive(t, cRun.send, "cRun")
	assertReceive(t, cAll.send, "cAll")
	assertNoReceive(t, cNone.send, "cNone")
}

func TestHubBroadcastFiltering(t *testing.T) {
	t.Parallel()

	hub := NewHub(slog.Default())
	go hub.Run()
	defer hub.Stop()

	stratID := uuid.New()
	otherStratID := uuid.New()

	c := &Client{
		hub:  hub,
		send: make(chan []byte, sendBufferSize),
		subscriptions: Subscriptions{
			StrategyIDs: map[uuid.UUID]bool{stratID: true},
			RunIDs:      make(map[uuid.UUID]bool),
		},
	}
	hub.register <- c
	waitFor(t, func() bool { return hub.ClientCount() == 1 })

	// Message for other strategy should not be received.
	hub.Broadcast(WSMessage{
		Type:       EventOrderSubmitted,
		StrategyID: otherStratID,
		Timestamp:  time.Now(),
	})
	assertNoReceive(t, c.send, "c (other strategy)")

	// Message for subscribed strategy should be received.
	hub.Broadcast(WSMessage{
		Type:       EventOrderFilled,
		StrategyID: stratID,
		Timestamp:  time.Now(),
	})
	assertReceive(t, c.send, "c (subscribed strategy)")
}

func TestHubStop(t *testing.T) {
	t.Parallel()

	hub := NewHub(slog.Default())
	go hub.Run()

	c := &Client{
		hub:  hub,
		send: make(chan []byte, sendBufferSize),
		subscriptions: Subscriptions{
			StrategyIDs: make(map[uuid.UUID]bool),
			RunIDs:      make(map[uuid.UUID]bool),
			AllEvents:   true,
		},
	}
	hub.register <- c
	waitFor(t, func() bool { return hub.ClientCount() == 1 })

	hub.Stop()

	// After stop, the send channel should be closed.
	select {
	case _, ok := <-c.send:
		if ok {
			t.Fatal("expected send channel to be closed after hub stop")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for send channel close after hub stop")
	}
}

// ---------------------------------------------------------------------------
// Client subscription tests
// ---------------------------------------------------------------------------

func TestClientMatchesSubscription(t *testing.T) {
	t.Parallel()

	stratID := uuid.New()
	runID := uuid.New()

	tests := []struct {
		name   string
		subs   Subscriptions
		msg    WSMessage
		expect bool
	}{
		{
			name:   "all events",
			subs:   Subscriptions{AllEvents: true, StrategyIDs: make(map[uuid.UUID]bool), RunIDs: make(map[uuid.UUID]bool)},
			msg:    WSMessage{Type: EventPipelineStart, StrategyID: uuid.New()},
			expect: true,
		},
		{
			name:   "strategy match",
			subs:   Subscriptions{StrategyIDs: map[uuid.UUID]bool{stratID: true}, RunIDs: make(map[uuid.UUID]bool)},
			msg:    WSMessage{Type: EventSignal, StrategyID: stratID},
			expect: true,
		},
		{
			name:   "run match",
			subs:   Subscriptions{StrategyIDs: make(map[uuid.UUID]bool), RunIDs: map[uuid.UUID]bool{runID: true}},
			msg:    WSMessage{Type: EventAgentDecision, RunID: runID},
			expect: true,
		},
		{
			name:   "no match",
			subs:   Subscriptions{StrategyIDs: map[uuid.UUID]bool{uuid.New(): true}, RunIDs: make(map[uuid.UUID]bool)},
			msg:    WSMessage{Type: EventError, StrategyID: uuid.New()},
			expect: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := &Client{subscriptions: tc.subs}
			data, _ := json.Marshal(tc.msg)
			if got := c.matchesSubscription(data); got != tc.expect {
				t.Fatalf("matchesSubscription() = %v, want %v", got, tc.expect)
			}
		})
	}
}

func TestClientHandleCommand(t *testing.T) {
	t.Parallel()

	hub := NewHub(slog.Default())
	go hub.Run()
	defer hub.Stop()

	stratID := uuid.New()
	runID := uuid.New()

	c := &Client{
		hub:  hub,
		send: make(chan []byte, sendBufferSize),
		subscriptions: Subscriptions{
			StrategyIDs: make(map[uuid.UUID]bool),
			RunIDs:      make(map[uuid.UUID]bool),
		},
	}

	// Subscribe to a strategy.
	cmd, _ := json.Marshal(clientCommand{
		Action:      "subscribe",
		StrategyIDs: []string{stratID.String()},
		RunIDs:      []string{runID.String()},
	})
	c.handleCommand(cmd)
	drainSend(c.send)

	c.mu.RLock()
	if !c.subscriptions.StrategyIDs[stratID] {
		t.Fatal("expected strategy subscription")
	}
	if !c.subscriptions.RunIDs[runID] {
		t.Fatal("expected run subscription")
	}
	c.mu.RUnlock()

	// Unsubscribe from the strategy.
	cmd, _ = json.Marshal(clientCommand{
		Action:      "unsubscribe",
		StrategyIDs: []string{stratID.String()},
	})
	c.handleCommand(cmd)
	drainSend(c.send)

	c.mu.RLock()
	if c.subscriptions.StrategyIDs[stratID] {
		t.Fatal("strategy subscription should be removed")
	}
	if !c.subscriptions.RunIDs[runID] {
		t.Fatal("run subscription should still exist")
	}
	c.mu.RUnlock()

	// Subscribe all.
	cmd, _ = json.Marshal(clientCommand{Action: "subscribe_all"})
	c.handleCommand(cmd)
	drainSend(c.send)

	c.mu.RLock()
	if !c.subscriptions.AllEvents {
		t.Fatal("expected AllEvents after subscribe_all")
	}
	c.mu.RUnlock()

	// Unsubscribe all.
	cmd, _ = json.Marshal(clientCommand{Action: "unsubscribe_all"})
	c.handleCommand(cmd)
	drainSend(c.send)

	c.mu.RLock()
	if c.subscriptions.AllEvents {
		t.Fatal("AllEvents should be false after unsubscribe_all")
	}
	if len(c.subscriptions.StrategyIDs) != 0 || len(c.subscriptions.RunIDs) != 0 {
		t.Fatal("all subscriptions should be cleared")
	}
	c.mu.RUnlock()
}

func TestClientHandleCommandInvalid(t *testing.T) {
	t.Parallel()

	hub := NewHub(slog.Default())
	go hub.Run()
	defer hub.Stop()

	c := &Client{
		hub:  hub,
		send: make(chan []byte, sendBufferSize),
		subscriptions: Subscriptions{
			StrategyIDs: make(map[uuid.UUID]bool),
			RunIDs:      make(map[uuid.UUID]bool),
		},
	}

	// Invalid JSON.
	c.handleCommand([]byte("{invalid"))
	msg := drainSend(c.send)
	if msg == nil {
		t.Fatal("expected error message for invalid JSON")
	}
	var errResp map[string]string
	if err := json.Unmarshal(msg, &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp["type"] != "error" {
		t.Fatalf("expected error type, got %q", errResp["type"])
	}

	// Unknown action.
	cmd, _ := json.Marshal(clientCommand{Action: "foobar"})
	c.handleCommand(cmd)
	msg = drainSend(c.send)
	if msg == nil {
		t.Fatal("expected error message for unknown action")
	}

	// Subscribe with invalid UUID should return an error, not "ok".
	cmd, _ = json.Marshal(clientCommand{
		Action:      "subscribe",
		StrategyIDs: []string{"not-a-uuid"},
	})
	c.handleCommand(cmd)
	msg = drainSend(c.send)
	if msg == nil {
		t.Fatal("expected error message for invalid UUID")
	}
	var invalidResp map[string]string
	if err := json.Unmarshal(msg, &invalidResp); err != nil {
		t.Fatalf("unmarshal invalid uuid response: %v", err)
	}
	if invalidResp["type"] != "error" {
		t.Fatalf("expected error type for invalid UUID, got %q", invalidResp["type"])
	}
}

// ---------------------------------------------------------------------------
// WebSocket handler integration test
// ---------------------------------------------------------------------------

func TestWebSocketEndpoint(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	go srv.hub.Run()
	defer srv.hub.Stop()

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}

	// Subscribe to all events.
	subCmd, _ := json.Marshal(clientCommand{Action: "subscribe_all"})
	if err := conn.WriteMessage(websocket.TextMessage, subCmd); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}

	// Read the subscribe acknowledgement.
	_, ackMsg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read ack: %v", err)
	}
	var ack map[string]string
	if err := json.Unmarshal(ackMsg, &ack); err != nil {
		t.Fatalf("unmarshal ack: %v", err)
	}
	if ack["status"] != "ok" {
		t.Fatalf("ack status = %q, want %q", ack["status"], "ok")
	}

	// Broadcast an event.
	srv.hub.Broadcast(WSMessage{
		Type:       EventPipelineStart,
		StrategyID: uuid.New(),
		Timestamp:  time.Now(),
		Data:       map[string]string{"ticker": "AAPL"},
	})

	// Read the broadcasted event.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, eventMsg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read event: %v", err)
	}

	var event WSMessage
	if err := json.Unmarshal(eventMsg, &event); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if event.Type != EventPipelineStart {
		t.Fatalf("event type = %q, want %q", event.Type, EventPipelineStart)
	}
}

func TestWebSocketSubscriptionFiltering(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	go srv.hub.Run()
	defer srv.hub.Stop()

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	targetStrategy := uuid.New()

	// Subscribe to a specific strategy.
	subCmd, _ := json.Marshal(clientCommand{
		Action:      "subscribe",
		StrategyIDs: []string{targetStrategy.String()},
	})
	if err := conn.WriteMessage(websocket.TextMessage, subCmd); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}

	// Read the ack.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read ack: %v", err)
	}

	// Broadcast an event for a different strategy (should not arrive).
	srv.hub.Broadcast(WSMessage{
		Type:       EventOrderSubmitted,
		StrategyID: uuid.New(),
		Timestamp:  time.Now(),
	})

	// Broadcast an event for the subscribed strategy (should arrive).
	srv.hub.Broadcast(WSMessage{
		Type:       EventOrderFilled,
		StrategyID: targetStrategy,
		Timestamp:  time.Now(),
	})

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, eventMsg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read event: %v", err)
	}

	var event WSMessage
	if err := json.Unmarshal(eventMsg, &event); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if event.Type != EventOrderFilled {
		t.Fatalf("event type = %q, want %q (filtering failed)", event.Type, EventOrderFilled)
	}
}

func TestWebSocketDisconnection(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	go srv.hub.Run()
	defer srv.hub.Stop()

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Wait for the client to be registered.
	waitFor(t, func() bool { return srv.hub.ClientCount() == 1 })

	// Close the connection from client side.
	conn.Close()

	// Hub should unregister the client.
	waitFor(t, func() bool { return srv.hub.ClientCount() == 0 })
}

func TestWSMessageTypes(t *testing.T) {
	t.Parallel()

	// Verify all event type constants are valid non-empty strings.
	eventTypes := []EventType{
		EventPipelineStart,
		EventAgentDecision,
		EventDebateRound,
		EventSignal,
		EventOrderSubmitted,
		EventOrderFilled,
		EventPositionUpdate,
		EventCircuitBreaker,
		EventError,
	}

	for _, et := range eventTypes {
		if et == "" {
			t.Fatal("event type must not be empty")
		}
	}

	// Verify WSMessage serialization.
	msg := WSMessage{
		Type:       EventPipelineStart,
		StrategyID: uuid.New(),
		RunID:      uuid.New(),
		Timestamp:  time.Now(),
		Data:       map[string]string{"key": "value"},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal WSMessage: %v", err)
	}

	var decoded WSMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal WSMessage: %v", err)
	}
	if decoded.Type != msg.Type {
		t.Fatalf("type = %q, want %q", decoded.Type, msg.Type)
	}
	if decoded.StrategyID != msg.StrategyID {
		t.Fatalf("strategy_id mismatch")
	}
	if decoded.RunID != msg.RunID {
		t.Fatalf("run_id mismatch")
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// waitFor polls cond until it returns true or a timeout expires.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("waitFor: timed out")
}

// assertReceive asserts that at least one message is received on ch within a
// reasonable time.
func assertReceive(t *testing.T, ch <-chan []byte, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: expected message, timed out", name)
	}
}

// assertNoReceive asserts that no messages are available on ch.
func assertNoReceive(t *testing.T, ch <-chan []byte, name string) {
	t.Helper()
	select {
	case msg := <-ch:
		t.Fatalf("%s: unexpected message: %s", name, string(msg))
	case <-time.After(50 * time.Millisecond):
		// Good — nothing received.
	}
}

// drainSend reads and returns the first message from ch, or nil if none is
// available within a short timeout.
func drainSend(ch <-chan []byte) []byte {
	select {
	case msg := <-ch:
		return msg
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}
