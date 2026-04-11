package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer (subscription commands).
	maxMessageSize = 4096

	// Client send channel buffer size.
	sendBufferSize = 256
)

// newUpgrader creates a WebSocket upgrader that validates the Origin header
// against the server's configured CORS allowed origins.
func newUpgrader(allowedOrigins []string) websocket.Upgrader {
	var allowAll bool
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
		}
		allowed[o] = struct{}{}
	}

	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			if allowAll {
				return true
			}
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true // non-browser clients typically omit Origin
			}
			_, ok := allowed[origin]
			return ok
		},
	}
}

// Subscriptions tracks what a client is interested in.
type Subscriptions struct {
	StrategyIDs map[uuid.UUID]bool
	RunIDs      map[uuid.UUID]bool
	AllEvents   bool
}

// Client is a middleman between the WebSocket connection and the Hub.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte

	mu            sync.RWMutex
	subscriptions Subscriptions
}

// clientCommand is the JSON schema of messages sent by the client.
type clientCommand struct {
	Action      string   `json:"action"`
	StrategyIDs []string `json:"strategy_ids,omitempty"`
	RunIDs      []string `json:"run_ids,omitempty"`
}

// matchesParsed checks whether a pre-parsed event envelope should be
// delivered to this client. This avoids repeated JSON unmarshalling in the
// hub's broadcast loop.
func (c *Client) matchesParsed(strategyID, runID uuid.UUID) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.subscriptions.AllEvents {
		return true
	}
	if strategyID != uuid.Nil && c.subscriptions.StrategyIDs[strategyID] {
		return true
	}
	if runID != uuid.Nil && c.subscriptions.RunIDs[runID] {
		return true
	}
	return false
}

// matchesSubscription checks whether msg should be delivered to this client.
// Used only in tests; the hub's broadcast loop uses matchesParsed instead.
func (c *Client) matchesSubscription(msg []byte) bool {
	var envelope struct {
		StrategyID uuid.UUID `json:"strategy_id"`
		RunID      uuid.UUID `json:"run_id"`
	}
	if err := json.Unmarshal(msg, &envelope); err != nil {
		return false
	}
	return c.matchesParsed(envelope.StrategyID, envelope.RunID)
}

// readPump reads subscription commands from the client. It runs in its own
// goroutine and unregisters the client when the connection is closed.
func (c *Client) readPump() {
	defer func() {
		select {
		case c.hub.unregister <- c:
		case <-c.hub.done:
		}
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.hub.logger.Warn("ws read error", slog.String("error", err.Error()))
			}
			return
		}
		c.handleCommand(message)
	}
}

// writePump writes messages from the send channel to the WebSocket connection.
// It also sends periodic ping frames.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel.
				_ = c.conn.WriteMessage(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleCommand processes a subscription/unsubscription command from the client.
func (c *Client) handleCommand(raw []byte) {
	var cmd clientCommand
	if err := json.Unmarshal(raw, &cmd); err != nil {
		c.sendError("invalid command JSON")
		return
	}

	switch cmd.Action {
	case "subscribe":
		if errs := c.applySubscribe(cmd); len(errs) > 0 {
			c.sendError("invalid UUID format: " + strings.Join(errs, ", "))
			return
		}
	case "unsubscribe":
		c.applyUnsubscribe(cmd)
	case "subscribe_all":
		c.mu.Lock()
		c.subscriptions.AllEvents = true
		c.mu.Unlock()
	case "unsubscribe_all":
		c.mu.Lock()
		c.subscriptions = Subscriptions{
			StrategyIDs: make(map[uuid.UUID]bool),
			RunIDs:      make(map[uuid.UUID]bool),
		}
		c.mu.Unlock()
	default:
		c.sendError("unknown action: " + cmd.Action)
		return
	}

	// Acknowledge the command.
	ack, _ := json.Marshal(map[string]string{"status": "ok", "action": cmd.Action})
	select {
	case c.send <- ack:
	default:
	}
}

func (c *Client) applySubscribe(cmd clientCommand) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var invalid []string
	for _, raw := range cmd.StrategyIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			invalid = append(invalid, raw)
			continue
		}
		c.subscriptions.StrategyIDs[id] = true
	}
	for _, raw := range cmd.RunIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			invalid = append(invalid, raw)
			continue
		}
		c.subscriptions.RunIDs[id] = true
	}
	return invalid
}

func (c *Client) applyUnsubscribe(cmd clientCommand) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, raw := range cmd.StrategyIDs {
		if id, err := uuid.Parse(raw); err == nil {
			delete(c.subscriptions.StrategyIDs, id)
		}
	}
	for _, raw := range cmd.RunIDs {
		if id, err := uuid.Parse(raw); err == nil {
			delete(c.subscriptions.RunIDs, id)
		}
	}
}

// sendError writes a JSON error message to the client's send channel.
func (c *Client) sendError(msg string) {
	data, _ := json.Marshal(map[string]string{"type": "error", "error": msg})
	select {
	case c.send <- data:
	default:
	}
}

// handleWebSocket upgrades an HTTP connection to a WebSocket and registers
// the resulting Client with the Hub.
//
// Authentication is enforced before the upgrade. Browser WebSocket clients
// that cannot send custom headers should supply a JWT via the "token" query
// parameter or an API key via the "api_key" query parameter.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		http.Error(w, "websocket not available", http.StatusServiceUnavailable)
		return
	}

	// Authenticate before upgrading; once the connection is upgraded the
	// HTTP response headers are committed and cannot carry a 401.
	if _, err := s.auth.AuthenticateWSRequest(r); err != nil {
		s.logger.Warn("ws authentication failed", "reason", wsAuthFailureReason(err), "remote_addr", r.RemoteAddr)
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	conn, err := s.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("ws upgrade failed", slog.String("error", err.Error()))
		return
	}

	client := &Client{
		hub:  s.hub,
		conn: conn,
		send: make(chan []byte, sendBufferSize),
		subscriptions: Subscriptions{
			StrategyIDs: make(map[uuid.UUID]bool),
			RunIDs:      make(map[uuid.UUID]bool),
		},
	}

	select {
	case s.hub.register <- client:
		go client.writePump()
		go client.readPump()
	case <-s.hub.done:
		s.logger.Error("ws hub not accepting registrations")
		_ = conn.Close()
	}
}

func wsAuthFailureReason(err error) string {
	switch {
	case errors.Is(err, errMissingCredentials):
		return "missing_credentials"
	case errors.Is(err, errExpiredToken):
		return "token_expired"
	case errors.Is(err, errInvalidToken):
		return "invalid_token"
	case errors.Is(err, errInvalidAPIKey):
		return "invalid_api_key"
	case errors.Is(err, errAPIKeyExpired):
		return "api_key_expired"
	case errors.Is(err, errAPIKeyRevoked):
		return "api_key_revoked"
	default:
		return "unknown"
	}
}
