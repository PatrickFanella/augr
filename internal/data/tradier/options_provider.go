package tradier

import (
	"context"
	"encoding/json"
	"errors"
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
	productionBaseURL = "https://api.tradier.com"
	sandboxBaseURL    = "https://sandbox.tradier.com"
	defaultTimeout    = 30 * time.Second
)

// OptionsProvider retrieves options chain data from Tradier.
// Provides full Greeks, IV, bid/ask, volume, OI from ORATS data.
type OptionsProvider struct {
	baseURL string
	token   string
	client  *http.Client
	logger  *slog.Logger
}

var _ data.OptionsDataProvider = (*OptionsProvider)(nil)

// NewOptionsProvider constructs a Tradier options data provider.
// If sandbox is true, uses the sandbox endpoint (delayed data, 60 req/min).
func NewOptionsProvider(token string, sandbox bool, logger *slog.Logger) *OptionsProvider {
	if logger == nil {
		logger = slog.Default()
	}
	base := productionBaseURL
	if sandbox {
		base = sandboxBaseURL
	}
	return &OptionsProvider{
		baseURL: base,
		token:   token,
		client:  &http.Client{Timeout: defaultTimeout},
		logger:  logger,
	}
}

// GetOptionsChain returns option snapshots for an underlying ticker.
// If expiry is zero, the nearest expiration is fetched first.
// If optionType is empty, both calls and puts are included.
func (p *OptionsProvider) GetOptionsChain(
	ctx context.Context,
	underlying string,
	expiry time.Time,
	optionType domain.OptionType,
) ([]domain.OptionSnapshot, error) {
	if p == nil {
		return nil, errors.New("tradier/options: provider is nil")
	}
	underlying = strings.TrimSpace(strings.ToUpper(underlying))
	if underlying == "" {
		return nil, errors.New("tradier/options: underlying ticker is required")
	}

	// If no expiry given, fetch the nearest one.
	if expiry.IsZero() {
		exp, err := p.nearestExpiry(ctx, underlying)
		if err != nil {
			return nil, fmt.Errorf("tradier/options: fetch expirations: %w", err)
		}
		expiry = exp
	}

	params := url.Values{
		"symbol":     {underlying},
		"expiration": {expiry.Format("2006-01-02")},
		"greeks":     {"true"},
	}

	body, err := p.get(ctx, "/v1/markets/options/chains", params)
	if err != nil {
		return nil, fmt.Errorf("tradier/options: chain request failed: %w", err)
	}

	var resp tradierChainResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("tradier/options: decode response: %w", err)
	}

	if resp.Options == nil {
		return nil, nil
	}

	var snapshots []domain.OptionSnapshot
	for _, c := range resp.Options.Option {
		optType := domain.OptionType(c.OptionType)
		if optionType != "" && optType != optionType {
			continue
		}
		snapshots = append(snapshots, mapTradierContract(c, underlying))
	}

	return snapshots, nil
}

// GetOptionsOHLCV is not supported by Tradier free tier.
func (p *OptionsProvider) GetOptionsOHLCV(
	_ context.Context, _ string, _ data.Timeframe, _, _ time.Time,
) ([]domain.OHLCV, error) {
	return nil, fmt.Errorf("tradier/options: GetOptionsOHLCV: %w", data.ErrNotImplemented)
}

// nearestExpiry fetches available expiration dates and returns the nearest one.
func (p *OptionsProvider) nearestExpiry(ctx context.Context, underlying string) (time.Time, error) {
	params := url.Values{"symbol": {underlying}}
	body, err := p.get(ctx, "/v1/markets/options/expirations", params)
	if err != nil {
		return time.Time{}, err
	}

	var resp tradierExpirationsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return time.Time{}, fmt.Errorf("tradier/options: decode expirations: %w", err)
	}

	if resp.Expirations == nil || len(resp.Expirations.Date) == 0 {
		return time.Time{}, fmt.Errorf("tradier/options: no expirations for %s", underlying)
	}

	// Find the nearest expiry at least 7 days out.
	now := time.Now()
	minExpiry := now.AddDate(0, 0, 7)
	for _, dateStr := range resp.Expirations.Date {
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if t.After(minExpiry) {
			return t, nil
		}
	}

	// Fallback: return the last available.
	last := resp.Expirations.Date[len(resp.Expirations.Date)-1]
	t, _ := time.Parse("2006-01-02", last)
	return t, nil
}

// get performs an authenticated GET request.
func (p *OptionsProvider) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	reqURL := p.baseURL + path + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tradier/options: read body: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("tradier/options: rate limited (429)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tradier/options: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return body, nil
}

func mapTradierContract(c tradierOption, underlying string) domain.OptionSnapshot {
	optType := domain.OptionType(c.OptionType)
	expiry, _ := time.Parse("2006-01-02", c.ExpirationDate)

	multiplier := float64(c.ContractSize)
	if multiplier == 0 {
		multiplier = 100
	}

	mid := c.Last
	if c.Bid > 0 && c.Ask > 0 {
		mid = (c.Bid + c.Ask) / 2
	}

	snap := domain.OptionSnapshot{
		Contract: domain.OptionContract{
			OCCSymbol:  c.Symbol,
			Underlying: underlying,
			OptionType: optType,
			Strike:     c.Strike,
			Expiry:     expiry,
			Multiplier: multiplier,
			Style:      "american",
		},
		Bid:          c.Bid,
		Ask:          c.Ask,
		Mid:          mid,
		Last:         c.Last,
		Volume:       float64(c.Volume),
		OpenInterest: float64(c.OpenInterest),
	}

	if c.Greeks != nil {
		snap.Greeks = domain.OptionGreeks{
			Delta: c.Greeks.Delta,
			Gamma: c.Greeks.Gamma,
			Theta: c.Greeks.Theta,
			Vega:  c.Greeks.Vega,
			Rho:   c.Greeks.Rho,
			IV:    c.Greeks.MidIV,
		}
	}

	return snap
}

// Tradier response types.

type tradierChainResponse struct {
	Options *tradierOptions `json:"options"`
}

type tradierOptions struct {
	Option []tradierOption `json:"option"`
}

type tradierOption struct {
	Symbol         string         `json:"symbol"`
	Description    string         `json:"description"`
	Strike         float64        `json:"strike"`
	Bid            float64        `json:"bid"`
	Ask            float64        `json:"ask"`
	Last           float64        `json:"last"`
	Volume         int            `json:"volume"`
	OpenInterest   int            `json:"open_interest"`
	ContractSize   int            `json:"contract_size"`
	OptionType     string         `json:"option_type"` // "call" or "put"
	ExpirationDate string         `json:"expiration_date"`
	RootSymbol     string         `json:"root_symbol"`
	Greeks         *tradierGreeks `json:"greeks,omitempty"`
}

type tradierGreeks struct {
	Delta   float64 `json:"delta"`
	Gamma   float64 `json:"gamma"`
	Theta   float64 `json:"theta"`
	Vega    float64 `json:"vega"`
	Rho     float64 `json:"rho"`
	BidIV   float64 `json:"bid_iv"`
	MidIV   float64 `json:"mid_iv"`
	AskIV   float64 `json:"ask_iv"`
	SmvVol  float64 `json:"smv_vol"`
}

type tradierExpirationsResponse struct {
	Expirations *tradierExpirations `json:"expirations"`
}

type tradierExpirations struct {
	Date []string `json:"date"`
}
