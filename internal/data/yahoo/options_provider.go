package yahoo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	gpo "github.com/jasonmerecki/gopriceoptions"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const (
	crumbURL   = "https://query2.finance.yahoo.com/v1/test/getcrumb"
	cookieURL  = "https://fc.yahoo.com/"
	optionsURL = "https://query2.finance.yahoo.com"
)

// OptionsProvider retrieves options chain data from Yahoo Finance.
// Yahoo provides IV, bid/ask, volume, and open interest for free.
// Greeks (delta, gamma, theta, vega) are computed via gopriceoptions (Black-Scholes).
type OptionsProvider struct {
	client *http.Client
	logger *slog.Logger

	mu          sync.Mutex
	crumb       string
	crumbAt     time.Time
	rateLimited time.Time // backoff until this time on 429
}

var _ data.OptionsDataProvider = (*OptionsProvider)(nil)

// NewOptionsProvider constructs a Yahoo Finance options data provider.
func NewOptionsProvider(logger *slog.Logger) *OptionsProvider {
	if logger == nil {
		logger = slog.Default()
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: defaultTimeout,
	}

	return &OptionsProvider{client: client, logger: logger}
}

// ensureCrumb obtains a Yahoo crumb + session cookie, caching for 1 hour.
func (p *OptionsProvider) ensureCrumb(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.crumb != "" && time.Since(p.crumbAt) < time.Hour {
		return p.crumb, nil
	}

	// Respect rate limit backoff.
	if !p.rateLimited.IsZero() && time.Now().Before(p.rateLimited) {
		return "", fmt.Errorf("yahoo/options: rate limited, retry after %s", time.Until(p.rateLimited).Truncate(time.Second))
	}

	// Step 1: hit fc.yahoo.com to get session cookies.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cookieURL, nil)
	if err != nil {
		return "", fmt.Errorf("yahoo/options: build cookie request: %w", err)
	}
	req.Header.Set("User-Agent", defaultUA)
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("yahoo/options: cookie request: %w", err)
	}
	resp.Body.Close()

	// Step 2: fetch crumb using the session cookies.
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, crumbURL, nil)
	if err != nil {
		return "", fmt.Errorf("yahoo/options: build crumb request: %w", err)
	}
	req.Header.Set("User-Agent", defaultUA)
	resp, err = p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("yahoo/options: crumb request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		p.rateLimited = time.Now().Add(2 * time.Minute)
		return "", fmt.Errorf("yahoo/options: rate limited, backing off 2m")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("yahoo/options: crumb request status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("yahoo/options: read crumb: %w", err)
	}

	crumb := strings.TrimSpace(string(body))
	if crumb == "" {
		return "", errors.New("yahoo/options: empty crumb returned")
	}

	p.crumb = crumb
	p.crumbAt = time.Now()
	p.logger.Info("yahoo/options: crumb obtained")
	return crumb, nil
}

// invalidateCrumb forces a fresh crumb on next request.
func (p *OptionsProvider) invalidateCrumb() {
	p.mu.Lock()
	p.crumb = ""
	p.crumbAt = time.Time{}
	// Reset cookie jar too.
	jar, _ := cookiejar.New(nil)
	p.client.Jar = jar
	p.mu.Unlock()
}

// fetchOptions makes an authenticated GET to the Yahoo options endpoint.
func (p *OptionsProvider) fetchOptions(ctx context.Context, path string, params url.Values) ([]byte, error) {
	crumb, err := p.ensureCrumb(ctx)
	if err != nil {
		return nil, err
	}
	params.Set("crumb", crumb)

	reqURL := optionsURL + path + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("yahoo/options: build request: %w", err)
	}
	req.Header.Set("User-Agent", defaultUA)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yahoo/options: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("yahoo/options: read body: %w", err)
	}

	// Rate limited — don't invalidate crumb, just backoff.
	if resp.StatusCode == http.StatusTooManyRequests {
		p.mu.Lock()
		p.rateLimited = time.Now().Add(2 * time.Minute)
		p.mu.Unlock()
		return nil, fmt.Errorf("yahoo/options: rate limited (429), backing off 2m")
	}

	// If unauthorized, invalidate crumb and retry once.
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		p.invalidateCrumb()
		crumb, err = p.ensureCrumb(ctx)
		if err != nil {
			return nil, err
		}
		params.Set("crumb", crumb)
		reqURL = optionsURL + path + "?" + params.Encode()
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", defaultUA)
		req.Header.Set("Accept", "application/json")

		resp, err = p.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("yahoo/options: retry request failed: %w", err)
		}
		defer resp.Body.Close()
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("yahoo/options: read retry body: %w", err)
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yahoo/options: %s (status=%d)", strings.TrimSpace(string(body)), resp.StatusCode)
	}

	return body, nil
}

// GetOptionsChain returns option snapshots for an underlying ticker.
// If expiry is zero, the nearest expiration is returned. If optionType is
// empty, both calls and puts are included.
func (p *OptionsProvider) GetOptionsChain(
	ctx context.Context,
	underlying string,
	expiry time.Time,
	optionType domain.OptionType,
) ([]domain.OptionSnapshot, error) {
	if p == nil {
		return nil, errors.New("yahoo/options: provider is nil")
	}
	underlying = strings.TrimSpace(strings.ToUpper(underlying))
	if underlying == "" {
		return nil, errors.New("yahoo/options: underlying ticker is required")
	}

	chainPath := "/v7/finance/options/" + url.PathEscape(underlying)
	params := url.Values{}

	if !expiry.IsZero() {
		params.Set("date", fmt.Sprintf("%d", expiry.Unix()))
	}

	body, err := p.fetchOptions(ctx, chainPath, params)
	if err != nil {
		return nil, fmt.Errorf("yahoo/options: chain request failed: %w", err)
	}

	var resp yahooOptionsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("yahoo/options: decode response: %w", err)
	}
	if resp.OptionChain.Error != nil {
		return nil, fmt.Errorf("yahoo/options: %s", resp.OptionChain.Error.Description)
	}
	if len(resp.OptionChain.Result) == 0 {
		return nil, nil
	}

	result := resp.OptionChain.Result[0]

	// If caller asked for a specific expiry but didn't get one, scan all
	// available expiries that are close to the target (within 7 days).
	if !expiry.IsZero() && len(result.Options) == 0 {
		for _, epochDate := range result.ExpirationDates {
			expDate := time.Unix(epochDate, 0).UTC()
			if math.Abs(expDate.Sub(expiry).Hours()) > 7*24 {
				continue
			}
			params.Set("date", fmt.Sprintf("%d", epochDate))
			body, err = p.fetchOptions(ctx, chainPath, params)
			if err != nil {
				continue
			}
			var retry yahooOptionsResponse
			if json.Unmarshal(body, &retry) == nil && len(retry.OptionChain.Result) > 0 {
				r := retry.OptionChain.Result[0]
				if len(r.Options) > 0 {
					result = r
					break
				}
			}
		}
	}

	// Extract underlying price for Greeks computation.
	underlyingPrice := result.Quote.RegularMarketPrice

	var snapshots []domain.OptionSnapshot
	for _, opt := range result.Options {
		if optionType == "" || optionType == domain.OptionTypeCall {
			for _, c := range opt.Calls {
				snap := mapYahooContract(c, underlying, domain.OptionTypeCall, underlyingPrice)
				snapshots = append(snapshots, snap)
			}
		}
		if optionType == "" || optionType == domain.OptionTypePut {
			for _, c := range opt.Puts {
				snap := mapYahooContract(c, underlying, domain.OptionTypePut, underlyingPrice)
				snapshots = append(snapshots, snap)
			}
		}
	}

	return snapshots, nil
}

// GetOptionsOHLCV is not supported by Yahoo Finance.
func (p *OptionsProvider) GetOptionsOHLCV(
	_ context.Context, _ string, _ data.Timeframe, _, _ time.Time,
) ([]domain.OHLCV, error) {
	return nil, fmt.Errorf("yahoo/options: GetOptionsOHLCV: %w", data.ErrNotImplemented)
}

// mapYahooContract converts a Yahoo option contract to the domain snapshot.
func mapYahooContract(c yahooOptionContract, underlying string, optType domain.OptionType, underlyingPrice float64) domain.OptionSnapshot {
	expiry := time.Unix(c.Expiration, 0).UTC()
	multiplier := 100.0

	mid := c.LastPrice
	if c.Bid > 0 && c.Ask > 0 {
		mid = (c.Bid + c.Ask) / 2
	}

	snap := domain.OptionSnapshot{
		Contract: domain.OptionContract{
			OCCSymbol:  c.ContractSymbol,
			Underlying: underlying,
			OptionType: optType,
			Strike:     c.Strike,
			Expiry:     expiry,
			Multiplier: multiplier,
			Style:      "american",
		},
		Greeks: domain.OptionGreeks{
			IV: c.ImpliedVolatility,
		},
		Bid:          c.Bid,
		Ask:          c.Ask,
		Mid:          mid,
		Last:         c.LastPrice,
		Volume:       c.Volume,
		OpenInterest: c.OpenInterest,
	}

	// Compute Greeks via Black-Scholes if we have enough data.
	if underlyingPrice > 0 && c.Strike > 0 && c.ImpliedVolatility > 0 {
		t := time.Until(expiry).Hours() / (24 * 365.25)
		if t > 0 {
			snap.Greeks = computeGreeks(underlyingPrice, c.Strike, t, c.ImpliedVolatility, optType)
		}
	}

	return snap
}

// Yahoo Finance options response types.

type yahooOptionsResponse struct {
	OptionChain yahooOptionChain `json:"optionChain"`
}

type yahooOptionChain struct {
	Result []yahooOptionResult `json:"result"`
	Error  *yahooError         `json:"error"`
}

type yahooError struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

type yahooOptionResult struct {
	UnderlyingSymbol string              `json:"underlyingSymbol"`
	ExpirationDates  []int64             `json:"expirationDates"`
	Strikes          []float64           `json:"strikes"`
	Quote            yahooQuote          `json:"quote"`
	Options          []yahooOptionExpiry `json:"options"`
}

type yahooQuote struct {
	RegularMarketPrice float64 `json:"regularMarketPrice"`
}

type yahooOptionExpiry struct {
	ExpirationDate int64                 `json:"expirationDate"`
	Calls          []yahooOptionContract `json:"calls"`
	Puts           []yahooOptionContract `json:"puts"`
}

type yahooOptionContract struct {
	ContractSymbol    string  `json:"contractSymbol"`
	Strike            float64 `json:"strike"`
	Currency          string  `json:"currency"`
	LastPrice         float64 `json:"lastPrice"`
	Change            float64 `json:"change"`
	PercentChange     float64 `json:"percentChange"`
	Volume            float64 `json:"volume"`
	OpenInterest      float64 `json:"openInterest"`
	Bid               float64 `json:"bid"`
	Ask               float64 `json:"ask"`
	ImpliedVolatility float64 `json:"impliedVolatility"`
	InTheMoney        bool    `json:"inTheMoney"`
	Expiration        int64   `json:"expiration"`
	ContractSize      string  `json:"contractSize"`
}

// Greeks computation via gopriceoptions (Black-Scholes).

const riskFreeRate = 0.05 // approximate; good enough for scanning
const dividend = 0.0

// computeGreeks calculates option Greeks using the gopriceoptions package.
// s=underlying price, k=strike, t=time to expiry in years, sigma=annualised IV.
func computeGreeks(s, k, t, sigma float64, optType domain.OptionType) domain.OptionGreeks {
	isCall := optType == domain.OptionTypeCall

	return domain.OptionGreeks{
		Delta: gpo.BSDelta(isCall, s, k, t, sigma, riskFreeRate, dividend),
		Gamma: gpo.BSGamma(s, k, t, sigma, riskFreeRate, dividend),
		Theta: gpo.BSTheta(isCall, s, k, t, sigma, riskFreeRate, dividend),
		Vega:  gpo.BSVega(s, k, t, sigma, riskFreeRate, dividend),
		Rho:   gpo.BSRho(isCall, s, k, t, sigma, riskFreeRate, dividend),
		IV:    sigma,
	}
}
