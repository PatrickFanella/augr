package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
)

// GetMarketData fetches complete market metadata and book state for the given
// Polymarket market slug and maps it to an agent.PredictionMarketData.
func (c *Client) GetMarketData(ctx context.Context, slug string) (*agent.PredictionMarketData, error) {
	market, err := c.fetchMarketBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}

	book, err := c.fetchMarketBook(ctx, slug)
	if err != nil {
		// Non-fatal: return market metadata without book data.
		c.getLogger().Warn("polymarket: failed to fetch market book", "slug", slug, "error", err)
	}

	bbo, err := c.fetchMarketBBO(ctx, slug)
	if err != nil {
		c.getLogger().Warn("polymarket: failed to fetch market bbo", "slug", slug, "error", err)
	}

	var endDate *time.Time
	if market.EndDate != "" {
		if parsed, err := time.Parse(time.RFC3339, market.EndDate); err == nil {
			parsed = parsed.UTC()
			endDate = &parsed
		}
	}

	yesPrice := market.sidePrice(true)
	noPrice := market.sidePrice(false)
	if bbo.LastPriceSample.LongPx.Value != "" {
		yesPrice = parseDecimalOrZero(bbo.LastPriceSample.LongPx.Value)
	}
	if bbo.LastPriceSample.ShortPx.Value != "" {
		noPrice = parseDecimalOrZero(bbo.LastPriceSample.ShortPx.Value)
	}
	if yesPrice == 0 && bbo.CurrentPx.Value != "" {
		yesPrice = parseDecimalOrZero(bbo.CurrentPx.Value)
		if noPrice == 0 && yesPrice > 0 && yesPrice < 1 {
			noPrice = 1 - yesPrice
		}
	}

	bestBidYes := parseDecimalOrZero(bbo.BestBid.Value)
	bestAskYes := parseDecimalOrZero(bbo.BestAsk.Value)
	if len(book.Bids) > 0 {
		bestBidYes = parseDecimalOrZero(book.Bids[0].Px.Value)
	}
	if len(book.Offers) > 0 {
		bestAskYes = parseDecimalOrZero(book.Offers[0].Px.Value)
	}

	pmd := &agent.PredictionMarketData{
		Slug:               firstNonEmpty(market.Slug, slug),
		Question:           firstNonEmpty(market.Question, market.Title),
		Description:        market.Description,
		ResolutionCriteria: market.Subtitle,
		EndDate:            endDate,
		ResolutionSource:   market.EP3Status,
		YesPrice:           yesPrice,
		NoPrice:            noPrice,
		Volume24h:          parseDecimalOrZero(bbo.SharesTraded),
		Liquidity:          float64(bbo.BidDepth + bbo.AskDepth),
		OpenInterest:       parseDecimalOrZero(bbo.OpenInterest),
		BestBidYes:         bestBidYes,
		BestAskYes:         bestAskYes,
		SpreadYes:          max0(bestAskYes - bestBidYes),
	}

	// Current retail gateway responses are slug-centric; legacy token IDs are no
	// longer required for phase-1 live trading alignment.
	return pmd, nil
}

type marketBySlugResponse struct {
	Market gatewayMarket `json:"market"`
}

type gatewayMarket struct {
	ID            string `json:"id"`
	Question      string `json:"question"`
	Slug          string `json:"slug"`
	EndDate       string `json:"endDate"`
	Description   string `json:"description"`
	Subtitle      string `json:"subtitle"`
	EP3Status     string `json:"ep3Status"`
	Title         string `json:"title"`
	OutcomePrices string `json:"outcomePrices"`
	MarketSides   []struct {
		Description string `json:"description"`
		Price       string `json:"price"`
		Long        *bool  `json:"long"`
	} `json:"marketSides"`
}

type marketBookResponse struct {
	MarketData marketBookData `json:"marketData"`
}

type marketBookData struct {
	Bids []struct {
		Px amount `json:"px"`
	} `json:"bids"`
	Offers []struct {
		Px amount `json:"px"`
	} `json:"offers"`
}

type marketBBOResponse struct {
	MarketData marketBBOData `json:"marketData"`
}

type marketBBOData struct {
	CurrentPx       amount `json:"currentPx"`
	LastTradePx     amount `json:"lastTradePx"`
	SettlementPx    amount `json:"settlementPx"`
	SharesTraded    string `json:"sharesTraded"`
	OpenInterest    string `json:"openInterest"`
	BestAsk         amount `json:"bestAsk"`
	BestBid         amount `json:"bestBid"`
	AskDepth        int    `json:"askDepth"`
	BidDepth        int    `json:"bidDepth"`
	LastPriceSample struct {
		LongPx  amount `json:"longPx"`
		ShortPx amount `json:"shortPx"`
	} `json:"lastPriceSample"`
}

func (c *Client) fetchMarketBySlug(ctx context.Context, slug string) (gatewayMarket, error) {
	body, err := c.GetPublic(ctx, "/v1/market/slug/"+slug, nil)
	if err != nil {
		return gatewayMarket{}, fmt.Errorf("polymarket: fetch market %q: %w", slug, err)
	}

	var resp marketBySlugResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return gatewayMarket{}, fmt.Errorf("polymarket: parse market by slug: %w", err)
	}
	if firstNonEmpty(resp.Market.Slug, resp.Market.Question, resp.Market.Title) == "" {
		return gatewayMarket{}, fmt.Errorf("polymarket: market not found: %q", slug)
	}

	return resp.Market, nil
}

func (c *Client) fetchMarketBook(ctx context.Context, slug string) (marketBookData, error) {
	body, err := c.GetPublic(ctx, "/v1/markets/"+slug+"/book", nil)
	if err != nil {
		return marketBookData{}, err
	}

	var resp marketBookResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return marketBookData{}, fmt.Errorf("polymarket: parse market book: %w", err)
	}

	return resp.MarketData, nil
}

func (c *Client) fetchMarketBBO(ctx context.Context, slug string) (marketBBOData, error) {
	body, err := c.GetPublic(ctx, "/v1/markets/"+slug+"/bbo", nil)
	if err != nil {
		return marketBBOData{}, err
	}

	var resp marketBBOResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return marketBBOData{}, fmt.Errorf("polymarket: parse market bbo: %w", err)
	}

	return resp.MarketData, nil
}

func (m gatewayMarket) sidePrice(long bool) float64 {
	if len(m.MarketSides) > 0 {
		for _, side := range m.MarketSides {
			if side.Long != nil && *side.Long == long {
				return parseDecimalOrZero(side.Price)
			}
		}
	}

	trimmed := strings.TrimSpace(m.OutcomePrices)
	if trimmed == "" {
		return 0
	}
	var raw []string
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return 0
	}
	if len(raw) < 2 {
		return 0
	}
	if long {
		return parseDecimalOrZero(raw[0])
	}
	return parseDecimalOrZero(raw[1])
}

func parseDecimalOrZero(value string) float64 {
	parsed, err := strconvParse(value)
	if err != nil {
		return 0
	}
	return parsed
}

func strconvParse(value string) (float64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("empty decimal")
	}
	return strconv.ParseFloat(trimmed, 64)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func max0(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}
