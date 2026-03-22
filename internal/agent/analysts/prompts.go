// Package analysts provides prompt templates for the analyst agents in
// the trading pipeline.
package analysts

import (
	"fmt"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

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

	fmt.Fprintf(&b, "Analyze the following market data for %s.\n", ticker)

	// OHLCV section.
	b.WriteString("\n## OHLCV Data\n\n")
	if len(bars) == 0 {
		b.WriteString("No OHLCV data available.\n")
	} else {
		b.WriteString("| Date | Open | High | Low | Close | Volume |\n")
		b.WriteString("|------|------|------|-----|-------|--------|\n")
		for _, bar := range bars {
			fmt.Fprintf(&b, "| %s | %.2f | %.2f | %.2f | %.2f | %.0f |\n",
				bar.Timestamp.Format(time.DateOnly),
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
				ind.Name,
				ind.Timestamp.Format(time.DateOnly),
				ind.Value,
			)
		}
	}

	b.WriteString("\nProvide your structured technical analysis report.\n")

	return b.String()
}
