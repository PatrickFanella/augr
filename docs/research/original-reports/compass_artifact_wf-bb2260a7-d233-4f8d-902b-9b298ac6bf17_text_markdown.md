# Ten profitable trading strategies and the research behind them

Decades of academic research have identified a set of systematic strategies that generate returns beyond what standard models predict. These strategies — momentum, value, carry, trend following, mean reversion, statistical arbitrage, factor investing, quality, volatility selling, and low-volatility investing — share a common thread: each exploits a persistent market inefficiency rooted in behavioral biases, structural constraints, or compensation for bearing specific risks. What follows is a strategy-by-strategy guide to how each works, why it persists, and which landmark papers provide the empirical backbone.

---

## 1. Momentum: buying winners, selling losers

Momentum is perhaps the most robust anomaly in finance. **Cross-sectional momentum** ranks assets within a universe by their past 3–12 month returns, then goes long the top performers and short the bottom. **Time-series momentum** (or absolute momentum) evaluates each asset against its own history: if its excess return over the past 12 months is positive, go long; if negative, go short. The key difference is that time-series momentum can be net long or net short an entire asset class, while cross-sectional momentum is always dollar-neutral.

The strategy works because investors underreact to new information. Hong and Stein (1999, _Journal of Finance_) modeled how information diffuses gradually across "newswatchers," causing prices to adjust slowly. Barberis, Shleifer, and Vishny (1998, _Journal of Financial Economics_) showed that conservatism bias leads investors to underweight new evidence. The disposition effect — selling winners too early, holding losers too long — further dampens price adjustment.

The foundational empirical evidence comes from **Jegadeesh and Titman (1993, _Journal of Finance_, 48(1), 65–91)**, who showed that buying past 3–12 month winners and selling losers generates significant positive returns not explained by systematic risk. These findings held out-of-sample in their 2001 follow-up and across 12 European markets in Rouwenhorst (1998, _Journal of Finance_). **Asness, Moskowitz, and Pedersen (2013, _Journal of Finance_, 68(3), 929–985)** extended the evidence to eight diverse markets and asset classes, finding consistent momentum premia everywhere — and a striking **negative correlation of –0.41** between value and momentum, making their combination highly attractive.

For time-series momentum specifically, **Moskowitz, Ooi, and Pedersen (2012, _Journal of Financial Economics_, 104(2), 228–250)** documented significant effects across all 58 liquid futures contracts studied, with a diversified portfolio delivering substantial abnormal returns and a characteristic "smile" — performing best during extreme market conditions. The strategy's Achilles' heel is momentum crashes: Daniel and Moskowitz (2016, _Journal of Financial Economics_) showed these occur during bear market rebounds when past losers rally violently. Barroso and Santa-Clara (2015, _Journal of Financial Economics_) demonstrated that risk-managed momentum — scaling exposure by recent realized volatility — nearly **doubles the Sharpe ratio from 0.53 to 0.97** and virtually eliminates crashes.

---

## 2. Value investing: the original contrarian bet

Value investing buys securities trading below their intrinsic worth, measured by low price-to-book, price-to-earnings, or price-to-cash-flow ratios. Benjamin Graham and David Dodd codified the approach in _Security Analysis_ (1934), introducing the concept of "margin of safety" — purchasing only when price falls meaningfully below estimated value. The strategy has deep roots and an enormous empirical record.

**Fama and French (1992, _Journal of Finance_, 47(2), 427–465)** provided the canonical evidence: using NYSE, AMEX, and NASDAQ stocks from 1963–1990, they showed that book-to-market equity captures cross-sectional return variation far better than CAPM beta, which has a flat relationship with average returns after controlling for size. This finding directly challenged the Capital Asset Pricing Model. Their risk-based interpretation holds that value stocks are fundamentally riskier firms — distressed, with poor prospects — and the value premium is compensation for bearing that risk.

The behavioral counterargument is equally compelling. **Lakonishok, Shleifer, and Vishny (1994, _Journal of Finance_, 49(5), 1541–1578)** showed that investors systematically extrapolate past growth rates too far into the future, overpaying for "glamour" stocks and underpaying for value stocks. Crucially, value stocks did not underperform during market downturns, undermining the risk explanation. Basu (1977, _Journal of Finance_) provided early evidence that low P/E stocks outperform high P/E stocks.

The value premium has faced serious questions since the late 2000s. Fama and French themselves acknowledged in 2021 (_Review of Asset Pricing Studies_) that second-half premiums were "on average much lower." The primary explanation is structural: **book equity has become a poor proxy for fundamental value** as intangible assets have grown. Gonçalves and Stulz (2023, _Journal of Financial Economics_) showed that using a fundamental-to-market ratio largely explains the premium's apparent decline. Outside the United States, the value premium has persisted more robustly, and composite value measures adjusting for intangibles continue to show a meaningful effect.

---

## 3. Factor investing: from three factors to five

Factor investing systematically constructs portfolios tilted toward characteristics that explain cross-sectional return variation. The architecture evolved in distinct steps: the CAPM's single market factor (1964), Fama and French's three-factor model adding size (SMB) and value (HML) in **1993 (_Journal of Financial Economics_, 33(1), 3–56)**, Carhart's four-factor model adding momentum (1997, _Journal of Finance_), and finally the **Fama-French five-factor model (2015, _Journal of Financial Economics_, 116(1), 1–22)**, which added profitability (RMW) and investment (CMA).

The five-factor model explains **71–94%** of cross-sectional return variance across size, value, profitability, and investment portfolios. A striking finding: with profitability and investment factors included, the traditional value factor (HML) becomes redundant — its information is captured by the other factors. The model's primary failure involves small stocks that behave like unprofitable firms investing aggressively.

Whether factors represent risk compensation or behavioral mispricing remains the central debate in asset pricing. The practical implication is clear: investors can systematically harvest factor premia through transparent, rules-based portfolios, and diversifying across multiple uncorrelated factors significantly improves risk-adjusted returns compared to single-factor exposure.

---

## 4. Quality and profitability: the other side of value

Quality investing targets firms with high profitability, stable earnings, low leverage, and strong fundamentals. **Novy-Marx (2013, _Journal of Financial Economics_, 108(1), 1–28)** made the key discovery: gross profitability (revenue minus cost of goods sold, scaled by assets) predicts returns with roughly the same power as book-to-market. Because profitability and value are negatively correlated, combining both produces more consistent returns than either alone — a powerful diversification insight that directly motivated Fama and French's addition of the RMW factor.

**Asness, Frazzini, and Pedersen (2019, _Review of Accounting Studies_, 24(1), 34–112)** formalized the "Quality Minus Junk" (QMJ) factor as a composite of profitability, growth, safety, and payout. Their long-short QMJ factor earns significant risk-adjusted returns in **23 of 24 countries** studied. The puzzle is that high-quality stocks command only modestly higher valuations than junk stocks despite substantially superior fundamentals — a "puzzlingly modest" price of quality. QMJ performs well during market downturns and has negative exposure to market, value, and size factors, making it extremely difficult to explain through standard risk models. The authors conclude: "We cannot tie the returns of quality to risk."

---

## 5. Trend following and managed futures deliver "crisis alpha"

Trend following is the practical, multi-asset implementation of time-series momentum. Commodity trading advisors (CTAs) and managed futures funds identify trends across 50–100+ liquid futures markets using moving average crossovers, breakout signals, or blended lookback returns. Position sizes are scaled by inverse volatility to equalize risk contributions, typically targeting **~10% annualized portfolio volatility**.

The strategy's defining feature is its performance during crises. **Hurst, Ooi, and Pedersen (2017, _Journal of Portfolio Management_, 44(1), 15–29)** provided the most powerful evidence: trend following has delivered positive average returns with low correlation to traditional assets in **every decade since 1880**. It performed well in 8 of 10 of the largest crisis periods for a 60/40 portfolio. The SG CTA Index gained **+20.1% in 2022** — its best year since inception — while both stocks and bonds suffered severe losses.

Hurst, Ooi, and Pedersen (2013, _Journal of Investment Management_) showed that the returns of managed futures funds can be almost entirely explained by time-series momentum strategies, with individual manager alphas effectively dropping to zero after controlling for TSMOM. Structural explanations include slow-moving institutional capital, central bank policy actions creating sustained directional moves, and risk transfer from hedgers to speculators. The strategy underperforms in range-bound markets — approximately flat in 2023 and challenged in early 2025 — illustrating that trend following is not a consistent absolute-return strategy but rather a powerful portfolio diversifier with crisis-hedging properties.

---

## 6. Mean reversion operates across multiple time horizons

Mean reversion — the tendency of prices to revert toward historical averages — manifests differently depending on the time horizon. At **short horizons** (weekly/monthly), Jegadeesh (1990, _Journal of Finance_, 45(3), 881–898) documented strong negative first-order serial correlation, with contrarian strategies earning approximately **2% per month**. At **intermediate horizons** (3–12 months), momentum dominates. At **long horizons** (3–5 years), prices show significant negative autocorrelation.

The seminal paper is **De Bondt and Thaler (1985, _Journal of Finance_, 40(3), 793–805)**, which showed that portfolios of prior 3–5 year losers subsequently outperformed prior winners by a wide margin, consistent with investor overreaction. **Fama and French (1988, _Journal of Political Economy_)** estimated that a slowly mean-reverting component accounts for roughly **25–40% of 3–5 year return variance**. Poterba and Summers (1988, _Journal of Financial Economics_) confirmed this pattern using variance ratio tests across the U.S. and 17 other countries.

Lo and MacKinlay (1990, _Review of Financial Studies_) complicated the picture by showing that contrarian profits partly arise from lead-lag effects between large and small stocks, not purely from overreaction. This distinction matters for implementation: pure mean-reversion strategies must differentiate genuine overreaction from cross-serial correlation effects.

---

## 7. Statistical arbitrage and pairs trading exploit temporary mispricings

Pairs trading identifies two securities with historically co-moving prices, waits for the spread between them to diverge beyond a threshold (typically two standard deviations), then goes long the underperformer and short the outperformer. Three main approaches exist: the **distance method** (minimizing Euclidean distance between normalized prices), the **cointegration method** (building on Engle and Granger's 1987 Nobel Prize-winning framework in _Econometrica_), and **factor-model statistical arbitrage** (decomposing returns into systematic and idiosyncratic components).

**Gatev, Goetzmann, and Rouwenhorst (2006, _Review of Financial Studies_, 19(3), 797–827)** provided the definitive empirical study: using daily U.S. equity data over 1962–2002, top pairs portfolios generated annualized excess returns of up to **11%**, exceeding conservative transaction cost estimates. **Avellaneda and Lee (2010, _Quantitative Finance_)** developed PCA-based statistical arbitrage achieving Sharpe ratios of **1.44** during 1997–2007.

However, profitability has declined significantly. Do and Faff (2010, _Financial Analysts Journal_) showed mean excess returns for top pairs fell from **0.86%/month** (1962–1988) to just **0.24%/month** (2003–2009). The August 2007 quant crisis — when many stat-arb funds suffered simultaneous large losses from crowded, correlated positions — highlighted the strategy's key vulnerability. After realistic transaction costs, basic distance-method pairs trading is now marginally profitable at best, though more sophisticated approaches (cointegration, machine learning, volume signals) and within-industry pairs continue to show promise.

---

## 8. The carry trade profits from interest rate differentials

The carry trade borrows in low-interest-rate currencies and invests in high-interest-rate currencies. Under Uncovered Interest Parity (UIP), high-rate currencies should depreciate to offset the differential — but empirically they often appreciate instead, a phenomenon known as the forward premium puzzle. **Koijen, Moskowitz, Pedersen, and Vrugt (2018, _Journal of Financial Economics_, 127(2), 197–225)** generalized carry beyond FX to equities, bonds, commodities, Treasuries, credit, and options, showing that carry predicts returns cross-sectionally and in time series across all these asset classes. A global carry timing strategy achieves a **Sharpe ratio of approximately 0.9**.

**Lustig and Verdelhan (2007, _American Economic Review_)** showed that carry trade returns compensate for systematic consumption growth risk — high-rate currencies have higher exposure to aggregate consumption risk, particularly during recessions. **Brunnermeier, Nagel, and Pedersen (2009, _NBER Macroeconomics Annual_)** documented carry's crucial risk: significant negative skewness from sudden unwinding during periods of declining risk appetite. VIX increases predict carry trade losses, consistent with funding liquidity spirals. The strategy delivers steady small gains punctuated by infrequent but severe drawdowns — the classic insurance-seller profile.

---

## 9. Selling volatility harvests the insurance premium

The volatility risk premium (VRP) is the persistent tendency for implied volatility to exceed realized volatility. For S\&P 500 index options, this gap averages **3–4 percentage points**. Strategies harvest this premium by selling options, variance swaps, or VIX futures.

**Coval and Shumway (2001, _Journal of Finance_, 56(3), 983–1009)** showed that zero-beta at-the-money straddle positions produce average losses of approximately **3% per week** for option buyers — a striking measure of the premium's magnitude. **Bakshi and Kapadia (2003, _Review of Financial Studies_)** confirmed that delta-hedged option portfolios systematically underperform zero, with the VRP accounting for roughly 16% of call option prices for S\&P 500 index options.

The premium exists because option buyers pay for protection against market crashes — volatility functions as a hedging asset. Institutional demand for portfolio insurance, particularly since the 1987 crash, creates persistent buying pressure on index puts. Ilmanen (2012, _Financial Analysts Journal_) framed volatility selling as part of a broader pattern: strategies that resemble "selling insurance" are systematically rewarded, while strategies resembling "buying lottery tickets" are systematically costly. The critical caveat is tail risk: volatility-selling strategies exhibit extreme negative skewness, with catastrophic losses during events like the 2008 financial crisis and the March 2020 COVID crash.

---

## 10. Low-volatility stocks defy the CAPM's core prediction

The low-volatility anomaly may be the most theoretically puzzling finding in empirical finance. CAPM predicts that higher risk (beta) should earn higher returns, but the empirical Security Market Line is dramatically flatter than theory predicts. **Black, Jensen, and Scholes (1972)** first documented this in their empirical tests of the CAPM. **Ang, Hodrick, Xing, and Zhang (2006, _Journal of Finance_, 61(1), 259–299)** showed that stocks with high idiosyncratic volatility earn "abysmally low" average returns — a puzzle unexplained by size, value, momentum, or liquidity.

**Frazzini and Pedersen (2014, _Journal of Financial Economics_, 111(1), 1–25)** provided the leading theoretical explanation with their Betting Against Beta (BAB) factor. When investors face leverage constraints (margin requirements, regulatory limits), they cannot simply leverage low-beta assets. Instead, they tilt toward high-beta stocks to reach desired return targets, bidding up their prices and compressing their future returns. The BAB factor produces significant positive risk-adjusted returns across U.S. equities, **20 international equity markets**, Treasury bonds, corporate bonds, and futures.

**Baker, Bradley, and Wurgler (2011, _Financial Analysts Journal_)** called this "the greatest anomaly in finance" and added a complementary explanation: institutional investors benchmarked to cap-weighted indices are penalized for holding low-beta stocks (which create tracking error), effectively preventing arbitrage of the anomaly. Low-volatility strategies underperform in strong bull markets and carry interest rate sensitivity from sector tilts toward utilities and consumer staples, but their risk-adjusted outperformance over full market cycles is among the most well-documented effects in finance.

---

## Conclusion: common threads and practical synthesis

Several themes unite these ten strategies. First, the behavioral-versus-risk debate runs through every one: momentum, value, carry, volatility selling, and low-vol all have both rational risk-compensation and irrational mispricing explanations, and the truth likely involves elements of both. Second, **negative correlation between certain strategies** — especially value and momentum (–0.41 correlation) and trend following versus carry/volatility selling — means combining them produces meaningfully higher risk-adjusted returns than any single strategy. Third, many strategies share an "insurance" profile: carry, volatility selling, and to some extent low-volatility investing deliver steady small gains with infrequent large drawdowns, while trend following provides the opposite profile, performing best during crises.

The most actionable insight from this body of research is that diversifying across strategy styles — not just asset classes — is the most reliable path to improved portfolio efficiency. Ilmanen's framework of harvesting multiple independent risk premia (value, momentum, carry, volatility, defensive/quality) captures this idea. Finally, post-publication evidence is mixed but instructive: some strategies have weakened as capital has flowed in (pairs trading, simple value), while others remain robust (momentum internationally, trend following, quality). The strategies that persist tend to be those rooted in deep behavioral biases or structural constraints unlikely to be arbitraged away by informed capital alone.
