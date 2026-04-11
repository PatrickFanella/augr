package polygon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// OptionsProvider retrieves options data from Polygon/Massive.
type OptionsProvider struct {
	client *Client
}

var _ data.OptionsDataProvider = (*OptionsProvider)(nil)

// NewOptionsProvider creates a Polygon options data provider.
func NewOptionsProvider(client *Client) *OptionsProvider {
	return &OptionsProvider{client: client}
}

// GetOptionsChain fetches the options chain snapshot for an underlying ticker.
// Returns contracts with Greeks, IV, bid/ask, volume, and open interest.
func (p *OptionsProvider) GetOptionsChain(
	ctx context.Context,
	underlying string,
	expiry time.Time,
	optionType domain.OptionType,
) ([]domain.OptionSnapshot, error) {
	if p == nil || p.client == nil {
		return nil, errors.New("polygon/options: provider or client is nil")
	}
	underlying = strings.TrimSpace(strings.ToUpper(underlying))
	if underlying == "" {
		return nil, errors.New("polygon/options: underlying ticker is required")
	}

	requestPath := fmt.Sprintf("/v3/snapshot/options/%s", url.PathEscape(underlying))
	params := url.Values{
		"limit": []string{"250"},
		"order": []string{"asc"},
		"sort":  []string{"expiration_date"},
	}

	if !expiry.IsZero() {
		params.Set("expiration_date", expiry.Format("2006-01-02"))
	}
	if optionType != "" {
		params.Set("contract_type", string(optionType))
	}

	var allSnapshots []domain.OptionSnapshot
	for {
		body, err := p.client.Get(ctx, requestPath, params)
		if err != nil {
			return nil, fmt.Errorf("polygon/options: chain request failed: %w", err)
		}

		var resp optionsChainResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("polygon/options: unmarshal chain response: %w", err)
		}

		for _, r := range resp.Results {
			snap := mapChainResult(r)
			if snap.Contract.OCCSymbol != "" {
				allSnapshots = append(allSnapshots, snap)
			}
		}

		if resp.NextURL == "" {
			break
		}
		// Follow pagination
		nextURL, err := url.Parse(resp.NextURL)
		if err != nil {
			break
		}
		requestPath = nextURL.Path
		params = nextURL.Query()
	}

	return allSnapshots, nil
}

// GetOptionsOHLCV returns historical OHLCV bars for a specific options contract.
// The occSymbol should be in standard OCC format (e.g., AAPL241220C00150000).
// The Massive API requires the O: prefix which is added automatically.
func (p *OptionsProvider) GetOptionsOHLCV(
	ctx context.Context,
	occSymbol string,
	timeframe data.Timeframe,
	from, to time.Time,
) ([]domain.OHLCV, error) {
	if p == nil || p.client == nil {
		return nil, errors.New("polygon/options: provider or client is nil")
	}
	occSymbol = strings.TrimSpace(occSymbol)
	if occSymbol == "" {
		return nil, errors.New("polygon/options: OCC symbol is required")
	}

	massiveSym := domain.MassiveSymbol(occSymbol)
	mapping, err := mapTimeframe(timeframe)
	if err != nil {
		return nil, err
	}

	requestPath := fmt.Sprintf(
		"/v2/aggs/ticker/%s/range/%d/%s/%d/%d",
		url.PathEscape(massiveSym),
		mapping.multiplier,
		mapping.timespan,
		from.UTC().UnixMilli(),
		to.UTC().UnixMilli(),
	)
	params := url.Values{
		"adjusted": []string{"true"},
		"sort":     []string{"asc"},
		"limit":    []string{strconv.Itoa(polygonMaxPageSize)},
	}

	body, err := p.client.Get(ctx, requestPath, params)
	if err != nil {
		return nil, fmt.Errorf("polygon/options: ohlcv request failed: %w", err)
	}

	var resp aggregateResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("polygon/options: unmarshal ohlcv response: %w", err)
	}

	bars := make([]domain.OHLCV, 0, len(resp.Results))
	for _, r := range resp.Results {
		bars = append(bars, domain.OHLCV{
			Timestamp: time.UnixMilli(r.Timestamp).UTC(),
			Open:      r.Open,
			High:      r.High,
			Low:       r.Low,
			Close:     r.Close,
			Volume:    r.Volume,
		})
	}

	return bars, nil
}

// Response types for the Massive options chain endpoint.

type optionsChainResponse struct {
	Results []optionsChainResult `json:"results"`
	NextURL string               `json:"next_url"`
}

type optionsChainResult struct {
	BreakEvenPrice    float64                 `json:"break_even_price"`
	Day               *optionsChainDay        `json:"day"`
	Details           *optionsChainDetails    `json:"details"`
	Greeks            *optionsChainGreeks     `json:"greeks"`
	ImpliedVolatility *float64                `json:"implied_volatility"`
	OpenInterest      float64                 `json:"open_interest"`
	LastQuote         *optionsChainQuote      `json:"last_quote"`
	LastTrade         *optionsChainTrade      `json:"last_trade"`
	UnderlyingAsset   *optionsChainUnderlying `json:"underlying_asset"`
}

type optionsChainDay struct {
	Open          float64 `json:"open"`
	High          float64 `json:"high"`
	Low           float64 `json:"low"`
	Close         float64 `json:"close"`
	Volume        float64 `json:"volume"`
	VWAP          float64 `json:"vwap"`
	PreviousClose float64 `json:"previous_close"`
}

type optionsChainDetails struct {
	Ticker            string  `json:"ticker"`          // O:AAPL241220C00150000
	ContractType      string  `json:"contract_type"`   // put, call
	ExerciseStyle     string  `json:"exercise_style"`  // american, european
	ExpirationDate    string  `json:"expiration_date"` // YYYY-MM-DD
	StrikePrice       float64 `json:"strike_price"`
	SharesPerContract float64 `json:"shares_per_contract"`
}

type optionsChainGreeks struct {
	Delta float64 `json:"delta"`
	Gamma float64 `json:"gamma"`
	Theta float64 `json:"theta"`
	Vega  float64 `json:"vega"`
}

type optionsChainQuote struct {
	Ask      float64 `json:"ask"`
	AskSize  float64 `json:"ask_size"`
	Bid      float64 `json:"bid"`
	BidSize  float64 `json:"bid_size"`
	Midpoint float64 `json:"midpoint"`
}

type optionsChainTrade struct {
	Price float64 `json:"price"`
	Size  float64 `json:"size"`
}

type optionsChainUnderlying struct {
	Ticker string  `json:"ticker"`
	Price  float64 `json:"price"`
}

func mapChainResult(r optionsChainResult) domain.OptionSnapshot {
	snap := domain.OptionSnapshot{
		OpenInterest: r.OpenInterest,
	}

	if r.Details != nil {
		occRaw := domain.AlpacaSymbol(r.Details.Ticker)
		var optType domain.OptionType
		switch strings.ToLower(r.Details.ContractType) {
		case "call":
			optType = domain.OptionTypeCall
		case "put":
			optType = domain.OptionTypePut
		}

		expiry, _ := time.Parse("2006-01-02", r.Details.ExpirationDate)
		multiplier := r.Details.SharesPerContract
		if multiplier == 0 {
			multiplier = 100
		}

		snap.Contract = domain.OptionContract{
			OCCSymbol:  occRaw,
			Underlying: strings.TrimSpace(r.Details.Ticker[:max(0, strings.IndexAny(r.Details.Ticker, "0123456789"))]),
			OptionType: optType,
			Strike:     r.Details.StrikePrice,
			Expiry:     expiry,
			Multiplier: multiplier,
			Style:      r.Details.ExerciseStyle,
		}

		// Use parsed OCC for a cleaner underlying extraction
		if parsed, err := domain.ParseOCC(occRaw); err == nil {
			snap.Contract.Underlying = parsed.Underlying
			snap.Contract.OCCSymbol = parsed.OCCSymbol
		}
	}

	if r.Greeks != nil {
		snap.Greeks = domain.OptionGreeks{
			Delta: r.Greeks.Delta,
			Gamma: r.Greeks.Gamma,
			Theta: r.Greeks.Theta,
			Vega:  r.Greeks.Vega,
		}
	}
	if r.ImpliedVolatility != nil {
		snap.Greeks.IV = *r.ImpliedVolatility
	}

	if r.LastQuote != nil {
		snap.Bid = r.LastQuote.Bid
		snap.Ask = r.LastQuote.Ask
		snap.Mid = r.LastQuote.Midpoint
		if snap.Mid == 0 && snap.Bid > 0 && snap.Ask > 0 {
			snap.Mid = (snap.Bid + snap.Ask) / 2
		}
	}

	if r.LastTrade != nil {
		snap.Last = r.LastTrade.Price
	}

	if r.Day != nil {
		snap.Volume = r.Day.Volume
	}

	return snap
}
