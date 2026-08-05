package signal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type PolygonMempoolSourceConfig struct {
	WSURL          string
	RPCURL         string
	WatchAddresses []string
	MaxSeenTxs     int
}

type PolygonMempoolSource struct {
	cfg    PolygonMempoolSourceConfig
	logger *slog.Logger

	mu   sync.Mutex
	seen map[string]struct{}
}

func NewPolygonMempoolSource(cfg PolygonMempoolSourceConfig, logger *slog.Logger) *PolygonMempoolSource {
	if cfg.MaxSeenTxs == 0 {
		cfg.MaxSeenTxs = 4096
	}
	if logger == nil {
		logger = slog.Default()
	}
	if len(cfg.WatchAddresses) == 0 {
		cfg.WatchAddresses = DefaultPolymarketContracts()
	}
	for i, a := range cfg.WatchAddresses {
		cfg.WatchAddresses[i] = strings.ToLower(strings.TrimSpace(a))
	}
	return &PolygonMempoolSource{cfg: cfg, logger: logger, seen: make(map[string]struct{})}
}

func DefaultPolymarketContracts() []string {
	return []string{
		"0x4bfb41d5b3570defd03c39a9a4d8de6bd8b8982e",
		"0xc5d563a36ae78145c45a50134d48a1215220f80a",
		"0xd91e80cf2e7be2e162c6513ced06f1dd0da35296",
	}
}

func (p *PolygonMempoolSource) Name() string { return "polymarket-mempool" }

func (p *PolygonMempoolSource) Start(ctx context.Context) (<-chan RawSignalEvent, error) {
	if p.cfg.WSURL == "" || p.cfg.RPCURL == "" {
		return nil, fmt.Errorf("polygon mempool: WSURL and RPCURL required")
	}
	out := make(chan RawSignalEvent, 64)
	go func() {
		defer close(out)
		for {
			if ctx.Err() != nil {
				return
			}
			if err := p.runOnce(ctx, out); err != nil {
				if ctx.Err() != nil {
					return
				}
				p.logger.Warn("polygon mempool source disconnected", slog.Any("error", err))
				select {
				case <-time.After(5 * time.Second):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (p *PolygonMempoolSource) runOnce(ctx context.Context, out chan<- RawSignalEvent) error {
	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(ctx, p.cfg.WSURL, nil)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.WriteJSON(map[string]any{"id": 1, "jsonrpc": "2.0", "method": "eth_subscribe", "params": []any{"newPendingTransactions"}}); err != nil {
		return err
	}
	var subResp map[string]any
	if err := conn.ReadJSON(&subResp); err != nil {
		return err
	}
	if _, ok := subResp["result"].(string); !ok {
		return fmt.Errorf("polygon mempool: invalid subscription response")
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			return err
		}
		params, _ := msg["params"].(map[string]any)
		hash, _ := params["result"].(string)
		if hash == "" {
			continue
		}
		if p.seenHash(hash) {
			continue
		}
		tx, err := p.fetchTx(ctx, hash)
		if err != nil {
			p.logger.Warn("polygon mempool: fetch tx failed", slog.Any("error", err))
			continue
		}
		to := strings.ToLower(tx.To)
		if !p.watched(to) {
			p.markSeen(hash)
			continue
		}
		inputLen := len(tx.InputBytes)
		toShort := shortAddr(to)
		from := strings.ToLower(tx.From)
		evt := RawSignalEvent{Source: p.Name(), Title: fmt.Sprintf("polygon mempool tx %s -> %s", shortHash(hash), toShort), Body: fmt.Sprintf("Pending Polygon transaction from %s to %s, value %s wei, input %d bytes", from, to, tx.Value, inputLen), URL: "", Metadata: map[string]any{"signal_kind": "copy_trade", "tx_hash": hash, "from": from, "to": to, "value": tx.Value, "input_size": inputLen}, ReceivedAt: time.Now()}
		p.markSeen(hash)
		select {
		case out <- evt:
		case <-ctx.Done():
			return nil
		}
	}
}

type polygonTx struct {
	From       string
	To         string
	Value      string
	InputBytes []byte
}

func (p *PolygonMempoolSource) fetchTx(ctx context.Context, hash string) (*polygonTx, error) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "eth_getTransactionByHash", "params": []any{hash}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.RPCURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := http.DefaultClient
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Result struct {
			From  string `json:"from"`
			To    string `json:"to"`
			Value string `json:"value"`
			Input string `json:"input"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return &polygonTx{From: payload.Result.From, To: payload.Result.To, Value: payload.Result.Value, InputBytes: []byte(strings.TrimPrefix(payload.Result.Input, "0x"))}, nil
}

func (p *PolygonMempoolSource) watched(to string) bool {
	for _, a := range p.cfg.WatchAddresses {
		if a == to {
			return true
		}
	}
	return false
}

func (p *PolygonMempoolSource) seenHash(hash string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.seen[hash]; ok {
		return true
	}
	p.seen[hash] = struct{}{}
	if len(p.seen) > p.cfg.MaxSeenTxs {
		p.seen = map[string]struct{}{}
	}
	return false
}

func (p *PolygonMempoolSource) markSeen(hash string) {
	p.mu.Lock()
	p.seen[hash] = struct{}{}
	p.mu.Unlock()
}

func shortHash(h string) string {
	if len(h) > 10 {
		return h[:10]
	}
	return h
}

func shortAddr(a string) string {
	if len(a) > 10 {
		return a[:10]
	}
	return a
}
