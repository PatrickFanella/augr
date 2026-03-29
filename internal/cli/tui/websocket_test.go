package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	internalapi "github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func TestConnectWebSocketSubscribesAndStreamsEvents(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	runID := uuid.New()
	var handlerErrs []error
	var handlerErrsMu sync.Mutex
	recordHandlerError := func(err error) {
		handlerErrsMu.Lock()
		handlerErrs = append(handlerErrs, err)
		handlerErrsMu.Unlock()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			recordHandlerError(fmt.Errorf("upgrade: %w", err))
			http.Error(w, "failed to upgrade websocket", http.StatusInternalServerError)
			return
		}
		defer conn.Close()

		var command map[string]string
		if err := conn.ReadJSON(&command); err != nil {
			recordHandlerError(fmt.Errorf("read command: %w", err))
			return
		}
		if command["action"] != "subscribe_all" {
			recordHandlerError(fmt.Errorf("action = %q, want subscribe_all", command["action"]))
			return
		}

		if err := conn.WriteJSON(map[string]string{"status": "ok", "action": "subscribe_all"}); err != nil {
			recordHandlerError(fmt.Errorf("write ack: %w", err))
			return
		}
		if err := conn.WriteJSON(internalapi.WSMessage{
			Type:      internalapi.EventSignal,
			RunID:     runID,
			Timestamp: time.Now().UTC(),
			Data:      map[string]string{"ticker": "AAPL", "signal": "buy"},
		}); err != nil {
			recordHandlerError(fmt.Errorf("write event: %w", err))
		}
	}))
	defer server.Close()
	t.Cleanup(func() {
		handlerErrsMu.Lock()
		defer handlerErrsMu.Unlock()
		if len(handlerErrs) == 0 {
			return
		}
		t.Fatalf("mock websocket handler errors: %v", handlerErrs)
	})

	source, err := ConnectWebSocket(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("ConnectWebSocket() error = %v", err)
	}
	defer source.Close()

	select {
	case msg := <-source.Messages():
		raw, err := json.Marshal(msg.Data)
		if err != nil {
			t.Fatalf("marshal msg data: %v", err)
		}
		if msg.RunID != runID {
			t.Fatalf("RunID = %s, want %s", msg.RunID, runID)
		}
		if !strings.Contains(string(raw), "AAPL") {
			t.Fatalf("event data = %s, want ticker payload", raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for websocket event")
	}
}
