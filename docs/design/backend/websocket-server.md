---
title: "WebSocket Server"
date: 2026-03-20
tags: [backend, websocket, real-time, streaming]
---

# WebSocket Server

Real-time event streaming from the Go backend to the React frontend using a hub-and-client pattern.

## Architecture

```
Pipeline Events (channels)
         │
         ▼
    ┌─────────┐
    │   Hub   │ ──── manages all connections
    │         │ ──── routes events to subscribers
    └─────────┘
     /    |    \
    ▼     ▼     ▼
 Client Client Client  ──── one goroutine pair per browser tab
```

## Hub Implementation

```go
// internal/api/websocket/hub.go
type Hub struct {
    clients    map[*Client]bool
    broadcast  chan []byte
    register   chan *Client
    unregister chan *Client
    mu         sync.RWMutex
}

func NewHub() *Hub {
    return &Hub{
        clients:    make(map[*Client]bool),
        broadcast:  make(chan []byte, 256),
        register:   make(chan *Client),
        unregister: make(chan *Client),
    }
}

func (h *Hub) Run() {
    for {
        select {
        case client := <-h.register:
            h.mu.Lock()
            h.clients[client] = true
            h.mu.Unlock()

        case client := <-h.unregister:
            h.mu.Lock()
            if _, ok := h.clients[client]; ok {
                delete(h.clients, client)
                close(client.send)
            }
            h.mu.Unlock()

        case message := <-h.broadcast:
            h.mu.RLock()
            for client := range h.clients {
                if client.matchesSubscription(message) {
                    select {
                    case client.send <- message:
                    default:
                        // client buffer full — disconnect slow consumer
                        close(client.send)
                        delete(h.clients, client)
                    }
                }
            }
            h.mu.RUnlock()
        }
    }
}
```

## Client Management

```go
// internal/api/websocket/client.go
type Client struct {
    hub           *Hub
    conn          *websocket.Conn
    send          chan []byte
    subscriptions Subscriptions
}

type Subscriptions struct {
    StrategyIDs map[uuid.UUID]bool
    RunIDs      map[uuid.UUID]bool
    AllEvents   bool
}

func (c *Client) matchesSubscription(msg []byte) bool {
    if c.subscriptions.AllEvents {
        return true
    }
    // Parse message to check strategy_id or run_id
    var event struct {
        StrategyID uuid.UUID `json:"strategy_id"`
        RunID      uuid.UUID `json:"run_id"`
    }
    json.Unmarshal(msg, &event)
    return c.subscriptions.StrategyIDs[event.StrategyID] ||
           c.subscriptions.RunIDs[event.RunID]
}
```

Each client has two goroutines:

- **readPump**: Reads subscription/unsubscription commands from the browser
- **writePump**: Sends events from the `send` channel to the browser

## Integration with Orchestrator

The orchestrator emits events via a channel that the hub consumes:

```go
// In the HTTP handler that starts a pipeline run
func (h *RunHandler) StartRun(w http.ResponseWriter, r *http.Request) {
    events := make(chan agent.PipelineEvent, 100)

    state := &agent.PipelineState{
        // ...
        Events: events,
    }

    // Forward pipeline events to WebSocket hub
    go func() {
        for event := range events {
            msg, _ := json.Marshal(WSMessage{
                Type:       event.Type,
                RunID:      event.RunID,
                StrategyID: strategy.ID,
                Data:       event.Data,
                Timestamp:  event.Timestamp,
            })
            h.hub.broadcast <- msg
        }
    }()

    // Run pipeline asynchronously
    go h.orchestrator.Run(r.Context(), state)
}
```

## Message Types

See [[api-design]] for the full list of WebSocket event types. Key events:

| Event             | Payload                               | Frontend Use               |
| ----------------- | ------------------------------------- | -------------------------- |
| `pipeline_start`  | `{run_id, ticker, strategy_id}`       | Show "running" indicator   |
| `agent_decision`  | `{agent_role, phase, output_preview}` | Update agent viz node      |
| `debate_round`    | `{phase, round, bull/bear content}`   | Animate debate UI          |
| `signal`          | `{signal, confidence}`                | Flash BUY/SELL/HOLD badge  |
| `order_submitted` | `{order_id, ticker, side, qty}`       | Add to pending orders      |
| `order_filled`    | `{order_id, fill_price, fill_qty}`    | Move to filled, update P&L |
| `circuit_breaker` | `{reason, breaker_type}`              | Show alert banner          |

## Connection Lifecycle

1. Client connects to `ws://localhost:8080/ws?token=<jwt>`
2. Server validates JWT, creates `Client`, registers with Hub
3. Client sends subscription messages: `{"action": "subscribe", "strategy_ids": [...]}`
4. Server pushes matching events
5. Client disconnects → `unregister` from Hub → goroutines exit
6. Heartbeat ping/pong every 30 seconds to detect stale connections

## Scaling Considerations

For a single-server deployment, the hub pattern is sufficient. If scaling to multiple servers:

- Use PostgreSQL `LISTEN/NOTIFY` or Redis pub/sub to fan events across servers
- Each server runs its own Hub
- Events published to shared channel, each Hub re-broadcasts to local clients

---

**Related:** [[api-design]] · [[agent-visualization]] · [[dashboard-design]] · [[agent-orchestration-engine]]
