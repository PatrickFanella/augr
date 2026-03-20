---
title: "Technical Indicators"
date: 2026-03-20
tags: [data, indicators, technical-analysis, go]
---

# Technical Indicators

14 technical indicators computed in Go from OHLCV data. These are consumed primarily by the Market Analyst agent.

## Implementation Approach

Indicators are computed in pure Go without external libraries. This avoids CGo dependencies and keeps the build simple.

```go
// internal/data/indicators.go
type Indicators struct {
    // Trend
    SMA20   float64
    SMA50   float64
    SMA200  float64
    EMA12   float64
    EMA26   float64
    MACD    MACDResult

    // Momentum
    RSI     float64
    MFI     float64
    Stoch   StochResult
    WilliamsR float64
    CCI     float64
    ROC     float64

    // Volatility
    BollingerBands BollingerResult
    ATR            float64

    // Volume
    VWMA    float64
    OBV     float64
    ADL     float64
}

func ComputeIndicators(bars []OHLCV, window int) (*Indicators, error) {
    if len(bars) < 200 {
        return nil, fmt.Errorf("need at least 200 bars, got %d", len(bars))
    }
    return &Indicators{
        SMA20:          sma(closePrices(bars), 20),
        SMA50:          sma(closePrices(bars), 50),
        SMA200:         sma(closePrices(bars), 200),
        EMA12:          ema(closePrices(bars), 12),
        EMA26:          ema(closePrices(bars), 26),
        MACD:           macd(closePrices(bars)),
        RSI:            rsi(closePrices(bars), window),
        MFI:            mfi(bars, window),
        Stoch:          stochastic(bars, 14, 3),
        WilliamsR:      williamsR(bars, 14),
        CCI:            cci(bars, 20),
        ROC:            roc(closePrices(bars), 12),
        BollingerBands: bollingerBands(closePrices(bars), 20, 2.0),
        ATR:            atr(bars, window),
        VWMA:           vwma(bars, 20),
        OBV:            obv(bars),
        ADL:            adl(bars),
    }, nil
}
```

## Indicator Reference

### Trend Indicators

| Indicator             | Formula                                    | Signal                              |
| --------------------- | ------------------------------------------ | ----------------------------------- |
| **SMA** (20, 50, 200) | Mean of last N closes                      | Price above/below = bullish/bearish |
| **EMA** (12, 26)      | Exponentially weighted mean                | More responsive to recent price     |
| **MACD**              | EMA(12) - EMA(26), signal = EMA(9) of MACD | Crossover = trend change            |

### Momentum Indicators

| Indicator       | Range     | Signal                             |
| --------------- | --------- | ---------------------------------- |
| **RSI**         | 0–100     | < 30 oversold, > 70 overbought     |
| **MFI**         | 0–100     | Money flow weighted RSI            |
| **Stochastic**  | 0–100     | %K/%D crossover                    |
| **Williams %R** | -100–0    | < -80 oversold, > -20 overbought   |
| **CCI**         | Unbounded | > +100 overbought, < -100 oversold |
| **ROC**         | % change  | Momentum direction and magnitude   |

### Volatility Indicators

| Indicator           | Output                     | Signal                                        |
| ------------------- | -------------------------- | --------------------------------------------- |
| **Bollinger Bands** | Upper, middle, lower bands | Price at band = mean reversion signal         |
| **ATR**             | Average true range         | Used for stop-loss placement, position sizing |

### Volume Indicators

| Indicator | Output                         | Signal                     |
| --------- | ------------------------------ | -------------------------- |
| **VWMA**  | Volume-weighted moving average | Volume-confirmed trend     |
| **OBV**   | Cumulative volume line         | Volume preceding price     |
| **ADL**   | Accumulation/Distribution      | Buying vs selling pressure |

## Core Calculation Functions

```go
func sma(data []float64, period int) float64 {
    if len(data) < period {
        return 0
    }
    sum := 0.0
    for _, v := range data[len(data)-period:] {
        sum += v
    }
    return sum / float64(period)
}

func ema(data []float64, period int) float64 {
    if len(data) < period {
        return 0
    }
    k := 2.0 / float64(period+1)
    ema := sma(data[:period], period) // seed with SMA
    for _, v := range data[period:] {
        ema = v*k + ema*(1-k)
    }
    return ema
}

func rsi(data []float64, period int) float64 {
    if len(data) < period+1 {
        return 50 // neutral
    }
    gains, losses := 0.0, 0.0
    for i := len(data) - period; i < len(data); i++ {
        change := data[i] - data[i-1]
        if change > 0 {
            gains += change
        } else {
            losses -= change
        }
    }
    avgGain := gains / float64(period)
    avgLoss := losses / float64(period)
    if avgLoss == 0 {
        return 100
    }
    rs := avgGain / avgLoss
    return 100 - (100 / (1 + rs))
}

func atr(bars []OHLCV, period int) float64 {
    if len(bars) < period+1 {
        return 0
    }
    trs := make([]float64, 0, len(bars)-1)
    for i := 1; i < len(bars); i++ {
        tr := math.Max(
            bars[i].High-bars[i].Low,
            math.Max(
                math.Abs(bars[i].High-bars[i-1].Close),
                math.Abs(bars[i].Low-bars[i-1].Close),
            ),
        )
        trs = append(trs, tr)
    }
    return sma(trs, period)
}
```

## Testing

Each indicator function has unit tests against known values from TradingView or stockstats:

```go
func TestRSI(t *testing.T) {
    // Known RSI(14) for a sample price series
    data := []float64{/* 30+ data points */}
    got := rsi(data, 14)
    assert.InDelta(t, 62.35, got, 0.5) // allow small floating point diff
}
```

---

**Related:** [[analyst-agents]] · [[data-architecture]] · [[data-ingestion-pipeline]]
