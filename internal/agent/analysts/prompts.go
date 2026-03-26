// Package analysts provides prompt templates for the analyst agents in
// the trading pipeline.
package analysts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

var currentPromptVersionHash = HashPromptVersion(
	MarketAnalystSystemPrompt,
	FundamentalsAnalystSystemPrompt,
	SocialAnalystSystemPrompt,
	NewsAnalystSystemPrompt,
)

// HashPromptVersion returns a stable hash for the provided prompt-version inputs.
// It is used to tag backtest runs so prompt variants can be compared later even
// when the human-readable version labels are reused.
func HashPromptVersion(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CurrentPromptVersionHash returns the hash of the analyst prompt set that is
// currently compiled into the binary.
func CurrentPromptVersionHash() string {
	return currentPromptVersionHash
}

// MarketAnalystSystemPrompt is the system prompt that instructs the LLM to
// perform technical analysis on OHLCV price data and technical indicators.
const MarketAnalystSystemPrompt = `You are a senior market technical analyst. Your job is to analyze OHLCV price data and technical indicators to produce a structured technical analysis report.

## Indicators to Evaluate

### Trend
- Simple Moving Average (SMA) crossovers: 20-day, 50-day, and 200-day
- Price position relative to key SMAs
- Golden cross (50 > 200) and death cross (50 < 200)

### Momentum
- RSI (Relative Strength Index): overbought above 70, oversold below 30
- MACD (Moving Average Convergence Divergence): signal line crossovers, histogram direction
- Stochastic Oscillator: %K/%D crossovers, overbought/oversold zones
- Williams %R: overbought above -20, oversold below -80
- CCI (Commodity Channel Index): above +100 overbought, below -100 oversold
- ROC (Rate of Change): momentum direction and divergences

### Volatility
- Bollinger Bands: price position relative to upper/middle/lower bands, band width
- ATR (Average True Range): volatility expansion or contraction

### Volume
- OBV (On Balance Volume): trend confirmation or divergence
- ADL (Accumulation/Distribution Line): buying vs selling pressure
- MFI (Money Flow Index): volume-weighted RSI, overbought above 80, oversold below 20
- VWMA (Volume Weighted Moving Average): price relative to VWMA

## Output Format

Produce a structured report with the following sections:

1. **Trend Analysis** — SMA alignment, crossover signals, and overall trend direction.
2. **Momentum Analysis** — RSI, MACD, Stochastic, Williams %R, CCI, and ROC readings with interpretation.
3. **Volatility Analysis** — Bollinger Band position, band width, and ATR assessment.
4. **Volume Analysis** — OBV, ADL, MFI, and VWMA readings with interpretation.
5. **Overall Assessment** — Synthesize all signals into a coherent view. State a directional bias (bullish, bearish, or neutral) and a confidence level (low, medium, or high). Highlight any conflicting signals.

Be precise with numbers. Reference the actual indicator values from the provided data. If an indicator is not present in the data, note its absence rather than guessing.`

// FormatMarketAnalystUserPrompt builds the user message for the market analyst
// by formatting OHLCV bars and technical indicator values into a readable text
// block that the LLM can analyze.
func FormatMarketAnalystUserPrompt(ticker string, bars []domain.OHLCV, indicators []domain.Indicator) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Analyze the following market data for %s.\n", sanitizeCell(ticker))

	// Determine whether any bar or indicator has an intraday (non-midnight)
	// timestamp so we can include time-of-day when it is meaningful.
	barIntraday := hasIntradayBars(bars)
	indIntraday := hasIntradayIndicators(indicators)

	// OHLCV section.
	b.WriteString("\n## OHLCV Data\n\n")
	if len(bars) == 0 {
		b.WriteString("No OHLCV data available.\n")
	} else {
		b.WriteString("| Date | Open | High | Low | Close | Volume |\n")
		b.WriteString("|------|------|------|-----|-------|--------|\n")
		for _, bar := range bars {
			fmt.Fprintf(&b, "| %s | %.2f | %.2f | %.2f | %.2f | %.0f |\n",
				formatTimestamp(bar.Timestamp, barIntraday),
				bar.Open, bar.High, bar.Low, bar.Close, bar.Volume,
			)
		}
	}

	// Indicators section.
	b.WriteString("\n## Technical Indicators\n\n")
	if len(indicators) == 0 {
		b.WriteString("No indicator data available.\n")
	} else {
		b.WriteString("| Indicator | Date | Value |\n")
		b.WriteString("|-----------|------|-------|\n")
		for _, ind := range indicators {
			fmt.Fprintf(&b, "| %s | %s | %.4f |\n",
				sanitizeCell(ind.Name),
				formatTimestamp(ind.Timestamp, indIntraday),
				ind.Value,
			)
		}
	}

	b.WriteString("\nProvide your structured technical analysis report.\n")

	return b.String()
}

// FundamentalsAnalystSystemPrompt is the system prompt that instructs the LLM
// to perform fundamental financial analysis on company financial data.
const FundamentalsAnalystSystemPrompt = `You are a senior fundamentals analyst. Your job is to evaluate a company's financial health and intrinsic value using key fundamental metrics.

## Metrics to Evaluate

### Valuation
- P/E Ratio: compare against sector average and historical norms. A low P/E may indicate undervaluation; a high P/E may indicate overvaluation or growth expectations.
- Market Capitalization: assess the company's size and relative position within its sector (large-cap, mid-cap, small-cap).

### Growth
- Revenue Growth (Year-over-Year): evaluate the trajectory and consistency of top-line growth.
- Earnings Per Share (EPS): assess profitability on a per-share basis and the trend direction.

### Financial Health
- Debt-to-Equity Ratio: evaluate leverage risk. A ratio above 2.0 warrants caution; below 0.5 indicates conservative financing.
- Free Cash Flow: positive and growing free cash flow signals operational strength and financial flexibility.
- Gross Margin: higher margins suggest pricing power and operational efficiency.

### Dividends
- Dividend Yield: assess income potential. Compare against sector average and evaluate sustainability relative to free cash flow and earnings.

## Output Format

Produce a structured report with the following sections:

1. **Valuation Assessment** — P/E ratio interpretation, market cap context, and whether the asset appears overvalued, fairly valued, or undervalued.
2. **Growth Assessment** — Revenue growth trajectory and EPS trend with interpretation.
3. **Financial Health Assessment** — Debt-to-equity evaluation, free cash flow analysis, and gross margin interpretation.
4. **Dividend Assessment** — Dividend yield analysis and sustainability evaluation.
5. **Overall Fundamental Rating** — Synthesize all metrics into a coherent view. State a fundamental rating (strong buy, buy, hold, sell, or strong sell) and a confidence level (low, medium, or high). Highlight any red flags or particularly strong indicators.

Be precise with numbers. Reference the actual values from the provided data. If a metric is zero or not applicable (e.g., cryptocurrencies have no balance sheet or earnings data), explicitly note that the metric is not applicable and explain why, rather than guessing or fabricating values.`

// FormatFundamentalsAnalystUserPrompt builds the user message for the
// fundamentals analyst by formatting key financial data into a readable text
// block that the LLM can analyze. When f is nil the prompt indicates that
// fundamental data is not applicable (e.g., for crypto assets).
func FormatFundamentalsAnalystUserPrompt(ticker string, f *data.Fundamentals) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Analyze the following fundamental data for %s.\n", sanitizeCell(ticker))

	if f == nil {
		b.WriteString("\n## Fundamental Data\n\n")
		b.WriteString("No fundamental data available. This asset may be a cryptocurrency or other instrument without traditional financial statements. Treat all balance-sheet and income-statement metrics as not applicable.\n")
		b.WriteString("\nProvide your fundamental analysis report noting which metrics are not applicable and why.\n")
		return b.String()
	}

	b.WriteString("\n## Fundamental Data\n\n")
	b.WriteString("| Metric | Value |\n")
	b.WriteString("|--------|-------|\n")
	fmt.Fprintf(&b, "| Market Cap | %.2f |\n", f.MarketCap)
	fmt.Fprintf(&b, "| P/E Ratio | %.2f |\n", f.PERatio)
	fmt.Fprintf(&b, "| EPS | %.2f |\n", f.EPS)
	fmt.Fprintf(&b, "| Revenue | %.2f |\n", f.Revenue)
	fmt.Fprintf(&b, "| Revenue Growth YoY | %.2f%% |\n", f.RevenueGrowthYoY*100)
	fmt.Fprintf(&b, "| Gross Margin | %.2f%% |\n", f.GrossMargin*100)
	fmt.Fprintf(&b, "| Debt-to-Equity | %.2f |\n", f.DebtToEquity)
	fmt.Fprintf(&b, "| Free Cash Flow | %.2f |\n", f.FreeCashFlow)
	fmt.Fprintf(&b, "| Dividend Yield | %.2f%% |\n", f.DividendYield*100)
	fmt.Fprintf(&b, "| Data Fetched At | %s |\n", f.FetchedAt.Format(time.DateOnly))

	b.WriteString("\nProvide your structured fundamental analysis report.\n")

	return b.String()
}

// SocialAnalystSystemPrompt is the system prompt that instructs the LLM to
// evaluate retail and social-media sentiment for a given ticker.
const SocialAnalystSystemPrompt = `You are a senior social media sentiment analyst. Your job is to evaluate retail and social-media sentiment signals to produce a structured sentiment report.

## Signals to Evaluate

### Sentiment Score
- Overall sentiment score: interpret the aggregated score as positive (bullish retail mood), negative (bearish retail mood), or neutral.
- Score magnitude: stronger absolute values indicate more conviction in the prevailing sentiment.

### Bullish / Bearish Positioning
- Bullish proportion: the fraction of social-media mentions expressing a positive outlook. Values above 0.6 suggest strong retail optimism.
- Bearish proportion: the fraction of social-media mentions expressing a negative outlook. Values above 0.6 suggest strong retail pessimism.
- Ratio analysis: compare bullish to bearish proportions to determine the dominant positioning.

### Engagement Volume
- Post count: total number of social-media posts mentioning the ticker. Higher volumes amplify the reliability of the sentiment signal.
- Comment count: total number of comments on those posts. High comment counts indicate deeper engagement and discussion.
- Volume context: a high sentiment score with low engagement is less reliable than one backed by thousands of posts and comments.

## Output Format

Produce a structured report with the following sections:

1. **Retail Sentiment Summary** — Interpret the overall sentiment score and bullish/bearish proportions. State whether retail participants are net bullish, bearish, or neutral and the strength of that conviction.
2. **Trending Assessment** — Evaluate post and comment volume to determine whether the ticker is trending on social media. Classify engagement as low, moderate, or high and note any implications for price action.
3. **Contrarian Signals** — Identify any contrarian indicators. Extreme bullish consensus (above 0.75) may signal euphoria and potential reversal risk. Extreme bearish consensus (above 0.75) may signal capitulation and potential recovery. Note when engagement volume is disproportionately high relative to the sentiment signal.
4. **Overall Assessment** — Synthesize all signals into a coherent view. State a directional bias (bullish, bearish, or neutral) and a confidence level (low, medium, or high). Highlight any conflicting signals between sentiment and engagement.

Be precise with numbers. Reference the actual values from the provided data. If a metric is missing or zero, note its absence rather than guessing.`

// NewsAnalystSystemPrompt is the system prompt that instructs the LLM to
// perform news sentiment and catalyst analysis on recent news articles.
const NewsAnalystSystemPrompt = `You are a senior news analyst specializing in financial markets. Your job is to evaluate recent news articles for a given ticker and produce a structured news sentiment and catalyst analysis report.

## Analysis Framework

### Sentiment Evaluation
- Classify each article's sentiment as bullish, bearish, or neutral.
- Compute an overall sentiment score from -1.0 (extremely bearish) to +1.0 (extremely bullish) by weighing article sentiments by recency and source credibility.
- Identify sentiment trends: is sentiment improving, deteriorating, or stable compared to earlier articles?

### Catalyst Identification
- **Earnings**: earnings beats/misses, guidance changes, revenue surprises.
- **Product Launches**: new product announcements, product updates, expansion into new markets.
- **Regulatory**: regulatory approvals, investigations, fines, policy changes affecting the company or sector.
- **Macro Events**: interest rate decisions, inflation data, geopolitical events, sector-wide trends.
- **M&A and Corporate Actions**: mergers, acquisitions, spin-offs, share buybacks, insider transactions.
- **Management Changes**: CEO/CFO changes, board reshuffles, key hire announcements.

### Macro Impact Assessment
- Assess how broader macroeconomic conditions mentioned in the news may affect the ticker.
- Consider sector-specific headwinds or tailwinds.
- Evaluate whether current news sentiment aligns with or diverges from the broader market narrative.

## Output Format

Produce a structured report with the following sections:

1. **Sentiment Summary** — Overall sentiment score, sentiment direction (improving/deteriorating/stable), and a brief narrative explaining the sentiment.
2. **Key Catalysts** — List each identified catalyst with its type (earnings, product, regulatory, macro, M&A, management), a brief description, and its expected impact (positive, negative, or neutral).
3. **Macro Impact** — How macroeconomic factors mentioned in the news may affect the ticker's near-term outlook.
4. **Risk Flags** — Any risks, red flags, or concerns identified from the news (e.g., regulatory threats, earnings warnings, competitive pressures).
5. **Overall Assessment** — Synthesize all signals into a coherent view. State a directional bias (bullish, bearish, or neutral) and a confidence level (low, medium, or high). Highlight any conflicting signals between sentiment and catalysts.

Be precise. Reference specific articles and their sentiments when supporting your analysis. If no articles are provided, explicitly state that no news data is available rather than fabricating information.`

// FormatSocialAnalystUserPrompt builds the user message for the social media
// analyst by formatting social-sentiment data into a readable text block that
// the LLM can analyze. When s is nil the prompt indicates that social
// sentiment data is not available.
func FormatSocialAnalystUserPrompt(ticker string, s *data.SocialSentiment) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Analyze the following social media sentiment data for %s.\n", sanitizeCell(ticker))

	if s == nil {
		b.WriteString("\n## Social Sentiment Data\n\n")
		b.WriteString("No social sentiment data available. This may be due to limited social media coverage or data source restrictions. Note the absence of data rather than guessing sentiment.\n")
		b.WriteString("\nProvide your sentiment analysis report noting which metrics are unavailable and why.\n")
		return b.String()
	}

	b.WriteString("\n## Social Sentiment Data\n\n")
	b.WriteString("| Metric | Value |\n")
	b.WriteString("|--------|-------|\n")
	fmt.Fprintf(&b, "| Sentiment Score | %.4f |\n", s.Score)
	fmt.Fprintf(&b, "| Bullish | %.4f |\n", s.Bullish)
	fmt.Fprintf(&b, "| Bearish | %.4f |\n", s.Bearish)
	fmt.Fprintf(&b, "| Post Count | %d |\n", s.PostCount)
	fmt.Fprintf(&b, "| Comment Count | %d |\n", s.CommentCount)
	fmt.Fprintf(&b, "| Measured At | %s |\n", s.MeasuredAt.Format(time.DateOnly))

	b.WriteString("\nProvide your structured social sentiment analysis report.\n")

	return b.String()
}

// FormatNewsAnalystUserPrompt builds the user message for the news analyst by
// formatting news article data into a readable text block that the LLM can
// analyze. When articles is nil or empty the prompt indicates that no news
// data is available.
func FormatNewsAnalystUserPrompt(ticker string, articles []data.NewsArticle) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Analyze the following news data for %s.\n", sanitizeCell(ticker))

	b.WriteString("\n## News Articles\n\n")
	if len(articles) == 0 {
		b.WriteString("No news articles available.\n")
		b.WriteString("\nProvide your news analysis report noting the absence of news data.\n")
		return b.String()
	}

	b.WriteString("| Date | Source | Title | Summary | Sentiment |\n")
	b.WriteString("|------|--------|-------|---------|----------|\n")
	for _, a := range articles {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %.2f |\n",
			a.PublishedAt.Format(time.DateOnly),
			sanitizeCell(a.Source),
			sanitizeCell(a.Title),
			sanitizeCell(a.Summary),
			a.Sentiment,
		)
	}

	b.WriteString("\nProvide your structured news analysis report.\n")

	return b.String()
}

// sanitizeCell normalises a string for safe inclusion in a Markdown table
// cell. It collapses newlines and carriage returns into spaces and replaces
// pipe characters so the table structure cannot be broken by untrusted input.
func sanitizeCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.TrimSpace(s)
}

// formatTimestamp renders a time as date-only (2006-01-02) when all
// timestamps in the series fall on midnight, or as date+time
// (2006-01-02 15:04 UTC) when any timestamp has a non-zero time component.
func formatTimestamp(t time.Time, intraday bool) string {
	if intraday {
		return t.UTC().Format("2006-01-02 15:04 UTC")
	}
	return t.Format(time.DateOnly)
}

// isIntraday returns true when the given time has a non-midnight time-of-day
// component (hour, minute, second, or nanosecond).
func isIntraday(t time.Time) bool {
	h, m, s := t.Clock()
	return h != 0 || m != 0 || s != 0 || t.Nanosecond() != 0
}

func hasIntradayBars(bars []domain.OHLCV) bool {
	for _, bar := range bars {
		if isIntraday(bar.Timestamp) {
			return true
		}
	}
	return false
}

func hasIntradayIndicators(indicators []domain.Indicator) bool {
	for _, ind := range indicators {
		if isIntraday(ind.Timestamp) {
			return true
		}
	}
	return false
}
