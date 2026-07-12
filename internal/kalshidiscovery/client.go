package kalshidiscovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	dataKalshi "github.com/PatrickFanella/get-rich-quick/internal/data/kalshi"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const (
	defaultCatalogLimit = 100
	maxCatalogLimit     = 100
)

// MarketCandidate is a read-only Kalshi market catalog entry.
type MarketCandidate struct {
	Ticker       string          `json:"ticker"`
	EventTicker  string          `json:"event_ticker,omitempty"`
	Title        string          `json:"title,omitempty"`
	Category     string          `json:"category,omitempty"`
	Status       string          `json:"status,omitempty"`
	Result       string          `json:"result,omitempty"`
	YesBid       float64         `json:"yes_bid"`
	YesAsk       float64         `json:"yes_ask"`
	NoBid        float64         `json:"no_bid"`
	NoAsk        float64         `json:"no_ask"`
	Volume       float64         `json:"volume"`
	OpenInterest float64         `json:"open_interest"`
	CloseTime    *time.Time      `json:"close_time,omitempty"`
	Raw          json.RawMessage `json:"raw,omitempty"`
}

// ListOptions controls a single market catalog page request.
type ListOptions struct {
	Limit    int
	Cursor   string
	Status   string
	Category string
}

// SyncResult summarizes a catalog sync run.
type SyncResult struct {
	Fetched            int
	SnapshotsPersisted int
	WatchedUpserted    int
}

// Client wraps the public Kalshi client for catalog sync.
type Client struct {
	api *dataKalshi.Client
}

// NewClient builds a catalog sync client.
func NewClient(api *dataKalshi.Client) *Client { return &Client{api: api} }

// ListMarkets fetches one page from /markets using the public GET endpoint.
func (c *Client) ListMarkets(ctx context.Context, opts ListOptions) ([]MarketCandidate, string, error) {
	if c == nil || c.api == nil {
		return nil, "", errors.New("kalshi discovery: client is nil")
	}

	query := url.Values{}
	query.Set("limit", strconv.Itoa(normalizeLimit(opts.Limit)))
	if cursor := strings.TrimSpace(opts.Cursor); cursor != "" {
		query.Set("cursor", cursor)
	}
	if status := strings.TrimSpace(opts.Status); status != "" {
		query.Set("status", status)
	}

	body, err := c.api.Get(ctx, "/markets", query, false)
	if err != nil {
		return nil, "", err
	}
	return decodeMarketListResponse(body)
}

// SyncCatalog fetches every catalog page, persists snapshots, and upserts watched
// markets only when selected returns true for a candidate.
func (c *Client) SyncCatalog(
	ctx context.Context,
	snapshots repository.KalshiMarketSnapshotsRepository,
	watched repository.KalshiWatchedMarketsRepository,
	opts ListOptions,
	selected func(MarketCandidate) bool,
) (SyncResult, error) {
	if snapshots == nil {
		return SyncResult{}, errors.New("kalshi discovery: snapshots repository is required")
	}

	pageOpts := opts
	if strings.TrimSpace(pageOpts.Status) == "" {
		pageOpts.Status = "open"
	}
	pageOpts.Limit = normalizeLimit(pageOpts.Limit)

	var result SyncResult
	for {
		candidates, cursor, err := c.ListMarkets(ctx, pageOpts)
		if err != nil {
			return result, err
		}
		for _, candidate := range candidates {
			if err := snapshots.Create(ctx, candidate.ToSnapshot()); err != nil {
				return result, err
			}
			result.Fetched++
			result.SnapshotsPersisted++
			if watched != nil && selected != nil && selected(candidate) {
				if err := watched.Upsert(ctx, candidate.ToWatchedMarket()); err != nil {
					return result, err
				}
				result.WatchedUpserted++
			}
		}
		if strings.TrimSpace(cursor) == "" {
			return result, nil
		}
		pageOpts.Cursor = cursor
	}
}

// ToSnapshot converts the candidate into a persistence snapshot.
func (m MarketCandidate) ToSnapshot() *domain.KalshiMarketSnapshot {
	return &domain.KalshiMarketSnapshot{
		Ticker:       m.Ticker,
		Title:        m.Title,
		Status:       m.Status,
		YesBid:       m.YesBid,
		YesAsk:       m.YesAsk,
		NoBid:        m.NoBid,
		NoAsk:        m.NoAsk,
		Volume:       m.Volume,
		OpenInterest: m.OpenInterest,
		CloseTime:    m.CloseTime,
		Raw:          append(json.RawMessage(nil), m.Raw...),
	}
}

// ToWatchedMarket converts the candidate into a watched market record.
func (m MarketCandidate) ToWatchedMarket() *domain.KalshiWatchedMarket {
	return &domain.KalshiWatchedMarket{
		Ticker:      m.Ticker,
		EventTicker: m.EventTicker,
		Title:       m.Title,
		Category:    m.Category,
		Status:      m.Status,
		CloseTime:   m.CloseTime,
	}
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultCatalogLimit
	}
	if limit > maxCatalogLimit {
		return maxCatalogLimit
	}
	return limit
}

func decodeMarketListResponse(body []byte) ([]MarketCandidate, string, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, "", errors.New("kalshi discovery: empty markets response")
	}

	var wrapped struct {
		Markets json.RawMessage `json:"markets"`
		Cursor  string          `json:"cursor"`
	}
	if err := json.Unmarshal(trimmed, &wrapped); err == nil && wrapped.Markets != nil {
		candidates, err := decodeMarketCandidates(wrapped.Markets)
		return candidates, strings.TrimSpace(wrapped.Cursor), err
	}

	if trimmed[0] == '[' {
		candidates, err := decodeMarketCandidates(trimmed)
		return candidates, "", err
	}

	return nil, "", fmt.Errorf("kalshi discovery: unexpected markets response: %s", strings.TrimSpace(string(trimmed)))
}

func decodeMarketCandidates(body []byte) ([]MarketCandidate, error) {
	var rawItems []json.RawMessage
	if err := json.Unmarshal(body, &rawItems); err != nil {
		if bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
			return nil, nil
		}
		return nil, fmt.Errorf("kalshi discovery: decode markets array: %w", err)
	}

	out := make([]MarketCandidate, 0, len(rawItems))
	for _, raw := range rawItems {
		candidate, err := decodeMarketCandidate(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, candidate)
	}
	return out, nil
}

func decodeMarketCandidate(raw json.RawMessage) (MarketCandidate, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return MarketCandidate{}, errors.New("kalshi discovery: empty market candidate")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return MarketCandidate{}, fmt.Errorf("kalshi discovery: decode market candidate: %w", err)
	}

	candidate := MarketCandidate{Raw: append(json.RawMessage(nil), raw...)}
	var err error
	if candidate.Ticker, err = lookupString(fields, "ticker", "market_ticker", "marketTicker"); err != nil {
		return MarketCandidate{}, err
	}
	if candidate.Ticker == "" {
		return MarketCandidate{}, errors.New("kalshi discovery: market candidate missing ticker")
	}
	if candidate.EventTicker, err = lookupString(fields, "event_ticker", "eventTicker"); err != nil {
		return MarketCandidate{}, err
	}
	if candidate.Title, err = lookupString(fields, "title"); err != nil {
		return MarketCandidate{}, err
	}
	if candidate.Category, err = lookupString(fields, "category"); err != nil {
		return MarketCandidate{}, err
	}
	if candidate.Status, err = lookupString(fields, "status"); err != nil {
		return MarketCandidate{}, err
	}
	if candidate.Result, err = lookupString(fields, "result"); err != nil {
		return MarketCandidate{}, err
	}
	if candidate.YesBid, err = lookupProbability(fields, []string{"yes_bid_dollars", "yesBidDollars"}, []string{"yes_bid", "yesBid"}); err != nil {
		return MarketCandidate{}, fmt.Errorf("kalshi discovery: yes_bid: %w", err)
	}
	if candidate.YesAsk, err = lookupProbability(fields, []string{"yes_ask_dollars", "yesAskDollars"}, []string{"yes_ask", "yesAsk"}); err != nil {
		return MarketCandidate{}, fmt.Errorf("kalshi discovery: yes_ask: %w", err)
	}
	if candidate.NoBid, err = lookupProbability(fields, []string{"no_bid_dollars", "noBidDollars"}, []string{"no_bid", "noBid"}); err != nil {
		return MarketCandidate{}, fmt.Errorf("kalshi discovery: no_bid: %w", err)
	}
	if candidate.NoAsk, err = lookupProbability(fields, []string{"no_ask_dollars", "noAskDollars"}, []string{"no_ask", "noAsk"}); err != nil {
		return MarketCandidate{}, fmt.Errorf("kalshi discovery: no_ask: %w", err)
	}
	if candidate.Volume, err = lookupFloat(fields, "volume_fp", "volumeFp", "volume"); err != nil {
		return MarketCandidate{}, fmt.Errorf("kalshi discovery: volume: %w", err)
	}
	if candidate.OpenInterest, err = lookupFloat(fields, "open_interest_fp", "openInterestFp", "open_interest", "openInterest"); err != nil {
		return MarketCandidate{}, fmt.Errorf("kalshi discovery: open_interest: %w", err)
	}
	if candidate.CloseTime, err = lookupTime(fields, "close_time", "closeTime"); err != nil {
		return MarketCandidate{}, fmt.Errorf("kalshi discovery: close_time: %w", err)
	}
	return candidate, nil
}

func lookupString(fields map[string]json.RawMessage, keys ...string) (string, error) {
	raw, ok := lookupRaw(fields, keys...)
	if !ok {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return strings.TrimSpace(value), nil
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return "", err
	}
	return strings.TrimSpace(fmt.Sprint(generic)), nil
}

func lookupFloat(fields map[string]json.RawMessage, keys ...string) (float64, error) {
	raw, ok := lookupRaw(fields, keys...)
	if !ok {
		return 0, nil
	}
	return parseFloat(raw)
}

func lookupProbability(fields map[string]json.RawMessage, dollarKeys, centKeys []string) (float64, error) {
	if raw, ok := lookupRaw(fields, dollarKeys...); ok {
		value, err := parseFloat(raw)
		if err != nil {
			return 0, err
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return 0, fmt.Errorf("quote dollars %.4f out of range [0,1]", value)
		}
		return value, nil
	}
	cents, err := lookupFloat(fields, centKeys...)
	if err != nil {
		return 0, err
	}
	return dataKalshi.QuoteCentsToProbability(cents)
}

func lookupTime(fields map[string]json.RawMessage, keys ...string) (*time.Time, error) {
	raw, ok := lookupRaw(fields, keys...)
	if !ok {
		return nil, nil
	}
	value, err := parseTime(raw)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func lookupRaw(fields map[string]json.RawMessage, keys ...string) (json.RawMessage, bool) {
	for _, key := range keys {
		if raw, ok := fields[key]; ok && len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return raw, true
		}
	}
	return nil, false
}

func parseFloat(raw json.RawMessage) (float64, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return 0, errors.New("empty value")
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return 0, err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return 0, errors.New("empty value")
		}
		return strconv.ParseFloat(s, 64)
	}
	var v float64
	if err := json.Unmarshal(trimmed, &v); err != nil {
		return 0, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errors.New("non-finite value")
	}
	return v, nil
}

func parseTime(raw json.RawMessage) (time.Time, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return time.Time{}, errors.New("empty value")
	}
	var value string
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return time.Time{}, err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("empty value")
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}
