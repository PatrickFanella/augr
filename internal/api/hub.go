package api

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// EventType represents the type of WebSocket event.
type EventType string

// Supported event types.
//
// Translation chain: Go agent.PipelineEventType (internal/agent/event.go)
// is mapped to API EventType (this file) which is sent over WebSocket and
// consumed by the TypeScript WebSocketEventType (web/src/lib/api/types.ts).
// All three vocabularies must stay in sync; see
// TestEventTypeVocabularyConsistency in event_consistency_test.go.
const (
	EventPipelineStart   EventType = "pipeline_start"
	EventAgentDecision   EventType = "agent_decision"
	EventDebateRound     EventType = "debate_round"
	EventSignal          EventType = "signal"
	EventOrderSubmitted  EventType = "order_submitted"
	EventOrderFilled     EventType = "order_filled"
	EventPositionUpdate  EventType = "position_update"
	EventCircuitBreaker  EventType = "circuit_breaker"
	EventError           EventType = "error"
	EventPipelineHealth  EventType = "pipeline_health"
)

// WSMessage is the envelope for every WebSocket event sent to clients.
type WSMessage struct {
	Type       EventType `json:"type"`
	StrategyID uuid.UUID `json:"strategy_id,omitempty"`
	RunID      uuid.UUID `json:"run_id,omitempty"`
	Data       any       `json:"data,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// broadcastMessage carries pre-parsed routing info alongside the raw JSON
// bytes so that subscription matching does not re-unmarshal per client.
type broadcastMessage struct {
	data       []byte
	strategyID uuid.UUID
	runID      uuid.UUID
}

// Hub manages all active WebSocket clients and broadcasts events to
// subscribers. A single goroutine (Run) serialises register, unregister,
// and broadcast operations.
type Hub struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan broadcastMessage

	mu     sync.RWMutex // protects clients for ClientCount
	logger *slog.Logger
	done   chan struct{}
}

// NewHub creates a ready-to-use Hub. Call Run() in a goroutine to start it.
func NewHub(logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan broadcastMessage, 256),
		logger:     logger,
		done:       make(chan struct{}),
	}
}

// Run is the main event loop. It must be called exactly once in its own
// goroutine. It returns when Stop() is called.
func (h *Hub) Run() {
	for {
		select {
		case <-h.done:
			h.mu.Lock()
			for c := range h.clients {
				close(c.send)
				delete(h.clients, c)
			}
			h.mu.Unlock()
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			h.logger.Info("ws client registered", slog.Int("total", h.ClientCount()))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			h.logger.Info("ws client unregistered", slog.Int("total", h.ClientCount()))

		case bm := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				if client.matchesParsed(bm.strategyID, bm.runID) {
					select {
					case client.send <- bm.data:
					default:
						// Slow consumer — drop the client.
						delete(h.clients, client)
						close(client.send)
						h.logger.Warn("ws client dropped (slow consumer)")
					}
				}
			}
			h.mu.Unlock()
		}
	}
}

// Stop shuts down the hub event loop.
func (h *Hub) Stop() {
	select {
	case <-h.done:
		// Already stopped.
	default:
		close(h.done)
	}
}

// Broadcast marshals msg to JSON and sends it to all matching subscribers.
func (h *Hub) Broadcast(msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("ws broadcast marshal error", slog.String("error", err.Error()))
		return
	}
	bm := broadcastMessage{
		data:       data,
		strategyID: msg.StrategyID,
		runID:      msg.RunID,
	}
	select {
	case h.broadcast <- bm:
	default:
		h.logger.Warn("ws broadcast channel full, dropping message")
	}
}

// ClientCount returns the number of currently connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
