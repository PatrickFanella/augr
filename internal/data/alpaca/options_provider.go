package alpaca

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const (
	alpacaDataBaseURL = "https://data.alpaca.markets"
	defaultTimeout    = 30 * time.Second
)

// OptionsDataProvider fetches options data from Alpaca's market data API.
type OptionsDataProvider struct {
	apiKey    string
	apiSecret string
	baseURL   string
	client    *http.Client
	logger    *slog.Logger
}

var _ data.OptionsDataProvider = (*OptionsDataProvider)(nil)

// NewOptionsDataProvider constructs an Alpaca options data provider.
// If logger is nil, slog.Default() is used.
func NewOptionsDataProvider(apiKey, apiSecret string, logger *slog.Logger) *OptionsDataProvider {
	if logger == nil {
		logger = slog.Default()
	}

	return &OptionsDataProvider{
		apiKey:    strings.TrimSpace(apiKey),
		apiSecret: strings.TrimSpace(apiSecret),
		baseURL:   alpacaDataBaseURL,
		client: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: logger,
	}
}

// SetBaseURL overrides the configured base URL. This is primarily useful for testing.
func (p *OptionsDataProvider) SetBaseURL(baseURL string) {
	if p != nil {
		p.baseURL = baseURL
	}
}

// SetHTTPClient replaces the underlying HTTP client. This is primarily useful for testing.
func (p *OptionsDataProvider) SetHTTPClient(client *http.Client) {
	if p != nil && client != nil {
		p.client = client
	}
}

// ------------------------------------------------------------------
// Options chain snapshots
// ------------------------------------------------------------------

// snapshotsResponse is the top-level response from the Alpaca options snapshots endpoint.
type snapshotsResponse struct {
	Snapshots     map[string]optionSnapshot `json:"snapshots"`
	NextPageToken string                    `json:"next_page_token"`
}

type optionSnapshot struct {
	LatestTrade       *optionTrade  `json:"latestTrade"`
	LatestQuote       *optionQuote  `json:"latestQuote"`
	ImpliedVolatility *float64      `json:"impliedVolatility"`
	Greeks            *optionGreeks `json:"greeks"`
}

type optionTrade struct {
	Price float64 `json:"p"`
	Size  float64 `json:"s"`
	Time  string  `json:"t"`
}

type optionQuote struct {
	AskPrice float64 `json:"ap"`
	AskSize  float64 `json:"as"`
	BidPrice float64 `json:"bp"`
	BidSize  float64 `json:"bs"`
	Time     string  `json:"t"`
}

type optionGreeks struct {
	Delta float64 `json:"delta"`
	Gamma float64 `json:"gamma"`
	Rho   float64 `json:"rho"`
	Theta float64 `json:"theta"`
	Vega  float64 `json:"vega"`
}

// GetOptionsChain returns option snapshots (price + Greeks) for an underlying.
// Snapshots are fetched from Alpaca and then filtered client-side by expiry and optionType.
func (p *OptionsDataProvider) GetOptionsChain(
	ctx context.Context,
	underlying string,
	expiry time.Time,
	optionType domain.OptionType,
) ([]domain.OptionSnapshot, error) {
	if p == nil {
		return nil, fmt.Errorf("alpaca/options: provider is nil")
	}

	underlying = strings.TrimSpace(strings.ToUpper(underlying))
	if underlying == "" {
		return nil, fmt.Errorf("alpaca/options: underlying ticker is required")
	}

	var allSnapshots []domain.OptionSnapshot
	var pageToken string

	for {
		params := url.Values{}
		params.Set("feed", "indicative")
		params.Set("limit", "100")
		if pageToken != "" {
			params.Set("page_token", pageToken)
		}

		requestPath := fmt.Sprintf("/v1beta1/options/snapshots/%s", url.PathEscape(underlying))
		body, err := p.doGet(ctx, requestPath, params)
		if err != nil {
			return nil, fmt.Errorf("alpaca/options: chain request failed: %w", err)
		}

		var resp snapshotsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("alpaca/options: unmarshal chain response: %w", err)
		}

		for occSymbol, snap := range resp.Snapshots {
			parsed, err := domain.ParseOCC(occSymbol)
			if err != nil {
				// Skip unparseable OCC symbols.
				p.logger.Debug("alpaca/options: skipping unparseable OCC symbol",
					slog.String("symbol", occSymbol),
					slog.Any("error", err),
				)
				continue
			}

			// Client-side filters.
			if !expiry.IsZero() && !parsed.Expiry.Equal(expiry) {
				continue
			}
			if optionType != "" && parsed.OptionType != optionType {
				continue
			}

			ds := domain.OptionSnapshot{
				Contract: *parsed,
			}

			if snap.Greeks != nil {
				ds.Greeks = domain.OptionGreeks{
					Delta: snap.Greeks.Delta,
					Gamma: snap.Greeks.Gamma,
					Theta: snap.Greeks.Theta,
					Vega:  snap.Greeks.Vega,
					Rho:   snap.Greeks.Rho,
				}
			}
			if snap.ImpliedVolatility != nil {
				ds.Greeks.IV = *snap.ImpliedVolatility
			}

			if snap.LatestQuote != nil {
				ds.Bid = snap.LatestQuote.BidPrice
				ds.Ask = snap.LatestQuote.AskPrice
				if ds.Bid > 0 && ds.Ask > 0 {
					ds.Mid = (ds.Bid + ds.Ask) / 2
				}
			}

			if snap.LatestTrade != nil {
				ds.Last = snap.LatestTrade.Price
			}

			allSnapshots = append(allSnapshots, ds)
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return allSnapshots, nil
}

// ------------------------------------------------------------------
// Options OHLCV bars
// ------------------------------------------------------------------

// barsResponse is the top-level response from the Alpaca options bars endpoint.
type barsResponse struct {
	Bars          map[string][]optionBar `json:"bars"`
	NextPageToken string                 `json:"next_page_token"`
}

type optionBar struct {
	Timestamp string  `json:"t"`
	Open      float64 `json:"o"`
	High      float64 `json:"h"`
	Low       float64 `json:"l"`
	Close     float64 `json:"c"`
	Volume    float64 `json:"v"`
	Count     float64 `json:"n"`
	VWAP      float64 `json:"vw"`
}

// GetOptionsOHLCV returns historical OHLCV bars for a specific options contract.
func (p *OptionsDataProvider) GetOptionsOHLCV(
	ctx context.Context,
	occSymbol string,
	timeframe data.Timeframe,
	from, to time.Time,
) ([]domain.OHLCV, error) {
	if p == nil {
		return nil, fmt.Errorf("alpaca/options: provider is nil")
	}

	occSymbol = strings.TrimSpace(occSymbol)
	if occSymbol == "" {
		return nil, fmt.Errorf("alpaca/options: OCC symbol is required")
	}

	alpacaTF, err := mapTimeframe(timeframe)
	if err != nil {
		return nil, err
	}

	var allBars []domain.OHLCV
	var pageToken string

	for {
		params := url.Values{}
		params.Set("symbols", occSymbol)
		params.Set("timeframe", alpacaTF)
		params.Set("start", from.UTC().Format(time.RFC3339))
		params.Set("end", to.UTC().Format(time.RFC3339))
		params.Set("limit", "1000")
		if pageToken != "" {
			params.Set("page_token", pageToken)
		}

		body, err := p.doGet(ctx, "/v1beta1/options/bars", params)
		if err != nil {
			return nil, fmt.Errorf("alpaca/options: ohlcv request failed: %w", err)
		}

		var resp barsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("alpaca/options: unmarshal ohlcv response: %w", err)
		}

		bars, ok := resp.Bars[occSymbol]
		if !ok {
			// Try without O: prefix or with it.
			for _, b := range resp.Bars {
				bars = b
				break
			}
		}

		for _, bar := range bars {
			ts, parseErr := time.Parse(time.RFC3339, bar.Timestamp)
			if parseErr != nil {
				ts, parseErr = time.Parse(time.RFC3339Nano, bar.Timestamp)
				if parseErr != nil {
					continue
				}
			}

			allBars = append(allBars, domain.OHLCV{
				Timestamp: ts.UTC(),
				Open:      bar.Open,
				High:      bar.High,
				Low:       bar.Low,
				Close:     bar.Close,
				Volume:    bar.Volume,
			})
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return allBars, nil
}

// ------------------------------------------------------------------
// HTTP helpers
// ------------------------------------------------------------------

func (p *OptionsDataProvider) doGet(ctx context.Context, requestPath string, params url.Values) ([]byte, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("alpaca/options: API key is required")
	}
	if p.apiSecret == "" {
		return nil, fmt.Errorf("alpaca/options: API secret is required")
	}

	u, err := url.Parse(p.baseURL)
	if err != nil {
		return nil, fmt.Errorf("alpaca/options: parse base url: %w", err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(requestPath, "/")
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("alpaca/options: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("APCA-API-KEY-ID", p.apiKey)
	req.Header.Set("APCA-API-SECRET-KEY", p.apiSecret)

	startedAt := time.Now()
	p.logger.Debug("alpaca/options: sending request",
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
	)

	resp, err := p.client.Do(req)
	if err != nil {
		p.logger.Warn("alpaca/options: request failed",
			slog.String("path", req.URL.Path),
			slog.Any("error", err),
			slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
		)
		return nil, fmt.Errorf("alpaca/options: do request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("alpaca/options: read response body: %w", err)
	}

	p.logger.Debug("alpaca/options: received response",
		slog.String("path", req.URL.Path),
		slog.Int("status", resp.StatusCode),
		slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
	)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("alpaca/options: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return body, nil
}

// mapTimeframe converts a data.Timeframe to the Alpaca bar timeframe string.
func mapTimeframe(tf data.Timeframe) (string, error) {
	switch tf {
	case data.Timeframe1m:
		return "1Min", nil
	case data.Timeframe5m:
		return "5Min", nil
	case data.Timeframe15m:
		return "15Min", nil
	case data.Timeframe1h:
		return "1Hour", nil
	case data.Timeframe1d:
		return "1Day", nil
	default:
		return "", fmt.Errorf("alpaca/options: unsupported timeframe %q", tf)
	}
}
