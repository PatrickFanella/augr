package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultGammaBaseURL = "https://gamma-api.polymarket.com"

// GammaClient reads public market metadata from Polymarket Gamma.
type GammaClient interface {
	GetMarket(ctx context.Context, slug string) (GammaMarket, error)
}

// GammaMarket is the normalized subset of Gamma market metadata used by the app.
type GammaMarket struct {
	Slug             string
	ConditionID      string
	YesTokenID       string
	NoTokenID        string
	YesOutcome       string
	NoOutcome        string
	Outcomes         []string
	OutcomePrices    []float64
	EnableOrderBook  bool
	NegRisk          bool
	MinimumTickSize  float64
	MinimumOrderSize float64
}

// GammaHTTPClient implements GammaClient over HTTP.
type GammaHTTPClient struct {
	baseURL    string
	httpClient *http.Client
}

var _ GammaClient = (*GammaHTTPClient)(nil)

// NewGammaClient constructs an HTTP Gamma client with official defaults.
func NewGammaClient(baseURL string, httpClient *http.Client) *GammaHTTPClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultGammaBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &GammaHTTPClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: httpClient,
	}
}

// GetMarket fetches a market by slug and normalizes YES/NO token and outcome fields.
func (c *GammaHTTPClient) GetMarket(ctx context.Context, slug string) (GammaMarket, error) {
	if c == nil {
		return GammaMarket{}, fmt.Errorf("polymarket: gamma client is nil")
	}
	slug = normalizeSlug(slug)
	if slug == "" {
		return GammaMarket{}, fmt.Errorf("polymarket: slug is required")
	}

	baseURL := c.baseURL
	if baseURL == "" {
		baseURL = defaultGammaBaseURL
	}
	requestURL, err := url.Parse(baseURL + "/markets")
	if err != nil {
		return GammaMarket{}, err
	}
	q := requestURL.Query()
	q.Set("slug", slug)
	requestURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return GammaMarket{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return GammaMarket{}, fmt.Errorf("polymarket: gamma get market: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return GammaMarket{}, fmt.Errorf("polymarket: gamma markets HTTP %d", resp.StatusCode)
	}

	markets, err := decodeGammaMarkets(resp.Body)
	if err != nil {
		return GammaMarket{}, err
	}
	if len(markets) == 0 {
		return GammaMarket{}, fmt.Errorf("polymarket: gamma market not found: %q", slug)
	}

	market := normalizeGammaMarket(markets[0])
	if market.Slug == "" {
		market.Slug = slug
	}
	return market, nil
}

type rawGammaMarket struct {
	Slug                  string          `json:"slug"`
	ConditionID           string          `json:"conditionId"`
	ConditionIDSnake      string          `json:"condition_id"`
	ClobTokenIDs          flexibleStrings `json:"clobTokenIds"`
	Outcomes              flexibleStrings `json:"outcomes"`
	OutcomePrices         flexibleFloats  `json:"outcomePrices"`
	EnableOrderBook       bool            `json:"enableOrderBook"`
	EnableOrderBookSnake  bool            `json:"enable_order_book"`
	NegRisk               bool            `json:"negRisk"`
	NegRiskSnake          bool            `json:"neg_risk"`
	MinimumTickSize       flexibleFloat   `json:"minimum_tick_size"`
	MinimumTickSizeCamel  flexibleFloat   `json:"minimumTickSize"`
	MTS                   flexibleFloat   `json:"mts"`
	MinimumOrderSize      flexibleFloat   `json:"minimum_order_size"`
	MinimumOrderSizeCamel flexibleFloat   `json:"minimumOrderSize"`
	MinOrderSize          flexibleFloat   `json:"min_order_size"`
	AssetID               string          `json:"asset_id"`
	TokenID               string          `json:"token_id"`
	SlugAlias             string          `json:"market_slug"`
	Outcome               string          `json:"outcome"`
	YesOutcome            string          `json:"yes_outcome"`
	NoOutcome             string          `json:"no_outcome"`
	YesTokenID            string          `json:"yes_token_id"`
	NoTokenID             string          `json:"no_token_id"`
}

func decodeGammaMarkets(body io.Reader) ([]rawGammaMarket, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("polymarket: read gamma markets: %w", err)
	}
	var markets []rawGammaMarket
	if err := json.Unmarshal(raw, &markets); err == nil {
		return markets, nil
	}
	var page struct {
		Data []rawGammaMarket `json:"data"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, fmt.Errorf("polymarket: decode gamma markets: %w", err)
	}
	return page.Data, nil
}

func normalizeGammaMarket(raw rawGammaMarket) GammaMarket {
	market := GammaMarket{
		Slug:             normalizeSlug(firstNonEmpty(raw.Slug, raw.SlugAlias)),
		ConditionID:      strings.TrimSpace(firstNonEmpty(raw.ConditionID, raw.ConditionIDSnake, raw.AssetID, raw.TokenID)),
		EnableOrderBook:  raw.EnableOrderBook || raw.EnableOrderBookSnake,
		NegRisk:          raw.NegRisk || raw.NegRiskSnake,
		MinimumTickSize:  maxFlexible(raw.MinimumTickSize, raw.MinimumTickSizeCamel, raw.MTS),
		MinimumOrderSize: maxFlexible(raw.MinimumOrderSize, raw.MinimumOrderSizeCamel, raw.MinOrderSize),
	}
	if len(raw.Outcomes) > 0 {
		market.Outcomes = normalizeOutcomes(raw.Outcomes)
		if len(market.Outcomes) > 0 {
			market.YesOutcome = market.Outcomes[0]
		}
		if len(market.Outcomes) > 1 {
			market.NoOutcome = market.Outcomes[1]
		}
	}
	if len(raw.OutcomePrices) > 0 {
		market.OutcomePrices = make([]float64, 0, len(raw.OutcomePrices))
		for _, price := range raw.OutcomePrices {
			market.OutcomePrices = append(market.OutcomePrices, float64(price))
		}
	}
	if len(raw.ClobTokenIDs) > 0 {
		for i, tokenID := range raw.ClobTokenIDs {
			trimmedToken := strings.TrimSpace(tokenID)
			if trimmedToken == "" {
				continue
			}
			outcome := ""
			if i < len(market.Outcomes) {
				outcome = market.Outcomes[i]
			}
			switch normalizeOutcome(outcome) {
			case "YES":
				market.YesTokenID = trimmedToken
				market.YesOutcome = "YES"
			case "NO":
				market.NoTokenID = trimmedToken
				market.NoOutcome = "NO"
			}
		}
		if market.YesTokenID == "" {
			market.YesTokenID = strings.TrimSpace(raw.ClobTokenIDs[0])
		}
		if market.NoTokenID == "" && len(raw.ClobTokenIDs) > 1 {
			market.NoTokenID = strings.TrimSpace(raw.ClobTokenIDs[1])
		}
	}
	if market.YesTokenID == "" {
		market.YesTokenID = strings.TrimSpace(firstNonEmpty(raw.YesTokenID, raw.TokenID))
	}
	if market.NoTokenID == "" {
		market.NoTokenID = strings.TrimSpace(raw.NoTokenID)
	}
	if market.YesOutcome == "" {
		market.YesOutcome = normalizeOutcome(firstNonEmpty(raw.YesOutcome, "YES"))
	}
	if market.NoOutcome == "" {
		market.NoOutcome = normalizeOutcome(firstNonEmpty(raw.NoOutcome, "NO"))
	}
	if market.ConditionID == "" {
		market.ConditionID = strings.TrimSpace(firstNonEmpty(raw.ConditionID, raw.AssetID, raw.TokenID))
	}
	return market
}

type flexibleStrings []string

func (f *flexibleStrings) UnmarshalJSON(data []byte) error {
	values, err := decodeFlexibleStrings(data)
	if err != nil {
		return err
	}
	*f = values
	return nil
}

type flexibleFloats []flexibleFloat

func (f *flexibleFloats) UnmarshalJSON(data []byte) error {
	values, err := decodeFlexibleFloats(data)
	if err != nil {
		return err
	}
	*f = values
	return nil
}

func decodeFlexibleStrings(data []byte) ([]string, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "\"") {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil, nil
		}
		if !strings.HasPrefix(raw, "[") {
			return []string{raw}, nil
		}
		trimmed = raw
	}

	var values []string
	if err := json.Unmarshal([]byte(trimmed), &values); err != nil {
		return nil, fmt.Errorf("polymarket: decode gamma string array: %w", err)
	}
	return values, nil
}

func decodeFlexibleFloats(data []byte) ([]flexibleFloat, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "\"") {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil, nil
		}
		if !strings.HasPrefix(raw, "[") {
			value, ok, err := parseFlexibleFloat([]byte(strconvQuote(raw)))
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, nil
			}
			return []flexibleFloat{flexibleFloat(value)}, nil
		}
		trimmed = raw
	}

	var values []flexibleFloat
	if err := json.Unmarshal([]byte(trimmed), &values); err != nil {
		return nil, fmt.Errorf("polymarket: decode gamma numeric array: %w", err)
	}
	return values, nil
}

func strconvQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
