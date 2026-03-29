package tui

import (
	"context"
	"errors"
	"net/http"
	"sync"

	internalapi "github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/gorilla/websocket"
)

type websocketSource struct {
	conn      *websocket.Conn
	messages  chan internalapi.WSMessage
	closeOnce sync.Once
	done      chan struct{}
}

type websocketAck struct {
	Status string `json:"status"`
	Action string `json:"action"`
	Error  string `json:"error"`
}

func ConnectWebSocket(ctx context.Context, endpoint string, headers http.Header) (EventSource, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		return nil, err
	}

	source := &websocketSource{
		conn:     conn,
		messages: make(chan internalapi.WSMessage, 32),
		done:     make(chan struct{}),
	}

	if err := conn.WriteJSON(map[string]string{"action": "subscribe_all"}); err != nil {
		conn.Close()
		return nil, err
	}

	var ack websocketAck
	if err := conn.ReadJSON(&ack); err != nil {
		conn.Close()
		return nil, err
	}
	if ack.Status != "ok" {
		conn.Close()
		if ack.Error != "" {
			return nil, errors.New(ack.Error)
		}
		return nil, errors.New("websocket subscription failed")
	}

	go source.readLoop()
	return source, nil
}

func (s *websocketSource) Messages() <-chan internalapi.WSMessage {
	return s.messages
}

func (s *websocketSource) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.done)
		err = s.conn.Close()
	})
	return err
}

func (s *websocketSource) readLoop() {
	defer close(s.messages)
	for {
		var msg internalapi.WSMessage
		if err := s.conn.ReadJSON(&msg); err != nil {
			return
		}
		select {
		case s.messages <- msg:
		case <-s.done:
			return
		default:
			// Drop messages rather than blocking the websocket reader.
		}
	}
}
