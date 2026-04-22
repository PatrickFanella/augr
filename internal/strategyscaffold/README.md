# Strategy scaffold package

This package provides repo-native paper-trading scaffolds for one stock strategy and one options strategy.

Included scaffolds
- Stock: moving-average trend scaffold using `rules_engine`
- Options: bull put spread premium-selling scaffold using `options_rules`

Usage pattern
1. Create the stock strategy scaffold:
   - `strategyscaffold.StockPaperMovingAverageCrossover("SPY")`
2. Create a reusable stock backtest config:
   - `strategyscaffold.StockPaperBacktestConfig(strategy, start, end, 100000)`
3. Create the options strategy scaffold:
   - `strategyscaffold.OptionsPaperBullPutSpread("QQQ")`
4. Run an options synthetic backtest/validation summary over historical bars:
   - `strategyscaffold.RunOptionsPaperBacktest(ctx, "QQQ", bars, start, end, 100000, logger)`

Why this exists
- The existing `service.BacktestService` currently backtests stock `rules_engine` configs.
- Options paper trading/backtesting already exists in the repo through the options discovery/sweep/validation path.
- This package gives one tested, concrete strategy for each path without introducing a new framework.

Current limitation
- `service.BacktestService.RunBacktest` still requires `rules_engine`; it does not execute `options_rules` directly.
- For options, use the existing synthetic options sweep/validation path wrapped by `RunOptionsPaperBacktest`.
